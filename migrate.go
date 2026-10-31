package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// migrate carries the previous version's settings, genre cache, token and browser profile into the plugins.
// It runs once, before the queue restores: Migrated stays false after any failure, so the next launch retries.
// A retry never overwrites a value the plugins already hold, so a setting the user changed since survives it.
// The old keys stay in config.json, and an old file is removed only after its copy is written and read back.
func (a *App) migrate() error {
	st := a.reg.State()
	if st.Migrated {
		return nil
	}
	var errs []error
	cfg, err := a.LoadSettings()
	if err != nil {
		errs = append(errs, fmt.Errorf("config.json: %w", err))
	} else {
		set := func(id, key string, v any) {
			if st.Settings[id] == nil {
				st.Settings[id] = map[string]any{}
			}
			if _, has := st.Settings[id][key]; !has {
				st.Settings[id][key] = v
			}
		}
		guards := []string{"lucida", "doubledouble"}
		limit, hasLimit := toInt(cfg["rateGuardLimit"])
		hasLimit = hasLimit && limit >= 0
		if on, ok := cfg["rateGuard"].(bool); ok && !on {
			limit, hasLimit = 0, true // switching the guard off wins over its old limit
		}
		wait, hasWait := toInt(cfg["rateGuardWait"])
		hasWait = hasWait && wait > 0
		for _, id := range guards {
			if hasLimit {
				set(id, guardLimitKey, limit)
			}
			if hasWait {
				set(id, guardWaitKey, wait)
			}
		}
		if on, ok := cfg["embedGenre"].(bool); ok && !on {
			if _, has := st.Enabled["chosic"]; !has {
				st.Enabled["chosic"] = false
			}
		}
		if v, ok := cfg["useSingleGenre"].(bool); ok {
			set("chosic", "singleGenre", v)
		}
	}

	data := a.reg.DataDir
	errs = append(errs,
		moveFile(filepath.Join(a.dataDir, "genres.json"), filepath.Join(data("chosic"), "genres.json")),
		moveFile(filepath.Join(a.dataDir, "doubledouble-token"), filepath.Join(data("doubledouble"), "token")),
		moveDir(filepath.Join(a.dataDir, "doubledouble-browser"), filepath.Join(data("doubledouble"), "profile")))

	err = errors.Join(errs...)
	st.Migrated = err == nil
	if serr := a.reg.SetState(st); serr != nil {
		err = errors.Join(err, serr)
	}
	return err
}

// moveFile copies from to to, reads the copy back, then removes from. A missing from is nothing to do,
// and an existing to is kept as it is, with from left in place unless it holds the same bytes.
func moveFile(from, to string) error {
	raw, err := os.ReadFile(from)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := os.Lstat(to); err == nil {
		// An existing copy stays. Identical bytes make from a leftover; different ones keep both.
		fromInfo, err1 := os.Stat(from)
		toInfo, err2 := os.Stat(to)
		if err1 == nil && err2 == nil && os.SameFile(fromInfo, toInfo) {
			return nil // to is a link to from: removing from would lose the only copy
		}
		if have, err := os.ReadFile(to); err == nil && bytes.Equal(have, raw) {
			return os.Remove(from)
		}
		return nil
	}
	mode := os.FileMode(0o600) // the token is a secret; the cache does not mind
	if fi, err := os.Stat(from); err == nil {
		mode = fi.Mode().Perm()
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	tmp := to + ".tmp"
	if err := os.WriteFile(tmp, raw, mode); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, to); err != nil {
		os.Remove(tmp)
		return err
	}
	if back, err := os.ReadFile(to); err != nil || !bytes.Equal(back, raw) {
		os.Remove(to) // a bad copy must not stand in for the file on the next try
		return fmt.Errorf("copy of %s did not read back", from)
	}
	return os.Remove(from)
}

// moveDir renames from to to; on failure from stays. A missing from is nothing to do, an existing to is kept.
func moveDir(from, to string) error {
	if _, err := os.Lstat(from); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if _, err := os.Lstat(to); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	return os.Rename(from, to)
}
