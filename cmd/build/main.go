// Command build builds the built-in plugins into build/bin/plugins and then sonar itself,
// with the plugin checksums baked in. Run it from the repo root:
//
//	go run ./cmd/build [-plugins-only] [-dev]
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"sonar/internal/plugin"
)

func main() {
	pluginsOnly := flag.Bool("plugins-only", false, "build the plugins and stop")
	dev := flag.Bool("dev", false, "run `wails dev` instead of `wails build`")
	flag.Parse()
	if err := run(*pluginsOnly, *dev); err != nil {
		fmt.Fprintln(os.Stderr, "build:", err)
		os.Exit(1)
	}
}

func run(pluginsOnly, dev bool) error {
	if _, err := os.Stat(filepath.Join("cmd", "plugins")); err != nil {
		return fmt.Errorf("run it from the repo root: %w", err)
	}
	ids, err := pluginIDs()
	if err != nil {
		return err
	}
	out := filepath.Join("build", "bin", "plugins")
	if err := os.RemoveAll(out); err != nil { // no stale plugin outlives its source
		return err
	}
	var sums []string
	for _, id := range ids {
		sum, err := buildPlugin(id, filepath.Join(out, id))
		if err != nil {
			return fmt.Errorf("plugin %s: %w", id, err)
		}
		fmt.Printf("plugin %s  %s\n", id, sum)
		sums = append(sums, id+"="+sum)
	}
	if pluginsOnly {
		return nil
	}
	verb := "build"
	if dev {
		verb = "dev"
	}
	return sh("go", "tool", "wails", verb, "-ldflags", "-X sonar/internal/plugin.BuiltinSums="+strings.Join(sums, ","))
}

// pluginIDs lists the cmd/plugins folders that hold a plugin.json.
func pluginIDs() ([]string, error) {
	entries, err := os.ReadDir(filepath.Join("cmd", "plugins"))
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		if _, err := os.Stat(filepath.Join("cmd", "plugins", e.Name(), "plugin.json")); err == nil {
			ids = append(ids, e.Name())
		}
	}
	return ids, nil
}

// buildPlugin makes dir hold <id>.exe and plugin.json, and returns the folder's checksum.
func buildPlugin(id, dir string) (string, error) {
	exe := id
	args := []string{"build"}
	if runtime.GOOS == "windows" {
		exe += ".exe"
		args = append(args, "-ldflags", "-H windowsgui") // no console window flashes
	}
	args = append(args, "-o", filepath.Join(dir, exe), "./"+filepath.ToSlash(filepath.Join("cmd", "plugins", id)))
	if err := sh("go", args...); err != nil {
		return "", err
	}
	manifest, err := os.ReadFile(filepath.Join("cmd", "plugins", id, "plugin.json"))
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), manifest, 0o644); err != nil {
		return "", err
	}
	return plugin.Checksum(dir)
}

func sh(name string, args ...string) error {
	fmt.Println("$", name, strings.Join(args, " "))
	cmd := exec.Command(name, args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}
