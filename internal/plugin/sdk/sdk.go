// Package sdk is the plugin side of sonar's plugin protocol: JSON-RPC 2.0 lines on stdin and stdout.
package sdk

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"sonar/internal/plugin"
)

// Call is the request a handler serves.
type Call struct {
	id int64
	s  *Server
}

// Progress sends a $/progress note for this request.
func (c *Call) Progress(bytes, size int64, status string) {
	c.s.note(plugin.MProgress, plugin.ProgressParams{ID: c.id, Progress: plugin.Progress{Bytes: bytes, Size: size, Status: status}})
}

// Handler serves one request. ctx is cancelled when sonar sends $/cancel for it.
// Return a *plugin.Error (see Fail) to choose the error code; any other error becomes CodeOther.
type Handler func(ctx context.Context, params json.RawMessage, c *Call) (any, error)

type Server struct {
	Init    func(p plugin.InitParams) error
	Methods map[string]Handler
	Notes   map[string]func(params json.RawMessage) // e.g. plugin.MSettingsChanged

	mu  sync.Mutex // guards out and serialises writes
	out io.Writer

	cmu     sync.Mutex
	cancels map[int64]context.CancelFunc // running requests
}

// Status sends $/status.
func (s *Server) Status(st plugin.PluginStatus) { s.note(plugin.MStatus, st) }

// Serve speaks on os.Stdin and os.Stdout; it returns nil when stdin ends.
func Serve(s *Server) error { return ServeIO(s, os.Stdin, os.Stdout) }

// ServeIO answers requests from in on out until in ends. initialize and notes run in order on
// the reading goroutine; every other request runs in its own goroutine.
func ServeIO(s *Server, in io.Reader, out io.Writer) error {
	s.mu.Lock()
	s.out = out
	s.mu.Unlock()
	s.cmu.Lock()
	s.cancels = map[int64]context.CancelFunc{}
	s.cmu.Unlock()
	defer func() { // sonar is gone: stop whatever still runs
		s.cmu.Lock()
		for _, cancel := range s.cancels {
			cancel()
		}
		s.cmu.Unlock()
	}()

	sc := bufio.NewScanner(in)
	sc.Buffer(nil, 64<<20)
	for sc.Scan() {
		var m plugin.Message
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			fmt.Fprintf(os.Stderr, "sdk: skipping a line that is not JSON-RPC: %v\n", err)
			continue
		}
		switch {
		case m.ID == nil:
			s.onNote(m)
		case m.Method == plugin.MInitialize:
			var p plugin.InitParams
			err := json.Unmarshal(m.Params, &p)
			if err == nil && s.Init != nil {
				err = s.Init(p)
			}
			s.reply(*m.ID, nil, err)
		case s.Methods[m.Method] == nil:
			s.reply(*m.ID, nil, &plugin.Error{Code: -32601, Message: "method not found: " + m.Method})
		default:
			id, h := *m.ID, s.Methods[m.Method]
			ctx, cancel := context.WithCancel(context.Background())
			s.cmu.Lock()
			s.cancels[id] = cancel // before the next line, which may be its $/cancel
			s.cmu.Unlock()
			go func() {
				res, err := h(ctx, m.Params, &Call{id: id, s: s})
				s.cmu.Lock()
				delete(s.cancels, id)
				s.cmu.Unlock()
				cancel()
				s.reply(id, res, err)
			}()
		}
	}
	return sc.Err()
}

func (s *Server) onNote(m plugin.Message) {
	if m.Method == plugin.MCancel {
		var p struct {
			ID int64 `json:"id"`
		}
		if json.Unmarshal(m.Params, &p) == nil {
			s.cmu.Lock()
			cancel := s.cancels[p.ID]
			s.cmu.Unlock()
			if cancel != nil {
				cancel()
			}
		}
		return
	}
	if f := s.Notes[m.Method]; f != nil {
		f(m.Params)
	}
}

func (s *Server) reply(id int64, res any, err error) {
	m := plugin.Message{ID: &id}
	if err == nil {
		if m.Result, err = json.Marshal(res); err != nil {
			err = fmt.Errorf("encode result: %w", err)
		}
	}
	if err != nil {
		var pe *plugin.Error
		if !errors.As(err, &pe) {
			pe = &plugin.Error{Code: plugin.CodeOther, Message: err.Error()}
		}
		m.Result, m.Error = nil, pe
	}
	s.write(m)
}

func (s *Server) note(method string, params any) {
	b, err := json.Marshal(params)
	if err != nil {
		return // only our own structs come here
	}
	s.write(plugin.Message{Method: method, Params: b})
}

func (s *Server) write(m plugin.Message) {
	m.JSONRPC = "2.0"
	b, err := json.Marshal(m)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sdk: encode message: %v\n", err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.out != nil {
		s.out.Write(append(b, '\n')) // a write error means sonar is gone; stdin EOF ends Serve
	}
}

// Fail returns a *plugin.Error with code, msg and data (JSON-encoded) for a handler to return.
func Fail(code int, msg string, data any) error {
	e := &plugin.Error{Code: code, Message: msg}
	if data != nil {
		e.Data, _ = json.Marshal(data)
	}
	return e
}
