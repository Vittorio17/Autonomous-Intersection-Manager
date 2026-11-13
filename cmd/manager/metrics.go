package main

import (
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

type Metrics struct {
	startTime         time.Time
	totalRequests     atomic.Uint64
	totalLocks        atomic.Uint64
	totalRejects      atomic.Uint64
	totalPreemptions  atomic.Uint64
	vehiclesCompleted atomic.Uint64
	seenVehicles      sync.Map // map[string]struct{} per contare i veicoli unici
}

func NewMetrics() *Metrics {
	return &Metrics{
		startTime: time.Now(),
	}
}

func (m *Metrics) RecordVehicleSeen(id string) {
	m.seenVehicles.Store(id, struct{}{})
}

func (m *Metrics) RecordVehicleCompleted() {
	m.vehiclesCompleted.Add(1)
}

func (m *Metrics) RecordDecision(isReject bool) {
	m.totalRequests.Add(1)
	if isReject {
		m.totalRejects.Add(1)
	} else {
		m.totalLocks.Add(1)
	}
}

func (m *Metrics) RecordPreemption() {
	m.totalPreemptions.Add(1)
}

func (m *Metrics) PrintSummary() {
	var uniqueCount uint64
	m.seenVehicles.Range(func(_, _ any) bool {
		uniqueCount++
		return true
	})

	rejects := m.totalRejects.Load()
	completed := m.vehiclesCompleted.Load()
	elapsed := time.Since(m.startTime).Seconds()

	var avgRenegotiations float64
	if uniqueCount > 0 {
		avgRenegotiations = float64(rejects) / float64(uniqueCount)
	}

	var throughputPerMin float64
	if elapsed > 0 {
		throughputPerMin = (float64(completed) / elapsed) * 60.0
	}

	slog.Info("Statistic summary of Intersection Manager",
		slog.Float64("uptime_sec", elapsed),
		slog.Uint64("unique_vehicles", uniqueCount),
		slog.Uint64("vehicles_completed", completed),
		slog.Float64("throughput_veh_per_min", throughputPerMin),
		slog.Uint64("total_requests", m.totalRequests.Load()),
		slog.Uint64("conflicts_rejected", rejects),
		slog.Uint64("emergency_preemptions", m.totalPreemptions.Load()),
		slog.Float64("avg_renegotiations_per_vehicle", avgRenegotiations),
	)
}