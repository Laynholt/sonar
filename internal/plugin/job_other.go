//go:build !windows

package plugin

// job is a no-op here.
// shortcut: off Windows a plugin's own children outlive it; use a process group if a plugin there spawns any.
type job struct{}

func newJob(int) (job, error) { return job{}, nil }
func (job) kill()             {}
func (job) close()            {}
