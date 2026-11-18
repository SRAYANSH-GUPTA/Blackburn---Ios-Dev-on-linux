package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func withoutPath(env []string) []string {
	out := make([]string, 0, len(env))
	for _, entry := range env {
		if !strings.HasPrefix(entry, "PATH=") {
			out = append(out, entry)
		}
	}
	return out
}

func (a *App) xcrossLink() string {
	return filepath.Join(a.env("HOME", os.Getenv("HOME")), ".local", "bin", "xcross")
}

func (a *App) installXcross() error {
	target := filepath.Join(a.Root, "bin", "xcross")
	if info, err := os.Stat(target); err != nil {
		return err
	} else if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return fmt.Errorf("bundled xcross launcher is not executable: %s", target)
	}
	link := a.xcrossLink()
	if err := os.MkdirAll(filepath.Dir(link), 0755); err != nil {
		return err
	}
	info, err := os.Lstat(link)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			if old, err := os.Readlink(link); err != nil {
				return err
			} else if old == target {
				return nil
			}
		} else {
			if !info.Mode().IsRegular() {
				return fmt.Errorf("refusing to replace non-file %s", link)
			}
			backup := link + ".upstream-backup"
			if _, err := os.Lstat(backup); !os.IsNotExist(err) {
				return fmt.Errorf("refusing to overwrite backup %s", backup)
			}
			if err := os.Rename(link, backup); err != nil {
				return err
			}
			if err := os.Symlink(target, link); err != nil {
				if restoreErr := os.Rename(backup, link); restoreErr != nil {
					return fmt.Errorf("%w; restore failed: %v", err, restoreErr)
				}
				return err
			}
			return nil
		}
	}
	// Rename the new link over an old symlink without leaving a broken command.
	temp := link + ".blackburn-tmp"
	if err := os.Symlink(target, temp); err != nil {
		return err
	}
	defer os.Remove(temp)
	return os.Rename(temp, link)
}
