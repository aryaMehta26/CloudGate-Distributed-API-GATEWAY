package audit

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
)

func newTestWriter(batch int, interval time.Duration) (*Writer, *[]Event, *sync.Mutex) {
	w := New(nil, zap.NewNop())
	w.batchSize, w.flushInterval = batch, interval
	var mu sync.Mutex
	var got []Event
	w.write = func(es []Event) error {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, es...)
		return nil
	}
	return w, &got, &mu
}

func TestStopDrainsQueuedEvents(t *testing.T) {
	w, got, mu := newTestWriter(100, time.Hour) // only a full batch or Stop flushes
	w.Start()
	for i := 0; i < 250; i++ {
		w.Log(Event{RequestID: fmt.Sprint(i)})
	}
	w.Stop()
	mu.Lock()
	defer mu.Unlock()
	if len(*got) != 250 {
		t.Fatalf("Stop returned with %d of 250 events written", len(*got))
	}
	w.Stop() // second Stop must not panic
}

func TestTickerFlushesPartialBatch(t *testing.T) {
	w, got, mu := newTestWriter(100, 5*time.Millisecond)
	w.Start()
	defer w.Stop()
	w.Log(Event{RequestID: "one"})
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(*got)
		mu.Unlock()
		if n == 1 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("partial batch never flushed")
}

func TestLogDropsWhenQueueFull(t *testing.T) {
	w := New(nil, zap.NewNop())
	w.queue = make(chan Event, 1)
	w.Log(Event{})
	done := make(chan struct{})
	go func() { w.Log(Event{}); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Log blocked on a full queue")
	}
}
