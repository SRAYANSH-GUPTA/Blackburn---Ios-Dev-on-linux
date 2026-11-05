package stream

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"reflect"
	"strings"
	"testing"
)

func TestGuestCapture(t *testing.T) {
	var buf bytes.Buffer
	png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 8)))
	var command string
	b := &GuestBridge{Device: "test'udid", Scale: 2, Command: func(_ context.Context, c string) ([]byte, error) { command = c; return buf.Bytes(), nil }}
	f, err := b.Capture(context.Background())
	if err != nil || f.Width != 4 || f.Height != 8 || f.Scale != 2 || f.Device != b.Device {
		t.Fatal(f, err)
	}
	if command != "xcrun simctl io 'test'\"'\"'udid' screenshot --type=png -" || strings.Contains(command, "open ") {
		t.Fatal(command)
	}
	b.Helper = "/opt/frame helper"
	if _, err = b.Capture(context.Background()); err != nil {
		t.Fatal(err)
	}
	if command != "'/opt/frame helper' frame 'test'\"'\"'udid'" {
		t.Fatal(command)
	}
	b.Command = func(context.Context, string) ([]byte, error) { return []byte("diagnostic"), nil }
	if _, err = b.Capture(context.Background()); err == nil {
		t.Fatal("invalid PNG accepted")
	}
}
func TestAgentInputRouting(t *testing.T) {
	var got []string
	b := &GuestBridge{Agent: func(_ context.Context, args ...string) error { got = args; return nil }}
	for _, tc := range []struct {
		input Input
		want  []string
	}{
		{Input{Type: "touch", X: 10, Y: 20}, []string{"press", "10.000", "20.000"}},
		{Input{Type: "drag", X: 10, Y: 20, EndX: 30, EndY: 40}, []string{"swipe", "10.000", "20.000", "30.000", "40.000"}},
		{Input{Type: "tap", Target: "label=Save"}, []string{"click", "label=Save"}},
		{Input{Type: "text", Text: "hello '世界' $(id)"}, []string{"type", "hello '世界' $(id)"}},
		{Input{Type: "key", Key: "home"}, []string{"home"}},
		{Input{Type: "key", Key: "Enter"}, []string{"type", "\n"}},
	} {
		if err := b.Input(context.Background(), tc.input); err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Fatal(tc.input, got, err)
		}
	}
	for _, input := range []Input{{Type: "key", Key: "ArrowUp"}, {Type: "touch", X: -1}, {Type: "tap", Target: "--evil"}, {Type: "text"}, {Type: "bogus"}} {
		if err := b.Input(context.Background(), input); err == nil {
			t.Fatal("accepted", input)
		}
	}
}

func TestHelperFailureFallsBack(t *testing.T) {
	var buf bytes.Buffer
	png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 8)))
	calls := 0
	b := &GuestBridge{Device: "SIM-1", Scale: 1, Helper: "/opt/helper", Command: func(_ context.Context, cmd string) ([]byte, error) {
		calls++
		if strings.Contains(cmd, "/opt/helper") {
			return []byte("incompatible helper"), nil
		}
		return buf.Bytes(), nil
	}}
	for j := 0; j < 2; j++ {
		f, err := b.Capture(context.Background())
		if err != nil || f.Backend != "simctl-screenshot" || f.Warning == "" {
			t.Fatal(f, err)
		}
	}
	if calls != 3 {
		t.Fatal("broken helper was not disabled", calls)
	}
}
