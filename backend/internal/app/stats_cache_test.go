package app

import (
	"context"
	"errors"
	"fmt"
	"github.com/compose-manager/compose-manager/backend/internal/model"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestStatsCoalescingAndWorkerBound(t *testing.T) {
	var calls, active, peak atomic.Int32
	read := func(ctx context.Context, id string) (float64, uint64, uint64, error) {
		calls.Add(1)
		current := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); current > old; old = peak.Load() {
			if peak.CompareAndSwap(old, current) {
				break
			}
		}
		time.Sleep(time.Millisecond)
		return 1.5, 123, 456, nil
	}
	s := &Service{}
	var wait sync.WaitGroup
	for request := 0; request < 3; request++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			items := make([]model.Container, 40)
			for i := range items {
				items[i] = model.Container{ID: fmt.Sprint(i), State: "running"}
			}
			s.sampleContainerStats(context.Background(), items, read)
			for _, item := range items {
				if item.CPUPercent != 1.5 || item.MemoryBytes != 123 {
					t.Errorf("missing sample: %+v", item)
				}
			}
		}()
	}
	wait.Wait()
	if calls.Load() != 40 || peak.Load() > 4 {
		t.Fatalf("calls=%d peak=%d", calls.Load(), peak.Load())
	}
	s.invalidateReadCaches()
	items := []model.Container{{ID: "0", State: "running"}}
	s.sampleContainerStats(context.Background(), items, read)
	if calls.Load() != 41 || len(s.statsCache) != 1 {
		t.Fatal("invalidation/pruning failed")
	}
}

func TestStatsExpirationFailureAndStopped(t *testing.T) {
	s := &Service{statsCache: map[string]containerStat{"expired": {cpu: 9, at: time.Now().Add(-6 * time.Second)}, "stopped": {cpu: 9, at: time.Now()}}}
	items := []model.Container{{ID: "expired", State: "running"}, {ID: "stopped", State: "exited"}, {ID: "failed", State: "running"}}
	read := func(ctx context.Context, id string) (float64, uint64, uint64, error) {
		if id == "failed" {
			return 0, 0, 0, errors.New("unavailable")
		}
		if id == "stopped" {
			t.Error("sampled stopped container")
		}
		return 0, 0, 0, nil
	}
	s.sampleContainerStats(context.Background(), items, read)
	if len(s.statsCache) != 1 || items[0].CPUPercent != 0 || items[1].CPUPercent != 0 {
		t.Fatal("stale/failed sample retained")
	}
	if _, ok := s.statsCache["expired"]; !ok {
		t.Fatal("valid zero sample not cached")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.sampleContainerStats(ctx, []model.Container{{ID: "cancelled", State: "running"}}, func(ctx context.Context, id string) (float64, uint64, uint64, error) { return 0, 0, 0, ctx.Err() })
	if _, ok := s.statsCache["cancelled"]; ok {
		t.Fatal("cancelled sample cached")
	}
}
