//go:build !windows

package browser

// launchSysProcAttr is a no-op outside Windows.
func launchSysProcAttr() interface{} { return nil }
