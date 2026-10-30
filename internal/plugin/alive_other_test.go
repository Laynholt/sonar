//go:build !windows

package plugin

// alive is only checked on Windows, where the job object kills the tree.
func alive(int) bool { return false }
