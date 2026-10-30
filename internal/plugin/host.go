package plugin

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ErrClosed is what calls get once the Host is closed.
var ErrClosed = errors.New("plugin closed")

const stderrTail = 20 // stderr lines kept for crash reports

// Host runs one plugin process: started on the first Call, restarted after a crash.
type Host struct {
	OnStatus func(PluginStatus) // set before the first Call

	f      Found
	init   func() InitParams
	logf   func(format string, args ...any)
	nextID atomic.Int64

	backoff     time.Duration // least time between a crash and the next start
	initTimeout time.Duration
	stall       time.Duration // a call with onProgress fails after this long without a note
	pipeGrace   time.Duration // how long output may stay open after the process exits
	maxLine     int

	startMu   sync.Mutex // one start at a time; calls queue behind it
	mu        sync.Mutex
	p         *proc // the running process, nil when none
	closed    bool
	crashedAt time.Time
	lastCrash string
}

func NewHost(f Found, init func() InitParams, logf func(format string, args ...any)) *Host {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Host{f: f, init: init, logf: logf,
		backoff: 10 * time.Second, initTimeout: 10 * time.Second, stall: 60 * time.Second,
		pipeGrace: time.Second, maxLine: 64 << 20}
}

// proc is one run of the plugin program.
type proc struct {
	h     *Host
	cmd   *exec.Cmd
	stdin io.WriteCloser
	wmu   sync.Mutex  // serialises writes to stdin
	ready atomic.Bool // initialize succeeded

	mu         sync.Mutex
	pending    map[int64]*pending
	tail       []string
	killReason string
	job        job // zero once closed

	dead chan struct{} // closed once the process is gone
	err  error         // why it is gone; set before dead closes
}

// pending is one call waiting for its answer; the reader hands it answers and progress notes.
type pending struct {
	in   chan Message
	gone chan struct{} // closed when the caller stops listening
}

// Call sends method and decodes the answer into result (when non-nil).
// onProgress, when non-nil, gets $/progress notes and arms the no-progress watchdog.
func (h *Host) Call(ctx context.Context, method string, params, result any, onProgress func(Progress)) error {
	p, err := h.running()
	if err != nil {
		return err
	}
	return p.call(ctx, method, params, result, onProgress)
}

// Notify sends a notification to the running process; until one is running and initialised it does nothing.
func (h *Host) Notify(method string, params any) error {
	h.mu.Lock()
	p := h.p
	h.mu.Unlock()
	if p == nil || !p.ready.Load() {
		return nil
	}
	return p.send(nil, method, params)
}

// Close kills the process; pending and later calls fail with ErrClosed.
func (h *Host) Close() {
	h.mu.Lock()
	h.closed = true
	p := h.p
	h.mu.Unlock()
	if p != nil {
		p.kill("")
		<-p.dead
	}
}

// LastCrash is the last crash or hang reason with the stderr tail; "" after a clean start.
func (h *Host) LastCrash() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.lastCrash
}

// running returns the live process, starting and initialising one when needed.
func (h *Host) running() (*proc, error) {
	h.startMu.Lock()
	defer h.startMu.Unlock()
	h.mu.Lock()
	p, closed, crashedAt, crash := h.p, h.closed, h.crashedAt, h.lastCrash
	h.mu.Unlock()
	switch {
	case closed:
		return nil, ErrClosed
	case p != nil:
		return p, nil
	case !crashedAt.IsZero() && time.Since(crashedAt) < h.backoff:
		return nil, fmt.Errorf("restarting after a crash: %s", crash)
	}
	p, err := h.start()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), h.initTimeout)
	defer cancel()
	if err := p.call(ctx, MInitialize, h.init(), nil, nil); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			err = fmt.Errorf("no answer in %g s", h.initTimeout.Seconds())
		}
		p.kill("initialize: " + err.Error())
		<-p.dead
		return nil, p.err
	}
	p.ready.Store(true)
	h.mu.Lock()
	h.lastCrash = ""
	h.mu.Unlock()
	return p, nil
}

func (h *Host) start() (*proc, error) {
	cmd := exec.Command(resolveRun(h.f.Dir, h.f.M.Run[0]), h.f.M.Run[1:]...)
	cmd.Dir = h.f.Dir
	hideWindow(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	// Our own pipes, not StdoutPipe: Wait closes those as soon as the process exits, losing its
	// last lines, and run must be able to stop reading when a grandchild keeps them open.
	stdout, outW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	stderr, errW, err := os.Pipe()
	if err != nil {
		stdout.Close()
		outW.Close()
		return nil, err
	}
	cmd.Stdout, cmd.Stderr = outW, errW
	err = cmd.Start()
	outW.Close()
	errW.Close()
	if err != nil {
		stdout.Close()
		stderr.Close()
		return nil, fmt.Errorf("plugin %s: %w", h.f.M.ID, err)
	}
	j, err := newJob(cmd.Process.Pid)
	if err != nil {
		h.logf("%s: no job object, its child processes may outlive it: %v", h.f.M.ID, err)
	}
	p := &proc{h: h, cmd: cmd, stdin: stdin, job: j, pending: map[int64]*pending{}, dead: make(chan struct{})}
	h.mu.Lock()
	closed := h.closed
	if !closed {
		h.p = p // before run starts, so its exit always clears it
	}
	h.mu.Unlock()
	go p.run(stdout, stderr)
	if closed {
		p.kill("")
		<-p.dead
		return nil, ErrClosed
	}
	return p, nil
}

// resolveRun finds run[0] in the plugin dir (adding .exe on Windows), else leaves it to PATH.
func resolveRun(dir, name string) string {
	cands := []string{filepath.Join(dir, name)}
	if runtime.GOOS == "windows" {
		cands = append(cands, cands[0]+".exe")
	}
	for _, c := range cands {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	return name
}

// run waits for the process to exit and its output to end, then fails everything waiting on it.
func (p *proc) run(stdout, stderr *os.File) {
	h := p.h
	outDone, errDone := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(errDone)
		sc := bufio.NewScanner(stderr)
		sc.Buffer(nil, h.maxLine)
		for sc.Scan() {
			h.logf("%s: %s", h.f.M.ID, sc.Text())
			p.mu.Lock()
			p.tail = append(p.tail, sc.Text())
			if len(p.tail) > stderrTail {
				p.tail = p.tail[1:]
			}
			p.mu.Unlock()
		}
		io.Copy(io.Discard, stderr) // keep the pipe drained after an overlong line
	}()
	go func() {
		defer close(outDone)
		sc := bufio.NewScanner(stdout)
		sc.Buffer(nil, h.maxLine)
		for sc.Scan() {
			p.handle(sc.Bytes())
		}
		if errors.Is(sc.Err(), bufio.ErrTooLong) {
			p.kill("stdout: " + sc.Err().Error())
		}
	}()
	waitErr := p.cmd.Wait()
	p.mu.Lock()
	p.job.close() // takes any grandchildren down with it
	var none job
	p.job = none
	p.mu.Unlock()
	grace := time.NewTimer(h.pipeGrace)
	defer grace.Stop()
	for _, done := range []chan struct{}{outDone, errDone} {
		select {
		case <-done:
		case <-grace.C: // something else still holds the pipes: stop reading
			stdout.Close()
			stderr.Close()
			<-done
		}
	}
	stdout.Close()
	stderr.Close()

	p.mu.Lock()
	reason, tail := p.killReason, strings.Join(p.tail, "; ")
	p.mu.Unlock()
	if reason == "" {
		reason = "exited"
		if waitErr != nil {
			reason = fmt.Sprintf("exited (%v)", waitErr)
		}
	}
	msg := fmt.Sprintf("plugin %s: %s", h.f.M.ID, reason)
	if tail != "" {
		msg += "; stderr: " + tail
	}
	h.mu.Lock()
	if h.p == p {
		h.p = nil
	}
	if h.closed {
		p.err = ErrClosed
	} else {
		p.err = errors.New(msg)
		h.crashedAt, h.lastCrash = time.Now(), msg
	}
	h.mu.Unlock()
	if p.err != ErrClosed {
		h.logf("%s", msg)
	}
	close(p.dead)
}

// handle routes one stdout line: answers and progress to their call, $/status to OnStatus, the rest to the log.
func (p *proc) handle(line []byte) {
	if len(bytes.TrimSpace(line)) == 0 {
		return
	}
	h := p.h
	var m Message
	if json.Unmarshal(line, &m) != nil || m.JSONRPC != "2.0" {
		h.logf("%s: stdout: %s", h.f.M.ID, line)
		return
	}
	id := m.ID
	switch {
	case m.Method == MProgress:
		var n ProgressParams
		if err := json.Unmarshal(m.Params, &n); err != nil {
			h.logf("%s: bad progress: %v", h.f.M.ID, err)
			return
		}
		id = &n.ID
	case m.Method == MStatus:
		var st PluginStatus
		if err := json.Unmarshal(m.Params, &st); err != nil {
			h.logf("%s: bad status: %v", h.f.M.ID, err)
		} else if h.OnStatus != nil {
			h.OnStatus(st)
		}
		return
	case m.Method != "" || id == nil:
		h.logf("%s: unexpected message: %s", h.f.M.ID, line)
		return
	}
	p.mu.Lock()
	pd := p.pending[*id]
	p.mu.Unlock()
	if pd != nil { // else a late answer to a cancelled call
		select {
		case pd.in <- m:
		case <-pd.gone:
		}
	}
}

func (p *proc) call(ctx context.Context, method string, params, result any, onProgress func(Progress)) error {
	id := p.h.nextID.Add(1)
	pd := &pending{in: make(chan Message), gone: make(chan struct{})}
	p.mu.Lock()
	p.pending[id] = pd
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		delete(p.pending, id)
		p.mu.Unlock()
		close(pd.gone)
	}()
	b, err := encode(&id, method, params)
	if err != nil {
		return fmt.Errorf("plugin %s: %s params: %w", p.h.f.M.ID, method, err)
	}
	if err := p.write(b); err != nil { // it cannot hear us: end it and report why it died
		p.kill("")
		return p.drainUntilDead(pd)
	}
	var watchdog *time.Timer
	var stall <-chan time.Time
	if onProgress != nil {
		watchdog = time.NewTimer(p.h.stall)
		defer watchdog.Stop()
		stall = watchdog.C
	}
	for {
		select {
		case m := <-pd.in:
			if m.Method == MProgress {
				if onProgress != nil {
					watchdog.Reset(p.h.stall)
					var n ProgressParams
					json.Unmarshal(m.Params, &n) // handle already decoded it once
					onProgress(n.Progress)
				}
				continue
			}
			if m.Error != nil {
				return m.Error
			}
			if result != nil && len(m.Result) > 0 {
				if err := json.Unmarshal(m.Result, result); err != nil {
					return fmt.Errorf("plugin %s: %s answer: %w", p.h.f.M.ID, method, err)
				}
			}
			return nil
		case <-ctx.Done():
			go p.send(nil, MCancel, map[string]int64{"id": id}) // a plugin that stopped reading must not hold us; kill unblocks it
			return ctx.Err()
		case <-stall:
			p.kill(fmt.Sprintf("no progress for %g s", p.h.stall.Seconds()))
			return p.drainUntilDead(pd)
		case <-p.dead:
			return p.err
		}
	}
}

// drainUntilDead keeps taking pd's messages, so the reader never blocks on it, until the process is gone.
func (p *proc) drainUntilDead(pd *pending) error {
	for {
		select {
		case <-pd.in:
		case <-p.dead:
			return p.err
		}
	}
}

// send writes one message line; id nil makes it a notification.
func (p *proc) send(id *int64, method string, params any) error {
	b, err := encode(id, method, params)
	if err != nil {
		return err
	}
	return p.write(b)
}

func encode(id *int64, method string, params any) ([]byte, error) {
	m := Message{JSONRPC: "2.0", ID: id, Method: method}
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return nil, err
		}
		m.Params = b
	}
	b, err := json.Marshal(m)
	return append(b, '\n'), err
}

func (p *proc) write(line []byte) error {
	p.wmu.Lock()
	defer p.wmu.Unlock()
	_, err := p.stdin.Write(line)
	return err
}

// kill ends the process; the first non-empty reason becomes its crash reason.
func (p *proc) kill(reason string) {
	p.mu.Lock()
	if p.killReason == "" {
		p.killReason = reason
	}
	p.job.kill()
	p.mu.Unlock()
	p.cmd.Process.Kill() // fails only when it already exited
}
