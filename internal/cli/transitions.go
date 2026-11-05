package cli

import (
	"blackburn/internal/config"
	"fmt"
)

// TransitionPlan describes changes to execution, independent of viewer attachment.
// The pinned renderer cannot migrate a live surface between Wayland compositors.
type TransitionPlan struct {
	Restart bool     `json:"restart_required"`
	Reasons []string `json:"reasons"`
}

func PlanTransition(from, to config.Profile) (TransitionPlan, error) {
	plan := TransitionPlan{Reasons: []string{}}
	if from.Endpoint != to.Endpoint {
		return plan, fail("CAPABILITY_UNSUPPORTED", "viewer transitions cannot change execution endpoints", 3)
	}
	add := func(reason string) { plan.Restart = true; plan.Reasons = append(plan.Reasons, reason) }
	if from.Memory != to.Memory {
		add("guest memory resize")
	}
	if from.CPUs != to.CPUs {
		add("vCPU quota change")
	}
	if from.Display != to.Display {
		add("guest display mode change")
	}
	if from.Window != to.Window {
		add("Reims window realization")
	}
	if from.Window && to.Window && from.Presentation != to.Presentation {
		add("Wayland compositor binding / socket injection")
	}
	return plan, nil
}

// restartView never uses the legacy provider down command (which can force-stop).
// Device data and installed apps persist; process memory and debugger ports do not.
func (a *App) restartView(old Session, target config.Profile) error {
	fmt.Fprintln(a.Err, "session.restarting: debugger connection invalidated; restoring persistent simulator data")
	if old.Device != "" {
		if _, e := a.ssh(old.Endpoint, "xcrun simctl shutdown "+quote(old.Device)); e != nil {
			return e
		}
	}
	if e := a.down(old, a.Timeout); e != nil {
		return e
	}
	return a.bootView(old, target)
}

func (a *App) bootView(old Session, target config.Profile) error {
	if e := a.up(old.Mode, target, old.Endpoint, Session{}); e != nil {
		return e
	}
	s, e := a.session()
	if e != nil {
		return e
	}
	s.Device = old.Device
	// Persist the lease before booting so a readiness failure does not lose ownership.
	if e = a.save(s); e != nil {
		return e
	}
	if s.Device != "" {
		if _, e = a.ssh(s.Endpoint, "xcrun simctl boot "+quote(s.Device)+" && xcrun simctl bootstatus "+quote(s.Device)+" -b"); e != nil {
			return e
		}
		if target.Presentation != "none" {
			if _, e = a.ssh(s.Endpoint, "open -a Simulator --args -CurrentDeviceUDID "+quote(s.Device)); e != nil {
				return e
			}
		}
		s.Stage = "SIM_READY"
	}
	return a.save(s)
}
