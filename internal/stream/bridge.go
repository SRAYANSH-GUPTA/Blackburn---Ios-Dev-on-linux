package stream

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/png"
	"strconv"
	"strings"
	"sync"
	"time"
)

// GuestCommand executes a guest command with stdout separated from stderr.
type GuestCommand func(context.Context, string) ([]byte, error)
type AgentCommand func(context.Context, ...string) error

type Input struct {
	Type       string  `json:"type"`
	Epoch      uint64  `json:"epoch"`
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	EndX       float64 `json:"end_x"`
	EndY       float64 `json:"end_y"`
	ViewWidth  float64 `json:"view_width"`
	ViewHeight float64 `json:"view_height"`
	Target     string  `json:"target"`
	Key        string  `json:"key"`
	Text       string  `json:"text"`
}

func (i Input) Validate() error {
	switch i.Type {
	case "touch", "drag":
		for _, v := range []float64{i.X, i.Y, i.EndX, i.EndY} {
			if !finite(v) || v < 0 {
				return errors.New("invalid input coordinates")
			}
		}
	case "tap":
		if i.Target == "" || len(i.Target) > 1024 || strings.HasPrefix(i.Target, "-") {
			return errors.New("invalid tap target")
		}
	case "key":
		if i.Key == "" || len(i.Key) > 64 {
			return errors.New("invalid key")
		}
	case "text":
		if i.Text == "" || len(i.Text) > 4096 {
			return errors.New("text must contain 1–4096 bytes")
		}
	default:
		return errors.New("unknown input type")
	}
	return nil
}

type Bridge interface {
	Capture(context.Context) (Frame, error)
	Input(context.Context, Input) error
}

// GuestBridge uses simctl's screenshot fallback, or a user-installed frame helper.
// Helper's contract: EXECUTABLE frame UDID -> one PNG on stdout. No GUI is opened.
// Agent is device-scoped by the caller and receives logical point coordinates.
type GuestBridge struct {
	Device      string
	Scale       float64
	Helper      string
	Command     GuestCommand
	Agent       AgentCommand
	helperError string
	captureMu   sync.Mutex
	inputMu     sync.Mutex
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func (b *GuestBridge) Capture(ctx context.Context) (Frame, error) {
	b.captureMu.Lock()
	defer b.captureMu.Unlock()
	if b.Device == "" || b.Command == nil || !finite(b.Scale) || b.Scale <= 0 {
		return Frame{}, errors.New("missing device, command or backing scale")
	}
	fallback := "xcrun simctl io " + shellQuote(b.Device) + " screenshot --type=png -"
	if b.Helper != "" && b.helperError == "" {
		f, err := b.capture(ctx, shellQuote(b.Helper)+" frame "+shellQuote(b.Device))
		if err == nil {
			f.Backend = "guest-helper"
			return f, nil
		}
		if ctx.Err() != nil {
			return Frame{}, ctx.Err()
		}
		b.helperError = err.Error()
	}
	f, err := b.capture(ctx, fallback)
	f.Backend = "simctl-screenshot"
	f.Warning = b.helperError
	return f, err
}
func (b *GuestBridge) capture(ctx context.Context, cmd string) (Frame, error) {
	data, err := b.Command(ctx, cmd)
	if err != nil {
		return Frame{}, err
	}
	if len(data) > 32<<20 {
		return Frame{}, errors.New("frame exceeds 32 MiB")
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return Frame{}, fmt.Errorf("guest frame is not PNG: %w", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > 16384 || cfg.Height > 16384 {
		return Frame{}, errors.New("invalid frame dimensions")
	}
	return Frame{Version: 1, Device: b.Device, Width: cfg.Width, Height: cfg.Height, Scale: b.Scale, Captured: time.Now(), PNG: data}, nil
}

func (b *GuestBridge) Input(ctx context.Context, i Input) error {
	if err := i.Validate(); err != nil {
		return err
	}
	if b.Agent == nil {
		return errors.New("agent-device input bridge unavailable")
	}
	b.inputMu.Lock()
	defer b.inputMu.Unlock()
	n := func(v float64) string { return strconv.FormatFloat(v, 'f', 3, 64) }
	var args []string
	switch i.Type {
	case "touch":
		args = []string{"press", n(i.X), n(i.Y)}
	case "drag":
		args = []string{"swipe", n(i.X), n(i.Y), n(i.EndX), n(i.EndY)}
	case "tap":
		args = []string{"click", i.Target}
	case "text":
		args = []string{"type", i.Text}
	case "key":
		switch strings.ToLower(i.Key) {
		case "home":
			args = []string{"home"}
		case "enter", "return":
			args = []string{"type", "\n"}
		case "backspace":
			args = []string{"type", "\b"}
		case "tab":
			args = []string{"type", "\t"}
		default:
			if len([]rune(i.Key)) != 1 {
				return fmt.Errorf("unsupported simulator key %q", i.Key)
			}
			args = []string{"type", i.Key}
		}
	}
	return b.Agent(ctx, args...)
}
