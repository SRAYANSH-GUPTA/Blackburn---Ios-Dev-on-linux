package cli

import (
	"blackburn/internal/stream"
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func (a *App) simInteractive(s *Session, args []string) error {
	op := args[0]
	args = args[1:]
	port, fps, scale := 6082, 30, 1.0
	var input stream.Input
	if op == "stream" {
		f, err := parse("sim stream", args, func(f *flag.FlagSet) {
			f.IntVar(&port, "port", 6082, "loopback port")
			f.IntVar(&fps, "fps", 30, "capture rate limit")
			f.Float64Var(&scale, "scale", 1, "backing pixels per logical point")
		})
		if err != nil {
			return err
		}
		if f.NArg() != 0 || port < 1 || port > 65535 || fps < 1 || fps > 60 || !(scale == 1 || scale == 2 || scale == 3) {
			return fail("INVALID_ARGUMENT", "stream requires port 1–65535, fps 1–60, scale 1|2|3", 2)
		}
	} else {
		input.Type = op
		if op == "touch" {
			if len(args) != 2 {
				return fail("INVALID_ARGUMENT", "usage: sim touch X Y (logical points)", 2)
			}
			var err error
			input.X, err = strconv.ParseFloat(args[0], 64)
			if err != nil {
				return fail("INVALID_ARGUMENT", "invalid x coordinate", 2)
			}
			input.Y, err = strconv.ParseFloat(args[1], 64)
			if err != nil {
				return fail("INVALID_ARGUMENT", "invalid y coordinate", 2)
			}
		} else {
			if len(args) != 1 {
				return fail("INVALID_ARGUMENT", "usage: sim "+op+" VALUE", 2)
			}
			switch op {
			case "tap":
				input.Target = args[0]
			case "key":
				input.Key = args[0]
			case "text":
				input.Text = args[0]
			}
		}
		if err := input.Validate(); err != nil {
			return fail("INVALID_ARGUMENT", err.Error(), 2)
		}
	}
	if s.Endpoint.Provider == "direct-ios" {
		return fail("CAPABILITY_UNSUPPORTED", "direct iOS provider unavailable", 3)
	}
	if s.Device == "" {
		return fail("SIMULATOR_REQUIRED", "no leased simulator; run blackburn sim up first", 3)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	// Recheck the persisted lease before every guest action. A stopped/replaced
	// session must never continue receiving capture or input for the previous UDID.
	check := func() error {
		current, err := a.session()
		if err != nil {
			return err
		}
		if current.Device != s.Device || current.Endpoint.SSH != s.Endpoint.SSH || current.Stage == "STOPPED" {
			return errors.New("simulator lease lost; restart sim stream")
		}
		return nil
	}
	bridge := &stream.GuestBridge{Device: s.Device, Scale: scale, Helper: a.env("BLACKBURN_FRAME_HELPER", "")}
	bridge.Command = func(ctx context.Context, command string) ([]byte, error) {
		if err := check(); err != nil {
			return nil, err
		}
		// Unlike the control runner, never merge stderr into a binary PNG frame.
		cmd := exec.CommandContext(ctx, "ssh", "-T", "-o", "BatchMode=yes", "-o", "ConnectTimeout=5", s.Endpoint.SSH, command)
		cmd.Env = a.Env
		var diagnostic bytes.Buffer
		cmd.Stderr = &diagnostic
		out, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("guest capture: %w: %s", err, strings.TrimSpace(diagnostic.String()))
		}
		return out, nil
	}
	bridge.Agent = func(ctx context.Context, args ...string) error {
		if err := check(); err != nil {
			return err
		}
		_, err := a.macsimContext(ctx, s, append([]string{"ad"}, args...)...)
		return err
	}
	if op != "stream" {
		inputCtx, stop := context.WithTimeout(ctx, a.Timeout)
		defer stop()
		return bridge.Input(inputCtx, input)
	}
	server, err := stream.NewServer(bridge, fps)
	if err != nil {
		return err
	}
	// Fail early when the device or screenshot backend is unavailable.
	probeCtx, stop := context.WithTimeout(ctx, 10*time.Second)
	_, err = bridge.Capture(probeCtx)
	stop()
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return err
	}
	fmt.Fprintf(a.Out, "Experimental simulator viewer: http://127.0.0.1:%d/ (PNG capture, up to %d FPS; scale %.0fx)\n", port, fps, scale)
	return server.Serve(ctx, listener)
}
