package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"os"
	"sync"
	"sync/atomic"
	"time"

	pb "github.com/Vittorio17/Autonomous-Intersection-Manager/proto"
)

const (
	stopSpeedThresholdMps = 0.5
	metricIntersectionW   = 14.0
	metricDefaultVehLen   = 4.5
)

type vehicleMetricTracker struct {
	vehicleID     string
	isEmergency   bool
	firstSeenAt   time.Time
	lastSeenAt    time.Time
	initialDistM  float64
	initialSpeed  float64
	lastSpeed     float64
	wasStopped    bool
	stopCount     uint64
	energyPenalty float64
	completed     bool
	idealTimeSec  float64
	actualTimeSec float64
	delaySec      float64
}

// SimulationReport rappresenta il report finale esportabile in JSON e confrontabile tra regimi.
type SimulationReport struct {
	Mode                        string  `json:"mode"`
	Timestamp                   string  `json:"timestamp"`
	UptimeSec                   float64 `json:"uptime_sec"`
	UniqueVehicles              uint64  `json:"unique_vehicles"`
	VehiclesCompleted           uint64  `json:"vehicles_completed"`
	ThroughputVehPerMin         float64 `json:"throughput_veh_per_min"`
	AvgDelaySec                 float64 `json:"avg_delay_sec"`
	MaxDelaySec                 float64 `json:"max_delay_sec"`
	OrdinaryAvgDelaySec         float64 `json:"ordinary_avg_delay_sec"`
	EmergencyVehiclesCompleted  uint64  `json:"emergency_vehicles_completed"`
	EmergencyAvgTimeSec         float64 `json:"emergency_avg_time_sec"`
	EmergencyAvgDelaySec        float64 `json:"emergency_avg_delay_sec"`
	TotalStops                  uint64  `json:"total_stops"`
	EnergyPenaltyIndex          float64 `json:"energy_penalty_index"`
	TotalRequests               uint64  `json:"total_requests"`
	ConflictsRejected           uint64  `json:"conflicts_rejected"`
	EmergencyPreemptions        uint64  `json:"emergency_preemptions"`
	AvgRenegotiationsPerVehicle float64 `json:"avg_renegotiations_per_vehicle"`
}

type Metrics struct {
	mode             string
	outputPath       string
	startTime        time.Time
	firstTrafficAt   time.Time
	lastCompletionAt time.Time
	totalRequests    atomic.Uint64
	totalLocks       atomic.Uint64
	totalRejects     atomic.Uint64
	totalPreemptions atomic.Uint64

	mu       sync.Mutex
	vehicles map[string]*vehicleMetricTracker
}

func NewMetrics() *Metrics {
	return NewMetricsWithConfig("aim", "")
}

func NewMetricsWithConfig(mode, outputPath string) *Metrics {
	if mode == "" {
		mode = "aim"
	}
	if outputPath == "" {
		outputPath = fmt.Sprintf("results_%s.json", mode)
	}
	return &Metrics{
		mode:       mode,
		outputPath: outputPath,
		startTime:  time.Now(),
		vehicles:   make(map[string]*vehicleMetricTracker),
	}
}

// RecordVehicleSeen registra la presenza di un veicolo (retrocompatibile con i test esistenti).
func (m *Metrics) RecordVehicleSeen(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.vehicles[id]; !exists {
		now := time.Now()
		if m.firstTrafficAt.IsZero() {
			m.firstTrafficAt = now
		}
		m.vehicles[id] = &vehicleMetricTracker{
			vehicleID:   id,
			firstSeenAt: now,
			lastSeenAt:  now,
		}
	}
}

// RecordTelemetry aggiorna velocità, fermate complete ed energia cinetica per ogni pacchetto del veicolo.
func (m *Metrics) RecordTelemetry(req *pb.VehicleRequest, now time.Time) {
	if req == nil || req.VehicleId == "" {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.firstTrafficAt.IsZero() {
		m.firstTrafficAt = now
	}

	vLen := req.VehicleLengthM
	if vLen <= 0 {
		vLen = metricDefaultVehLen
	}
	totalPathM := math.Max(0, req.DistanceM) + metricIntersectionW + vLen

	tr, exists := m.vehicles[req.VehicleId]
	if !exists {
		ideal := 0.0
		if req.Speed > 0 && req.DistanceM > 0 {
			ideal = totalPathM / req.Speed
		}
		tr = &vehicleMetricTracker{
			vehicleID:    req.VehicleId,
			isEmergency:  req.IsEmergency,
			firstSeenAt:  now,
			lastSeenAt:   now,
			initialDistM: totalPathM,
			initialSpeed: req.Speed,
			lastSpeed:    req.Speed,
			idealTimeSec: ideal,
		}
		m.vehicles[req.VehicleId] = tr
	} else {
		// Variazione di energia cinetica specifica |v² - v_prev²| / 100
		deltaKinetic := math.Abs(req.Speed*req.Speed-tr.lastSpeed*tr.lastSpeed) / 100.0
		tr.energyPenalty += deltaKinetic
		tr.lastSpeed = req.Speed
		tr.lastSeenAt = now
		if req.IsEmergency {
			tr.isEmergency = true
		}
	}

	// Conta una fermata completa ogni volta che la velocità scende sotto 0.5 m/s prima della stop-line
	if !req.HasExited && req.DistanceM >= 0 {
		if req.Speed < stopSpeedThresholdMps {
			if !tr.wasStopped {
				tr.stopCount++
				tr.wasStopped = true
			}
		} else {
			tr.wasStopped = false
		}
	}
}

// RecordVehicleCompletedAt calcola il tempo effettivo di attraversamento e il delay rispetto al tempo ideale.
func (m *Metrics) RecordVehicleCompletedAt(id string, completedAt time.Time) {
	if id == "" {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	tr, exists := m.vehicles[id]
	if !exists {
		tr = &vehicleMetricTracker{
			vehicleID:   id,
			firstSeenAt: completedAt,
		}
		m.vehicles[id] = tr
	}
	if tr.completed {
		return
	}

	tr.completed = true
	tr.lastSeenAt = completedAt
	if m.lastCompletionAt.IsZero() || completedAt.After(m.lastCompletionAt) {
		m.lastCompletionAt = completedAt
	}

	actual := completedAt.Sub(tr.firstSeenAt).Seconds()
	if actual < 0 {
		actual = 0
	}
	tr.actualTimeSec = actual
	if tr.idealTimeSec > 0 && actual > tr.idealTimeSec {
		tr.delaySec = actual - tr.idealTimeSec
	} else {
		tr.delaySec = 0
	}
}

// RecordVehicleCompleted mantiene la retrocompatibilità con i test e le chiamate esistenti.
func (m *Metrics) RecordVehicleCompleted() {
	m.RecordVehicleCompletedAt(fmt.Sprintf("_anon_%d", time.Now().UnixNano()), time.Now())
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

// BuildReport calcola tutti i KPI aggregati al momento dell'invocazione.
func (m *Metrics) BuildReport() SimulationReport {
	m.mu.Lock()
	defer m.mu.Unlock()

	var uniqueCount uint64
	var completedCount uint64
	var totalStops uint64
	var totalEnergyPenalty float64

	var totalDelay float64
	var maxDelay float64

	var ordinaryCount uint64
	var ordinaryDelay float64

	var emergencyCount uint64
	var emergencyTotalTime float64
	var emergencyTotalDelay float64

	for _, tr := range m.vehicles {
		uniqueCount++
		totalStops += tr.stopCount
		totalEnergyPenalty += tr.energyPenalty

		if tr.completed {
			completedCount++
			totalDelay += tr.delaySec
			if tr.delaySec > maxDelay {
				maxDelay = tr.delaySec
			}

			if tr.isEmergency {
				emergencyCount++
				emergencyTotalTime += tr.actualTimeSec
				emergencyTotalDelay += tr.delaySec
			} else {
				ordinaryCount++
				ordinaryDelay += tr.delaySec
			}
		}
	}

	uptime := time.Since(m.startTime).Seconds()

	// Finestra attiva del traffico (dal primo veicolo visto all'ultimo completamento)
	activeWindow := uptime
	if !m.firstTrafficAt.IsZero() && !m.lastCompletionAt.IsZero() && m.lastCompletionAt.After(m.firstTrafficAt) {
		activeWindow = m.lastCompletionAt.Sub(m.firstTrafficAt).Seconds()
	}

	var throughputPerMin float64
	if activeWindow > 0 && completedCount > 0 {
		throughputPerMin = (float64(completedCount) / activeWindow) * 60.0
	}

	var avgDelay, ordAvgDelay, emgAvgTime, emgAvgDelay, energyIndex float64
	if completedCount > 0 {
		avgDelay = totalDelay / float64(completedCount)
		energyIndex = totalEnergyPenalty / float64(completedCount)
	}
	if ordinaryCount > 0 {
		ordAvgDelay = ordinaryDelay / float64(ordinaryCount)
	}
	if emergencyCount > 0 {
		emgAvgTime = emergencyTotalTime / float64(emergencyCount)
		emgAvgDelay = emergencyTotalDelay / float64(emergencyCount)
	}

	rejects := m.totalRejects.Load()
	var avgRenegotiations float64
	if uniqueCount > 0 {
		avgRenegotiations = float64(rejects) / float64(uniqueCount)
	}

	return SimulationReport{
		Mode:                        m.mode,
		Timestamp:                   time.Now().Format(time.RFC3339),
		UptimeSec:                   round3(uptime),
		UniqueVehicles:              uniqueCount,
		VehiclesCompleted:           completedCount,
		ThroughputVehPerMin:         round3(throughputPerMin),
		AvgDelaySec:                 round3(avgDelay),
		MaxDelaySec:                 round3(maxDelay),
		OrdinaryAvgDelaySec:         round3(ordAvgDelay),
		EmergencyVehiclesCompleted:  emergencyCount,
		EmergencyAvgTimeSec:         round3(emgAvgTime),
		EmergencyAvgDelaySec:        round3(emgAvgDelay),
		TotalStops:                  totalStops,
		EnergyPenaltyIndex:          round3(energyIndex),
		TotalRequests:               m.totalRequests.Load(),
		ConflictsRejected:           rejects,
		EmergencyPreemptions:        m.totalPreemptions.Load(),
		AvgRenegotiationsPerVehicle: round3(avgRenegotiations),
	}
}

// ExportJSON salva il report su disco in formato JSON indentato.
func (m *Metrics) ExportJSON(path string) error {
	if path == "" {
		path = m.outputPath
	}
	report := m.BuildReport()
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func (m *Metrics) PrintSummary() {
	rep := m.BuildReport()

	slog.Info("Statistic summary of Intersection Manager",
		slog.String("mode", rep.Mode),
		slog.Float64("uptime_sec", rep.UptimeSec),
		slog.Uint64("unique_vehicles", rep.UniqueVehicles),
		slog.Uint64("vehicles_completed", rep.VehiclesCompleted),
		slog.Float64("throughput_veh_per_min", rep.ThroughputVehPerMin),
		slog.Float64("avg_delay_sec", rep.AvgDelaySec),
		slog.Float64("max_delay_sec", rep.MaxDelaySec),
		slog.Float64("ordinary_avg_delay_sec", rep.OrdinaryAvgDelaySec),
		slog.Uint64("emergency_vehicles_completed", rep.EmergencyVehiclesCompleted),
		slog.Float64("emergency_avg_time_sec", rep.EmergencyAvgTimeSec),
		slog.Float64("emergency_avg_delay_sec", rep.EmergencyAvgDelaySec),
		slog.Uint64("total_stops", rep.TotalStops),
		slog.Float64("energy_penalty_index", rep.EnergyPenaltyIndex),
		slog.Uint64("total_requests", rep.TotalRequests),
		slog.Uint64("conflicts_rejected", rep.ConflictsRejected),
		slog.Uint64("emergency_preemptions", rep.EmergencyPreemptions),
		slog.Float64("avg_renegotiations_per_vehicle", rep.AvgRenegotiationsPerVehicle),
	)

	if err := m.ExportJSON(m.outputPath); err != nil {
		slog.Error("Errore esportazione report JSON", slog.String("path", m.outputPath), slog.Any("error", err))
	} else {
		slog.Info("Report JSON esportato con successo", slog.String("path", m.outputPath))
	}
}

func round3(v float64) float64 {
	return math.Round(v*1000) / 1000
}