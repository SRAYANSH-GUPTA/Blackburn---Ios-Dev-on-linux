package blackburn_test

import (
	"blackburn/internal/stream"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

type simulator struct {
	mu            sync.Mutex
	captures      int
	width, height int
	fail          bool
	inputs        chan stream.Input
	png           []byte
}

func (b *simulator) Capture(ctx context.Context) (stream.Frame, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.captures++
	if b.fail {
		return stream.Frame{}, fmt.Errorf("simulator unavailable")
	}
	return stream.Frame{Device: "SIM-TEST", Width: b.width, Height: b.height, Scale: 2, PNG: b.png, Captured: time.Now()}, nil
}
func (b *simulator) Input(ctx context.Context, i stream.Input) error {
	select {
	case b.inputs <- i:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type message struct {
	Type       string       `json:"type"`
	Controller bool         `json:"controller"`
	OK         bool         `json:"ok"`
	Error      string       `json:"error"`
	Frame      stream.Frame `json:"frame"`
}

func readMessage(t *testing.T, c *websocket.Conn, kind string) message {
	t.Helper()
	c.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		var m message
		if err := c.ReadJSON(&m); err != nil {
			t.Fatal(err)
		}
		if m.Type == kind {
			return m
		}
		if m.Type == "frame" {
			c.WriteJSON(map[string]any{"type": "ack"})
		}
	}
}
func newStream(t *testing.T) (*simulator, *stream.Server, *httptest.Server, string, string) {
	t.Helper()
	var buf bytes.Buffer
	png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 200, 400)))
	b := &simulator{width: 200, height: 400, inputs: make(chan stream.Input, 10), png: buf.Bytes()}
	s, err := stream.NewServer(b, 60)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	h := httptest.NewServer(s.Handler())
	t.Cleanup(func() { cancel(); <-done; h.Close() })
	resp, err := http.Get(h.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	tokens := regexp.MustCompile(`token=([a-f0-9]+)&control=([a-f0-9]+)`).FindStringSubmatch(string(body))
	if len(tokens) != 3 {
		t.Fatal("missing viewer credentials")
	}
	return b, s, h, tokens[1], tokens[2]
}
func dialStream(t *testing.T, h *httptest.Server, video, control string) *websocket.Conn {
	t.Helper()
	u := "ws" + strings.TrimPrefix(h.URL, "http") + "/ws?token=" + video + "&control=" + control
	c, _, err := websocket.DefaultDialer.Dial(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}
func TestStreamerEndpointsAndInput(t *testing.T) {
	b, _, h, video, control := newStream(t)
	for _, tc := range []struct {
		path string
		want int
	}{{"/health", 200}, {"/missing", 404}, {"/frame", 401}, {"/frame?token=" + video, 503}, {"/ws?token=no", 401}} {
		r, err := http.Get(h.URL + tc.path)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		if r.StatusCode != tc.want {
			t.Fatal(tc, r.StatusCode)
		}
	}
	b.mu.Lock()
	count := b.captures
	b.mu.Unlock()
	if count != 0 {
		t.Fatal("captured without viewers")
	}
	c := dialStream(t, h, video, control)
	if !readMessage(t, c, "hello").Controller {
		t.Fatal("no control")
	}
	f := readMessage(t, c, "frame").Frame
	if f.Epoch == 0 || f.Sequence == 0 || f.Scale != 2 || f.Delivered.Before(f.Captured) {
		t.Fatal(f)
	}
	// Canvas 100x200 -> backing 200x400 -> logical 100x200.
	send := func(i stream.Input) {
		t.Helper()
		if err := c.WriteJSON(map[string]any{"type": "input", "input": i}); err != nil {
			t.Fatal(err)
		}
	}
	send(stream.Input{Type: "touch", Epoch: f.Epoch, X: 25, Y: 50, ViewWidth: 100, ViewHeight: 200})
	if m := readMessage(t, c, "input"); !m.OK {
		t.Fatal(m.Error)
	}
	select {
	case i := <-b.inputs:
		if i.X != 25 || i.Y != 50 {
			t.Fatal(i)
		}
	case <-time.After(time.Second):
		t.Fatal("no input")
	}
	send(stream.Input{Type: "drag", Epoch: f.Epoch, X: 25, Y: 50, EndX: 75, EndY: 150, ViewWidth: 100, ViewHeight: 200})
	if !readMessage(t, c, "input").OK {
		t.Fatal("drag rejected")
	}
	if i := <-b.inputs; i.EndX != 75 || i.EndY != 150 {
		t.Fatal(i)
	}
	for _, i := range []stream.Input{{Type: "tap", Target: "Save"}, {Type: "key", Key: "home"}, {Type: "text", Text: "hello"}} {
		i.Epoch = f.Epoch
		send(i)
		if !readMessage(t, c, "input").OK {
			t.Fatal(i)
		}
		<-b.inputs
	}
	send(stream.Input{Type: "text", Epoch: f.Epoch + 100, Text: "stale"})
	if readMessage(t, c, "input").OK {
		t.Fatal("stale input accepted")
	}
	other := dialStream(t, h, video, control)
	if readMessage(t, other, "hello").Controller {
		t.Fatal("two controllers")
	}
	other.WriteJSON(map[string]any{"type": "input", "input": stream.Input{Type: "text", Epoch: f.Epoch, Text: "stolen"}})
	if readMessage(t, other, "input").OK {
		t.Fatal("noncontroller input accepted")
	}
	r, err := http.Get(h.URL + "/frame?token=" + video)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	if r.StatusCode != 200 || r.Header.Get("Content-Type") != "image/png" {
		t.Fatal(r.Status)
	}
}
func TestStreamerOriginAndHostChecks(t *testing.T) {
	_, s, h, video, _ := newStream(t)
	for _, tc := range []struct{ host, origin string }{{"attacker.example", ""}, {"127.0.0.1", "https://attacker.example"}} {
		req := httptest.NewRequest("GET", "http://"+tc.host+"/", nil)
		req.Host = tc.host
		req.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, req)
		if w.Code != 403 {
			t.Fatal(w.Code)
		}
	}
	header := http.Header{"Origin": []string{"http://attacker.example"}}
	_, r, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(h.URL, "http")+"/ws?token="+video, header)
	if err == nil {
		t.Fatal("cross-origin socket accepted")
	}
	if r != nil {
		r.Body.Close()
	}
}
func TestStreamerRotationFailureAndReconnect(t *testing.T) {
	b, _, h, video, control := newStream(t)
	c := dialStream(t, h, video, control)
	readMessage(t, c, "hello")
	first := readMessage(t, c, "frame").Frame
	b.mu.Lock()
	b.width, b.height = 400, 200
	b.mu.Unlock()
	var rotated stream.Frame
	for j := 0; j < 10; j++ {
		c.WriteJSON(map[string]any{"type": "ack"})
		rotated = readMessage(t, c, "frame").Frame
		if rotated.Width == 400 {
			break
		}
	}
	if rotated.Width != 400 || rotated.Epoch <= first.Epoch {
		t.Fatal("rotation did not advance epoch")
	}
	// No ack: at most one frame in flight while server continues capturing.
	b.mu.Lock()
	before := b.captures
	b.fail = true
	b.mu.Unlock()
	deadline := time.Now().Add(3 * time.Second)
	for {
		r, err := http.Get(h.URL + "/health")
		if err != nil {
			t.Fatal(err)
		}
		var state map[string]any
		json.NewDecoder(r.Body).Decode(&state)
		r.Body.Close()
		if state["capture_error"] != "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("failure not reported")
		}
		time.Sleep(10 * time.Millisecond)
	}
	b.mu.Lock()
	if b.captures <= before {
		t.Fatal("capture stopped under backpressure")
	}
	b.fail = false
	b.mu.Unlock()
	c.Close()
	// Wait for controller release before reconnecting.
	for {
		r, err := http.Get(h.URL + "/health")
		if err != nil {
			t.Fatal(err)
		}
		var state map[string]any
		json.NewDecoder(r.Body).Decode(&state)
		r.Body.Close()
		if state["viewers"] == float64(0) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("viewer did not detach")
		}
		time.Sleep(10 * time.Millisecond)
	}
	next := dialStream(t, h, video, control)
	if !readMessage(t, next, "hello").Controller {
		t.Fatal("control not released")
	}
	f := readMessage(t, next, "frame").Frame
	if f.Epoch <= rotated.Epoch {
		t.Fatal("recovery did not advance epoch")
	}
}
func TestServeCancellation(t *testing.T) {
	b := &simulator{}
	s, _ := stream.NewServer(b, 30)
	ctx, cancel := context.WithCancel(context.Background())
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, l) }()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server failed to stop")
	}
}
