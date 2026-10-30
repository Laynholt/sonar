package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"
)

// State is what the user decided; it lives in plugins.json.
type State struct {
	Enabled  map[string]bool           `json:"enabled"` // missing → built-in on, third-party off
	Order    []string                  `json:"order"`   // ids; unknown ids are appended in Discover order
	Settings map[string]map[string]any `json:"settings"`
	Trusted  map[string]string         `json:"trusted"` // id → Checksum the user confirmed
	Migrated bool                      `json:"migrated"`
}

// norm makes every map non-nil, so a saved file never holds null.
func (s *State) norm() {
	if s.Enabled == nil {
		s.Enabled = map[string]bool{}
	}
	if s.Order == nil {
		s.Order = []string{}
	}
	if s.Settings == nil {
		s.Settings = map[string]map[string]any{}
	}
	if s.Trusted == nil {
		s.Trusted = map[string]string{}
	}
}

func (s State) clone() State {
	c := State{Enabled: maps.Clone(s.Enabled), Order: slices.Clone(s.Order), Trusted: maps.Clone(s.Trusted), Migrated: s.Migrated}
	if s.Settings != nil {
		c.Settings = make(map[string]map[string]any, len(s.Settings))
		for id, v := range s.Settings {
			c.Settings[id] = maps.Clone(v)
		}
	}
	c.norm()
	return c
}

// Entry is one plugin as the UI and the typed clients see it.
type Entry struct {
	Found
	Enabled bool           // on, and (for a third-party copy) trusted
	Trusted bool           // built-in, or the user confirmed exactly this copy
	Values  map[string]any // saved settings over the manifest defaults
}

// ErrNeedsTrust is what SetEnabled answers for a third-party copy the user has not confirmed.
var ErrNeedsTrust = errors.New("needs_trust")

// Registry owns plugins.json and one Host per enabled plugin, started on first use.
type Registry struct {
	OnChange func() // called after every state change and Refresh, never under the lock
	// OnStatus gets the $/status notes of every plugin. Like OnChange, set it before the first Host call.
	OnStatus func(id string, st PluginStatus)

	dataDir, builtinDir, userDir string
	sums                         map[string]string
	init                         func(id string) InitParams
	logf                         func(format string, args ...any)

	refreshMu sync.Mutex // serialises Refresh
	installMu sync.Mutex // held while install, import or remove work in userDir; Refresh sweeps stale temps only when free
	mu        sync.Mutex
	st        State
	found     []Found // Discover order, every Sum filled
	hosts     map[string]hostRec
	links     map[string]linkRec // compiled source.links per id
}

// hostRec remembers which copy a Host was started from, so Refresh can tell when it was replaced.
type hostRec struct {
	h        *Host
	dir, sum string
}

// OpenRegistry loads plugins.json (a missing or corrupt file gives the default state) and discovers plugins.
// init supplies Lang and the proxy fields; the registry fills in Contract, Settings and DataDir.
func OpenRegistry(dataDir, builtinDir, userDir string, sums map[string]string, init func(id string) InitParams, logf func(string, ...any)) *Registry {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	r := &Registry{dataDir: dataDir, builtinDir: builtinDir, userDir: userDir, sums: sums, init: init, logf: logf,
		hosts: map[string]hostRec{}}
	if raw, err := os.ReadFile(r.statePath()); err == nil {
		if err := json.Unmarshal(raw, &r.st); err != nil {
			logf("plugins.json: %v; starting from the defaults", err)
			r.st = State{}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		logf("plugins.json: %v; starting from the defaults", err)
	}
	r.st.norm()
	r.found = r.discover()
	return r
}

func (r *Registry) statePath() string { return filepath.Join(r.dataDir, "plugins.json") }

// DataDir is the folder a plugin may keep files in.
func (r *Registry) DataDir(id string) string { return filepath.Join(r.dataDir, "plugins-data", id) }

// discover runs Discover and gives every runnable copy a checksum (Discover only hashes built-in-folder ones).
func (r *Registry) discover() []Found {
	found := Discover(r.builtinDir, r.userDir, r.sums)
	for i := range found {
		f := &found[i]
		if f.Sum == "" && f.Err == "" {
			var err error
			if f.Sum, err = Checksum(f.Dir); err != nil {
				f.Err = "checksum: " + err.Error()
			}
		}
	}
	return found
}

// Refresh rediscovers plugins and stops hosts whose plugin is gone, replaced, edited or no longer trusted.
func (r *Registry) Refresh() {
	if r.installMu.TryLock() { // else an install is running and its temp folders are live
		r.sweepStale()
		r.installMu.Unlock()
	}
	r.refreshMu.Lock() // so a slow older scan cannot overwrite a newer one; r.mu is not held while scanning
	found := r.discover()
	r.mu.Lock()
	r.found = found
	stop := r.dropInactive()
	r.mu.Unlock()
	r.refreshMu.Unlock()
	closeAll(stop)
	r.changed()
}

func (r *Registry) changed() {
	if r.OnChange != nil {
		r.OnChange()
	}
}

func closeAll(hs []*Host) {
	for _, h := range hs {
		h.Close()
	}
}

// find returns the discovered plugin with id. Call with mu held.
func (r *Registry) find(id string) (Found, bool) {
	for _, f := range r.found {
		if f.M.ID == id {
			return f, true
		}
	}
	return Found{}, false
}

// status reports whether f runs and whether it is trusted. Call with mu held.
func (r *Registry) status(f Found) (enabled, trusted bool) {
	on, set := r.st.Enabled[f.M.ID]
	if f.Builtin {
		return !set || on, true
	}
	trusted = f.Sum != "" && r.st.Trusted[f.M.ID] == f.Sum
	return on && trusted, trusted
}

// dropInactive forgets the hosts that must not run any more and returns them to close. Call with mu held.
func (r *Registry) dropInactive() (stop []*Host) {
	for id, rec := range r.hosts {
		f, ok := r.find(id)
		if on, _ := r.status(f); !ok || f.Err != "" || !on || f.Dir != rec.dir || f.Sum != rec.sum {
			stop = append(stop, rec.h)
			delete(r.hosts, id)
		}
	}
	return stop
}

// entry builds the Entry for f. Call with mu held.
func (r *Registry) entry(f Found) Entry {
	e := Entry{Found: f, Values: map[string]any{}}
	e.Enabled, e.Trusted = r.status(f)
	for _, fl := range f.M.Settings {
		if fl.Default != nil {
			e.Values[fl.Key] = fl.Default
		}
	}
	maps.Copy(e.Values, r.st.Settings[f.M.ID])
	return e
}

// List returns every plugin: the saved order first, then new ones in Discover order.
func (r *Registry) List() []Entry {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []Entry
	seen := map[string]bool{}
	add := func(f Found) {
		if !seen[f.M.ID] {
			seen[f.M.ID] = true
			out = append(out, r.entry(f))
		}
	}
	for _, id := range r.st.Order {
		if f, ok := r.find(id); ok {
			add(f)
		}
	}
	for _, f := range r.found {
		add(f)
	}
	return out
}

func (r *Registry) Get(id string) (Entry, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	f, ok := r.find(id)
	if !ok {
		return Entry{}, false
	}
	return r.entry(f), true
}

// Enabled lists the plugins of kind that are on and able to run, in Order.
func (r *Registry) Enabled(kind Kind) []Entry {
	var out []Entry
	for _, e := range r.List() {
		if e.Enabled && e.Err == "" && e.M.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

// Host returns the plugin's host, creating it on first use.
func (r *Registry) Host(id string) (*Host, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	f, ok := r.find(id)
	switch {
	case !ok:
		return nil, fmt.Errorf("plugin %q: not found", id)
	case f.Err != "":
		return nil, fmt.Errorf("plugin %q: %s", id, f.Err)
	}
	if on, trusted := r.status(f); !on {
		if !trusted {
			return nil, fmt.Errorf("plugin %q: not trusted", id)
		}
		return nil, fmt.Errorf("plugin %q: disabled", id)
	}
	if rec, ok := r.hosts[id]; ok {
		return rec.h, nil
	}
	h := NewHost(f, func() InitParams { return r.initParams(id) }, r.logf)
	h.OnStatus = func(st PluginStatus) {
		if r.OnStatus != nil {
			r.OnStatus(id, st)
		}
	}
	r.hosts[id] = hostRec{h, f.Dir, f.Sum}
	return h, nil
}

// initParams is what the plugin gets with initialize and with every $/settings note.
func (r *Registry) initParams(id string) InitParams {
	r.mu.Lock()
	var values map[string]any
	if f, ok := r.find(id); ok {
		values = r.entry(f).Values
	}
	r.mu.Unlock()
	p := r.init(id) // outside the lock: the app may call back into us
	p.Contract, p.Settings, p.DataDir = Contract, values, r.DataDir(id)
	if err := os.MkdirAll(p.DataDir, 0o755); err != nil {
		r.logf("%s: %v", id, err)
	}
	return p
}

// change applies mutate to a copy of the state, saves it, swaps it in and stops hosts that are now off.
func (r *Registry) change(mutate func(st *State) error) error {
	r.mu.Lock()
	st := r.st.clone()
	if err := mutate(&st); err != nil {
		r.mu.Unlock()
		return err
	}
	st.norm()
	if err := r.save(st); err != nil {
		r.mu.Unlock()
		return err
	}
	r.st = st
	stop := r.dropInactive()
	r.mu.Unlock()
	closeAll(stop)
	r.changed()
	return nil
}

// save writes plugins.json through a temp file in the same folder and a rename.
func (r *Registry) save(st State) error {
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(r.dataDir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(r.dataDir, "plugins-*.tmp")
	if err != nil {
		return err
	}
	_, err = tmp.Write(raw)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), r.statePath())
	}
	if err != nil {
		os.Remove(tmp.Name())
	}
	return err
}

// SetEnabled turns a plugin on or off. Turning on a third-party copy the user has not confirmed
// needs confirm, else ErrNeedsTrust; confirming records this copy's checksum.
func (r *Registry) SetEnabled(id string, on, confirm bool) error {
	return r.change(func(st *State) error {
		f, ok := r.find(id)
		if !ok {
			return fmt.Errorf("plugin %q: not found", id)
		}
		if on && !f.Builtin && f.Sum != "" && st.Trusted[id] != f.Sum {
			if !confirm {
				return ErrNeedsTrust
			}
			st.Trusted[id] = f.Sum
		}
		st.Enabled[id] = on
		return nil
	})
}

// SetOrder saves the display order; ids it does not list follow in Discover order.
func (r *Registry) SetOrder(ids []string) error {
	return r.change(func(st *State) error {
		st.Order = slices.Clone(ids)
		return nil
	})
}

// SetSettings saves the plugin's values, then tells a running plugin with $/settings.
func (r *Registry) SetSettings(id string, values map[string]any) error {
	err := r.change(func(st *State) error {
		if _, ok := r.find(id); !ok {
			return fmt.Errorf("plugin %q: not found", id)
		}
		st.Settings[id] = maps.Clone(values)
		return nil
	})
	if err != nil {
		return err
	}
	r.mu.Lock()
	rec, running := r.hosts[id]
	r.mu.Unlock()
	if running { // Notify does nothing before initialize, which reads the saved values anyway
		if err := rec.h.Notify(MSettingsChanged, r.initParams(id)); err != nil {
			r.logf("%s: settings note: %v", id, err)
		}
	}
	return nil
}

func (r *Registry) State() State {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.st.clone()
}

// SetState replaces the state (import and migration) but keeps the current Trusted map:
// trust is only ever granted by SetEnabled with confirm, never by a state coming from outside.
func (r *Registry) SetState(s State) error {
	return r.change(func(st *State) error {
		trusted := st.Trusted
		*st = s.clone()
		st.Trusted = trusted
		return nil
	})
}

// Close stops every running plugin.
func (r *Registry) Close() {
	r.mu.Lock()
	var stop []*Host
	for id, rec := range r.hosts {
		stop = append(stop, rec.h)
		delete(r.hosts, id)
	}
	r.mu.Unlock()
	closeAll(stop)
}
