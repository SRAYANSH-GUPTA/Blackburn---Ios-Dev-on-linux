// Package ide provides editor-neutral configuration and debug bridge contracts.
package ide

// Event is a versioned notification, suitable for JSONL or a DAP custom event.
type Event struct {
	SchemaVersion int            `json:"schema_version"`
	Type          string         `json:"type"`
	Event         string         `json:"event"`
	Body          map[string]any `json:"body"`
}

func RestartEvents(reason, device string) []Event {
	body := func() map[string]any {
		return map[string]any{"reason": reason, "device": device, "reattach_required": true}
	}
	return []Event{{1, "event", "session.restarting", body()}, {1, "event", "debug.invalidated", body()}}
}
