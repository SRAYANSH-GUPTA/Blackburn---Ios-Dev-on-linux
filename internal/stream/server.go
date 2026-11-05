package stream

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

//go:embed viewer.html
var viewer string

type Server struct {
	bridge                   Bridge
	fps                      int
	videoToken, controlToken string
	mu                       sync.Mutex
	viewers                  map[*websocket.Conn]*Queue
	controller               *websocket.Conn
	latest                   Frame
	sequence, epoch          uint64
	captureError             string
	inputMu                  sync.Mutex
}

func token() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	return hex.EncodeToString(b), err
}
func NewServer(b Bridge, fps int) (*Server, error) {
	if b == nil || fps < 1 || fps > 60 {
		return nil, errors.New("bridge required; fps must be 1–60")
	}
	v, err := token()
	if err != nil {
		return nil, err
	}
	c, err := token()
	if err != nil {
		return nil, err
	}
	return &Server{bridge: b, fps: fps, videoToken: v, controlToken: c, viewers: make(map[*websocket.Conn]*Queue)}, nil
}
func same(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }
func localHost(host string) bool {
	h, _, err := net.SplitHostPort(host)
	if err != nil {
		h = host
	}
	return h == "localhost" || net.ParseIP(h) != nil && net.ParseIP(h).IsLoopback()
}
func originOK(r *http.Request) bool {
	if r.Header.Get("Origin") == "" {
		return true
	}
	u, err := url.Parse(r.Header.Get("Origin"))
	return err == nil && u.Scheme == "http" && u.Host == r.Host
}
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, strings.NewReplacer("VIDEO_TOKEN", s.videoToken, "CONTROL_TOKEN", s.controlToken).Replace(viewer))
	})
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		var dropped uint64
		queued := 0
		for _, q := range s.viewers {
			dropped += q.Dropped()
			queued += len(q.Frames())
		}
		json.NewEncoder(w).Encode(map[string]any{"version": 1, "experimental": true, "codec": "png", "fps_limit": s.fps, "viewers": len(s.viewers), "epoch": s.epoch, "sequence": s.sequence, "capture_error": s.captureError, "backend": s.latest.Backend, "capture_warning": s.latest.Warning, "queued_frames": queued, "dropped_frames": dropped})
	})
	mux.HandleFunc("/frame", func(w http.ResponseWriter, r *http.Request) {
		if !same(r.URL.Query().Get("token"), s.videoToken) {
			http.Error(w, "unauthorized", 401)
			return
		}
		s.mu.Lock()
		f := s.latest
		s.mu.Unlock()
		if len(f.PNG) == 0 {
			http.Error(w, "no frame available; connect viewer first", 503)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write(f.PNG)
	})
	mux.HandleFunc("/ws", s.websocket)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		if !localHost(r.Host) || !originOK(r) {
			http.Error(w, "loopback same-origin access required", 403)
			return
		}
		if r.Method != "GET" {
			http.Error(w, "method not allowed", 405)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

// Run captures only while viewers are attached. Failed captures retry at the FPS
// limit and invalidate the epoch, preventing stale input after reconnect/reboot.
func (s *Server) Run(ctx context.Context) {
	tick := time.NewTicker(time.Second / time.Duration(s.fps))
	defer tick.Stop()
	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		for c := range s.viewers {
			c.Close()
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		s.mu.Lock()
		active := len(s.viewers) > 0
		s.mu.Unlock()
		if !active {
			continue
		}
		captureCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		f, err := s.bridge.Capture(captureCtx)
		cancel()
		s.inputMu.Lock()
		s.mu.Lock()
		if err != nil {
			s.captureError = err.Error()
			s.latest = Frame{}
			s.mu.Unlock()
			s.inputMu.Unlock()
			continue
		}
		if !(Transform{f.Width, f.Height, f.Scale, f.Rotation}).Valid() {
			s.captureError = "invalid guest geometry"
			s.latest = Frame{}
			s.mu.Unlock()
			s.inputMu.Unlock()
			continue
		}
		prev := s.latest
		if prev.Backend != f.Backend || prev.Device != f.Device || prev.Width != f.Width || prev.Height != f.Height || prev.Scale != f.Scale || prev.Rotation != f.Rotation || s.captureError != "" {
			s.epoch++
		}
		s.captureError = ""
		s.sequence++
		f.Sequence = s.sequence
		f.Epoch = s.epoch
		f.Version = 1
		s.latest = f
		for _, q := range s.viewers {
			q.Push(f)
		}
		s.mu.Unlock()
		s.inputMu.Unlock()
	}
}

// Serve requires a loopback listener. Cancellation closes clients and HTTP without
// touching VM, app, or simulator lifecycle. The caller owns the simulator lease.
func (s *Server) Serve(ctx context.Context, l net.Listener) error {
	host, _, err := net.SplitHostPort(l.Addr().String())
	if err != nil || !localHost(host) {
		l.Close()
		return errors.New("stream listener must be loopback")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); s.Run(ctx) }()
	srv := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second}
	stopped := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			srv.Close()
		case <-stopped:
		}
	}()
	err = srv.Serve(l)
	close(stopped)
	cancel()
	<-done
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
func (s *Server) route(ctx context.Context, c *websocket.Conn, i Input) error {
	if err := i.Validate(); err != nil {
		return err
	}
	s.inputMu.Lock()
	defer s.inputMu.Unlock()
	s.mu.Lock()
	f := s.latest
	owner := s.controller == c
	captureErr := s.captureError
	s.mu.Unlock()
	if !owner {
		return errors.New("another viewer owns control; reconnect after it disconnects")
	}
	if f.Epoch == 0 || i.Epoch != f.Epoch || captureErr != "" {
		return errors.New("stale stream epoch; wait for a fresh frame")
	}
	if i.Type == "touch" || i.Type == "drag" {
		t := Transform{f.Width, f.Height, f.Scale, f.Rotation}
		p, err := t.Backing(Point{i.X, i.Y}, i.ViewWidth, i.ViewHeight)
		if err != nil {
			return err
		}
		p = t.Logical(p)
		i.X, i.Y = p.X, p.Y
		if i.Type == "drag" {
			p, err = t.Backing(Point{i.EndX, i.EndY}, i.ViewWidth, i.ViewHeight)
			if err != nil {
				return err
			}
			p = t.Logical(p)
			i.EndX, i.EndY = p.X, p.Y
		}
	}
	return s.bridge.Input(ctx, i)
}
func (s *Server) websocket(w http.ResponseWriter, r *http.Request) {
	if !same(r.URL.Query().Get("token"), s.videoToken) {
		http.Error(w, "unauthorized", 401)
		return
	}
	c, err := (&websocket.Upgrader{CheckOrigin: originOK}).Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer c.Close()
	q := NewQueue()
	s.mu.Lock()
	s.viewers[c] = q
	if s.controller == nil && same(r.URL.Query().Get("control"), s.controlToken) {
		s.controller = c
	}
	owner := s.controller == c
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.viewers, c)
		if s.controller == c {
			s.controller = nil
		}
		s.mu.Unlock()
	}()
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	responses := make(chan map[string]any, 2)
	ack := make(chan struct{}, 1)
	go func() {
		defer cancel()
		defer c.Close()
		c.SetReadLimit(16 << 10)
		c.SetReadDeadline(time.Now().Add(30 * time.Second))
		for {
			var msg struct {
				Type  string `json:"type"`
				Input Input  `json:"input"`
			}
			if err := c.ReadJSON(&msg); err != nil {
				return
			}
			c.SetReadDeadline(time.Now().Add(30 * time.Second))
			if msg.Type == "ack" {
				select {
				case ack <- struct{}{}:
				default:
				}
				continue
			}
			if msg.Type == "ping" {
				continue
			}
			inputCtx, stop := context.WithTimeout(ctx, 10*time.Second)
			var err error
			if msg.Type != "input" {
				err = errors.New("unknown message type")
			} else {
				err = s.route(inputCtx, c, msg.Input)
			}
			stop()
			response := map[string]any{"type": "input", "ok": err == nil}
			if err != nil {
				response["error"] = err.Error()
			}
			select {
			case responses <- response:
			case <-ctx.Done():
				return
			}
		}
	}()
	write := func(v any) error { c.SetWriteDeadline(time.Now().Add(5 * time.Second)); return c.WriteJSON(v) }
	if write(map[string]any{"type": "hello", "version": 1, "controller": owner, "codec": "png", "clock": "host-utc", "experimental": true}) != nil {
		return
	}
	frames := q.Frames()
	waiting := false
	watchdog := time.NewTicker(time.Second)
	defer watchdog.Stop()
	sent := time.Now()
	for {
		var available <-chan Frame
		if !waiting {
			available = frames
		}
		select {
		case <-ctx.Done():
			return
		case <-watchdog.C:
			if waiting && time.Since(sent) > 10*time.Second {
				return
			}
		case <-ack:
			waiting = false
		case response := <-responses:
			if write(response) != nil {
				return
			}
		case f := <-available:
			f.Delivered = time.Now()
			if write(map[string]any{"type": "frame", "frame": f}) != nil {
				return
			}
			waiting = true
			sent = time.Now()
		}
	}
}
