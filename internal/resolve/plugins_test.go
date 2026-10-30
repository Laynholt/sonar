package resolve

import (
	"path/filepath"
	"strings"
	"testing"

	"sonar/internal/plugin"
)

func TestPluginFindersMissingSource(t *testing.T) {
	root := t.TempDir()
	reg := plugin.OpenRegistry(filepath.Join(root, "data"), filepath.Join(root, "b"), filepath.Join(root, "u"), nil,
		func(string) plugin.InitParams { return plugin.InitParams{} }, nil)
	t.Cleanup(reg.Close)
	_, err := PluginFinders(reg)("deezer", "amazon")
	if err == nil || !strings.Contains(err.Error(), "deezer") {
		t.Fatalf("err = %v, want one naming deezer", err)
	}
}
