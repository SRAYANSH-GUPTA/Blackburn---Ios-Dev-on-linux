package stream

import (
	"sync"
	"testing"
)

func TestQueueDropsOldest(t *testing.T) {
	q := NewQueue()
	for i := uint64(1); i <= 10; i++ {
		q.Push(Frame{Sequence: i})
	}
	if len(q.Frames()) != 2 || q.Dropped() != 8 {
		t.Fatalf("depth=%d drops=%d", len(q.Frames()), q.Dropped())
	}
	for _, want := range []uint64{9, 10} {
		if got := (<-q.Frames()).Sequence; got != want {
			t.Fatalf("got %d want %d", got, want)
		}
	}
}
func TestQueueConcurrent(t *testing.T) {
	q := NewQueue()
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				q.Push(Frame{})
				_ = q.Dropped()
			}
		}()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	for {
		select {
		case <-q.Frames():
		case <-done:
			if len(q.Frames()) > 2 {
				t.Fatal("queue grew")
			}
			return
		}
	}
}
