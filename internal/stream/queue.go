// Package stream implements the experimental simulator-only framebuffer transport.
package stream

import (
	"sync"
	"time"
)

// Frame is immutable after publication. PNG is encoded as base64 by JSON.
// Width/Height describe the encoded image; Rotation maps that image to backing
// coordinates clockwise. Capture timestamps use the host's clock domain.
type Frame struct {
	Backend   string    `json:"backend"`
	Warning   string    `json:"warning,omitempty"`
	Version   int       `json:"version"`
	Device    string    `json:"device"`
	Sequence  uint64    `json:"sequence"`
	Epoch     uint64    `json:"epoch"`
	Width     int       `json:"width"`
	Height    int       `json:"height"`
	Scale     float64   `json:"scale"`
	Rotation  int       `json:"rotation"`
	Captured  time.Time `json:"captured"`
	Delivered time.Time `json:"delivered"`
	PNG       []byte    `json:"png"`
}

// Queue retains at most two frames. Producers never wait on a slow viewer.
type Queue struct {
	mu      sync.Mutex
	frames  chan Frame
	dropped uint64
}

func NewQueue() *Queue { return &Queue{frames: make(chan Frame, 2)} }
func (q *Queue) Push(f Frame) {
	q.mu.Lock()
	defer q.mu.Unlock()
	select {
	case q.frames <- f:
		return
	default:
	}
	select {
	case <-q.frames:
		q.dropped++
	default:
	}
	q.frames <- f
}
func (q *Queue) Frames() <-chan Frame { return q.frames }
func (q *Queue) Dropped() uint64      { q.mu.Lock(); defer q.mu.Unlock(); return q.dropped }
