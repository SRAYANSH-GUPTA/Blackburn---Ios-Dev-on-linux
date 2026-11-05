package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"blackburn/internal/config"
)

func normalizeViewer(v string) (string, error) {
	switch v {
	case "none":
		return "none", nil
	case "browser-desktop", "stream":
		return "browser-desktop", nil
	case "native", "studio", "local":
		return "native", nil
	}
	return "", fail("INVALID_ARGUMENT", "unknown viewer "+v+" (choose none, browser-desktop/stream, native/studio/local)", 2)
}

func (a *App) viewer(s Session) (string, error) {
	if s.Mode == "" || s.Stage == "STOPPED" {
		return "none", nil
	}
	if s.Endpoint.Provider != "macbox" {
		return "", fail("CAPABILITY_UNSUPPORTED", "viewer control requires the local macbox provider", 3)
	}
	running, e := a.running()
	if e != nil {
		return "", e
	}
	if !running {
		return "none", nil
	}
	out, e := a.call(filepath.Join(a.Root, "macbox/macbox"), "view", "get")
	if e != nil {
		return "", e
	}
	// Strict parsing prevents stale/legacy provider output from being presented as fact.
	v := strings.TrimSpace(out)
	switch v {
	case "none", "native", "browser-desktop":
		return v, nil
	}
	return "", fmt.Errorf("invalid provider viewer state %q", v)
}

func (a *App) view(args []string) error {
	allowRestart := false
	filtered := []string{}
	for _, arg := range args {
		if arg == "--allow-restart" {
			allowRestart = true
		} else {
			filtered = append(filtered, arg)
		}
	}
	args = filtered
	if len(args) == 0 {
		args = []string{"get"}
	}
	op := args[0]
	if (op == "set" && len(args) != 2) || (op != "set" && len(args) != 1) || (allowRestart && op != "set") {
		return fail("INVALID_ARGUMENT", "usage: view list|get|set VIEWER [--allow-restart]|detach", 2)
	}
	var requested string
	switch op {
	case "list":
		fmt.Fprintln(a.Out, "none\nbrowser-desktop (stream)\nnative (studio, local)")
		return nil
	case "get":
		s, e := a.session()
		if e != nil {
			return e
		}
		v, e := a.viewer(s)
		if e == nil {
			fmt.Fprintln(a.Out, v)
		}
		return e
	case "detach":
		requested = "none"
	case "set":
		var e error
		requested, e = normalizeViewer(args[1])
		if e != nil {
			return e
		}
	default:
		return fail("INVALID_ARGUMENT", "usage: view list|get|set VIEWER [--allow-restart]|detach", 2)
	}
	unlock, e := a.lock()
	if e != nil {
		return e
	}
	defer unlock()
	old, e := a.session()
	if e != nil {
		return e
	}
	if old.Mode == "" || old.Stage == "STOPPED" {
		return fail("UNMET_PREREQUISITE", "start a managed session with blackburn up first", 3)
	}
	current, e := a.viewer(old)
	if e != nil {
		return e
	}
	running, e := a.running()
	if e != nil {
		return e
	}
	if !running {
		return fail("UNMET_PREREQUISITE", "VM is stopped", 3)
	}
	if current == requested {
		fmt.Fprintln(a.Out, "Viewer: "+requested+" (unchanged)")
		return nil
	}
	target := old.Profile
	// Detaching only affects presentation, retaining the backend and resource envelope.
	if requested != "none" {
		target.Presentation = requested
		target.Window = true
	}
	plan, e := PlanTransition(old.Profile, target)
	if e != nil {
		return e
	}
	if plan.Restart && !allowRestart {
		return fail("RESTART_REQUIRED", strings.Join(plan.Reasons, ", ")+"; repeat with --allow-restart (debugger will disconnect)", 6)
	}
	if requested == "native" {
		if _, e = a.call(filepath.Join(a.Root, "macbox/macbox"), "view", "capabilities"); e != nil {
			return fail("UNMET_PREREQUISITE", "native close protection unavailable: "+e.Error(), 3)
		}
		if e = a.nativeSocket(); e != nil {
			return e
		}
	}
	// Write-ahead record is retained on failure and includes both effective and desired state.
	journal := map[string]any{"schema_version": 1, "previous": old, "target": target, "viewer": requested, "plan": plan, "state": "applying"}
	journalPath := filepath.Join(a.StateDir, "view-transition.json")
	writeJournal := func() error {
		b, e := json.Marshal(journal)
		if e != nil {
			return e
		}
		return config.Atomic(journalPath, b)
	}
	if e = writeJournal(); e != nil {
		return e
	}
	provider := func(v string) error { _, err := a.call(filepath.Join(a.Root, "macbox/macbox"), "view", v); return err }
	if plan.Restart {
		e = a.restartView(old, target)
	} else {
		e = provider(requested)
	}
	if e == nil {
		s, err := a.session()
		e = err
		if e == nil {
			s.Viewer = requested
			e = a.save(s)
		}
	}
	if e != nil {
		original := e
		journal["state"] = "failed"
		journal["error"] = e.Error()
		// Recover only after QEMU is known stopped, or via another graceful shutdown.
		// A shutdown timeout must never trigger a second stop or a forced fallback.
		var failure *Failure
		if plan.Restart && !(errors.As(e, &failure) && failure.Code == "SHUTDOWN_TIMEOUT") {
			active, probeErr := a.running()
			if probeErr == nil {
				s, err := a.session()
				probeErr = err
				if probeErr == nil && active && s.Profile == old.Profile {
					// The original guest is still alive. A quiesce failure is not
					// permission to issue another shutdown request.
					s.Stage = "DEGRADED"
					_ = a.save(s)
					probeErr = fmt.Errorf("original guest retained after quiesce failure")
				} else if probeErr == nil {
					if active {
						probeErr = a.down(s, a.Timeout)
					}
					if probeErr == nil {
						probeErr = a.bootView(old, old.Profile)
					}
				}
			}
			if probeErr == nil {
				probeErr = provider(current)
			}
			if probeErr == nil {
				probeErr = a.save(old)
			}
			if probeErr != nil {
				journal["rollback_error"] = probeErr.Error()
			} else {
				journal["state"] = "rolled_back"
			}
		} else if !plan.Restart {
			if err := provider(current); err != nil {
				journal["rollback_error"] = err.Error()
			} else {
				journal["state"] = "rolled_back"
			}
		}
		_ = writeJournal()
		return original
	}
	journal["state"] = "committed"
	if e = writeJournal(); e != nil {
		return e
	}
	fmt.Fprintln(a.Out, "Viewer: "+requested)
	return nil
}

func (a *App) nativeSocket() error {
	path := a.env("WAYLAND_DISPLAY", "")
	if path != "" && !filepath.IsAbs(path) {
		path = filepath.Join(a.env("XDG_RUNTIME_DIR", ""), path)
	}
	info, e := os.Stat(path)
	if e != nil || info.Mode()&os.ModeSocket == 0 {
		return fail("UNMET_PREREQUISITE", "native viewer requires the invoking user's Wayland socket", 3)
	}
	return nil
}
