package app

import (
	"context"
	"sync"
	"time"

	"github.com/compose-manager/compose-manager/backend/internal/model"
)

type containerStat struct {
	cpu           float64
	memory, limit uint64
	at            time.Time
}

type statsReader func(context.Context, string) (float64, uint64, uint64, error)

// Coalesce overlapping overview/project/container requests. Cache only resource
// samples, not container metadata, health, or deletion references.
func (s *Service) sampleContainerStats(ctx context.Context, containers []model.Container, read statsReader) {
	s.statsMu.Lock()
	defer s.statsMu.Unlock()
	if s.statsCache == nil {
		s.statsCache = make(map[string]containerStat)
	}
	live := make(map[string]bool, len(containers))
	pending := make([]int, 0, len(containers))
	for i := range containers {
		c := &containers[i]
		if c.State != "running" {
			continue
		}
		live[c.ID] = true
		if stat, ok := s.statsCache[c.ID]; ok && time.Since(stat.at) < 5*time.Second {
			c.CPUPercent, c.MemoryBytes, c.MemoryLimit = stat.cpu, stat.memory, stat.limit
		} else {
			pending = append(pending, i)
		}
	}
	for id := range s.statsCache {
		if !live[id] {
			delete(s.statsCache, id)
		}
	}
	jobs := make(chan int)
	succeeded := make([]bool, len(containers))
	var wait sync.WaitGroup
	for worker := 0; worker < min(4, len(pending)); worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for i := range jobs {
				statCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
				cpu, memory, limit, err := read(statCtx, containers[i].ID)
				cancel()
				if err == nil {
					containers[i].CPUPercent, containers[i].MemoryBytes, containers[i].MemoryLimit = cpu, memory, limit
					succeeded[i] = true
				}
			}
		}()
	}
	completed := make([]int, 0, len(pending))
enqueue:
	for _, i := range pending {
		select {
		case jobs <- i:
			completed = append(completed, i)
		case <-ctx.Done():
			break enqueue
		}
	}
	close(jobs)
	wait.Wait()
	// Failed samples remain uncached so the next request can retry. A valid idle
	// sample can be zero CPU/memory, so successful completion is tracked separately.
	for _, i := range completed {
		if succeeded[i] {
			s.statsCache[containers[i].ID] = containerStat{containers[i].CPUPercent, containers[i].MemoryBytes, containers[i].MemoryLimit, time.Now()}
		}
	}
}

func (s *Service) invalidateReadCaches() {
	if s.discovery != nil {
		s.discovery.Invalidate()
	}
	s.statsMu.Lock()
	s.statsCache = nil
	s.statsMu.Unlock()
}
