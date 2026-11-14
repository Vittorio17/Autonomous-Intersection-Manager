package main

import (
	"math"
	"sync"

	pb "github.com/Vittorio17/Autonomous-Intersection-Manager/proto"
)

const (
	defaultVehicleLengthM = 4.5  // Lunghezza standard auto (m)
	minCrossingSpeedMS    = 5.0  // Velocità minima per il calcolo della durata (m/s)
	reactionTimeSec       = 0.5  // Tempo di reazione del sistema autonomo (s)
	commitHorizonMs       = 1500 // Orizzonte non interrompibile (1.5s prima dell'ingresso)
)

// Traiettoria fisica (in metri) all'interno dell'area dell'incrocio (14m x 14m)
func maneuverPathLength(dir pb.Direction) float64 {
	switch dir {
	case pb.Direction_DIR_RIGHT:
		return 9.0 // Arco stretto su 1 quadrante
	case pb.Direction_DIR_STRAIGHT:
		return 14.0 // Attraversamento rettilineo su 2 quadranti
	case pb.Direction_DIR_LEFT:
		return 18.5 // Arco ampio su 3 quadranti
	default:
		return 18.5 // Fail-safe conservativo
	}
}

// ComputeCrossingDurationMs calcola la durata fisica dell'attraversamento (in ms)
// più il margine di sicurezza dinamico basato su lunghezza veicolo e velocità.
func ComputeCrossingDurationMs(dir pb.Direction, vehicleLengthM, speedMS float64) (crossingMs int64, bufferMs int64) {
	if vehicleLengthM <= 0 {
		vehicleLengthM = defaultVehicleLengthM
	}
	effSpeed := math.Max(speedMS, minCrossingSpeedMS)
	pathLen := maneuverPathLength(dir)

	// Tempo necessario affinché l'intero veicolo (muso -> coda) liberi l'incrocio
	crossingSec := (pathLen + vehicleLengthM) / effSpeed

	// Buffer dinamico: tempo di reazione + tempo necessario a percorrere una lunghezza veicolo
	bufferSec := reactionTimeSec + (vehicleLengthM / effSpeed)

	return int64(math.Round(crossingSec * 1000.0)), int64(math.Round(bufferSec * 1000.0))
}

// GridReservation rappresenta l'occupazione spazio-temporale di un veicolo.
type GridReservation struct {
	VehicleID       string
	OriginLane      pb.Lane
	Direction       pb.Direction
	Quadrants       uint8
	EntryTimeMs     int64
	ExitTimeMs      int64 // Istante in cui la coda del veicolo esce dall'incrocio
	EffectiveEndMs  int64 // ExitTimeMs + Buffer dinamico
	Speed           float64
	VehicleLengthM  float64
	DistanceM       float64
	IsEmergency     bool
	NotifyCh        chan *pb.ManagerResponse
}

// ReservationGrid gestisce le prenotazioni spazio-temporali sui 4 quadranti.
type ReservationGrid struct {
	mu           sync.Mutex
	reservations map[string]GridReservation
}

func NewReservationGrid() *ReservationGrid {
	return &ReservationGrid{
		reservations: make(map[string]GridReservation),
	}
}

// RemoveVehicle rimuove la prenotazione quando il veicolo segnala has_exited o chiude lo stream.
func (g *ReservationGrid) RemoveVehicle(id string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.reservations, id)
}

// Count restituisce il numero di prenotazioni attualmente attive.
func (g *ReservationGrid) Count() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.reservations)
}

func intervalsOverlap(startA, endA, startB, endB int64) bool {
	return startA < endB && startB < endA
}

// ReserveOrSuggest valuta la richiesta usando timestamp assoluti (serverNowMs) e durata fisica.
// Restituisce: status, targetETA (s), entryTimeMs, exitTimeMs.
func (g *ReservationGrid) ReserveOrSuggest(
	req *pb.VehicleRequest,
	serverNowMs int64,
	notifyCh chan *pb.ManagerResponse,
	onPreempt func(vehicleID string, newETA float64),
) (pb.CommandStatus, float64, int64, int64) {
	g.mu.Lock()
	defer g.mu.Unlock()

	quads := getTrajectoryQuadrants(req.OriginLane, req.Direction)
	crossingMs, bufferMs := ComputeCrossingDurationMs(req.Direction, req.VehicleLengthM, req.Speed)
	totalSpanMs := crossingMs + bufferMs

	requestedEntryMs := serverNowMs + int64(math.Round(req.Eta*1000.0))
	candidateEntryMs := requestedEntryMs

	if req.IsEmergency {
		// 1. L'emergenza cede il passo solo ad altre emergenze o a veicoli già dentro il commit horizon
		for {
			shifted := false
			candidateEndMs := candidateEntryMs + totalSpanMs
			for id, res := range g.reservations {
				if id == req.VehicleId {
					continue
				}
				if arePathsConflicting(req.OriginLane, req.Direction, res.OriginLane, res.Direction) {
					if intervalsOverlap(candidateEntryMs, candidateEndMs, res.EntryTimeMs, res.EffectiveEndMs) {
						if res.IsEmergency || (res.EntryTimeMs-serverNowMs) <= commitHorizonMs {
							candidateEntryMs = res.EffectiveEndMs
							shifted = true
							break
						}
					}
				}
			}
			if !shifted {
				break
			}
		}

		// 2. Sposta i veicoli ordinari in conflitto dopo l'uscita dell'emergenza
		candidateExitMs := candidateEntryMs + crossingMs
		candidateEffectiveEndMs := candidateEntryMs + totalSpanMs
		g.preemptConflicting(req.VehicleId, quads, candidateEntryMs, candidateEffectiveEndMs, serverNowMs, onPreempt)

		vLen := req.VehicleLengthM
		if vLen <= 0 {
			vLen = defaultVehicleLengthM
		}

		g.reservations[req.VehicleId] = GridReservation{
			VehicleID:      req.VehicleId,
			OriginLane:     req.OriginLane,
			Direction:      req.Direction,
			Quadrants:      quads,
			EntryTimeMs:    candidateEntryMs,
			ExitTimeMs:     candidateExitMs,
			EffectiveEndMs: candidateEffectiveEndMs,
			Speed:          req.Speed,
			VehicleLengthM: vLen,
			DistanceM:      req.DistanceM,
			IsEmergency:    true,
			NotifyCh:       notifyCh,
		}

		assignedETA := float64(candidateEntryMs-serverNowMs) / 1000.0
		if candidateEntryMs == requestedEntryMs {
			return pb.CommandStatus_STATUS_ACK_LOCK, assignedETA, candidateEntryMs, candidateExitMs
		}
		return pb.CommandStatus_STATUS_REJECT, assignedETA, candidateEntryMs, candidateExitMs
	}

	// Veicolo ordinario: scorre in avanti finché non trova una finestra libera su tutti i quadranti
	for {
		shifted := false
		candidateEndMs := candidateEntryMs + totalSpanMs
		for id, res := range g.reservations {
			if id == req.VehicleId {
				continue
			}
			if arePathsConflicting(req.OriginLane, req.Direction, res.OriginLane, res.Direction) {
				if intervalsOverlap(candidateEntryMs, candidateEndMs, res.EntryTimeMs, res.EffectiveEndMs) {
					candidateEntryMs = res.EffectiveEndMs
					shifted = true
					break
				}
			}
		}
		if !shifted {
			break
		}
	}

	candidateExitMs := candidateEntryMs + crossingMs
	candidateEffectiveEndMs := candidateEntryMs + totalSpanMs
	assignedETA := float64(candidateEntryMs-serverNowMs) / 1000.0

	// Tolleranza di 50ms per assorbire piccoli jitter di rete tra invio e ricezione
	if math.Abs(float64(candidateEntryMs-requestedEntryMs)) <= 50.0 {
		vLen := req.VehicleLengthM
		if vLen <= 0 {
			vLen = defaultVehicleLengthM
		}

		g.reservations[req.VehicleId] = GridReservation{
			VehicleID:      req.VehicleId,
			OriginLane:     req.OriginLane,
			Direction:      req.Direction,
			Quadrants:      quads,
			EntryTimeMs:    candidateEntryMs,
			ExitTimeMs:     candidateExitMs,
			EffectiveEndMs: candidateEffectiveEndMs,
			Speed:          req.Speed,
			VehicleLengthM: vLen,
			DistanceM:      req.DistanceM,
			IsEmergency:    false,
			NotifyCh:       notifyCh,
		}
		return pb.CommandStatus_STATUS_ACK_LOCK, assignedETA, candidateEntryMs, candidateExitMs
	}

	return pb.CommandStatus_STATUS_REJECT, assignedETA, candidateEntryMs, candidateExitMs
}

func (g *ReservationGrid) preemptConflicting(
	emergID string,
	emergQuads uint8,
	emergStartMs, emergEffectiveEndMs, serverNowMs int64,
	onPreempt func(vehicleID string, newETA float64),
) {
	for id, res := range g.reservations {
		if id == emergID || res.IsEmergency {
			continue
		}
		if (emergQuads & res.Quadrants) != 0 {
			if intervalsOverlap(emergStartMs, emergEffectiveEndMs, res.EntryTimeMs, res.EffectiveEndMs) {
				durationMs := res.ExitTimeMs - res.EntryTimeMs
				bufferMs := res.EffectiveEndMs - res.ExitTimeMs

				res.EntryTimeMs = emergEffectiveEndMs
				res.ExitTimeMs = res.EntryTimeMs + durationMs
				res.EffectiveEndMs = res.ExitTimeMs + bufferMs
				g.reservations[id] = res

				newETA := float64(res.EntryTimeMs-serverNowMs) / 1000.0
				if onPreempt != nil {
					onPreempt(res.VehicleID, newETA)
				}

				if res.NotifyCh != nil {
					select {
					case res.NotifyCh <- &pb.ManagerResponse{
						VehicleId:    res.VehicleID,
						Status:       pb.CommandStatus_STATUS_ADJUST_SPEED,
						TargetEta:    newETA,
						EntryTimeMs:  res.EntryTimeMs,
						ExitTimeMs:   res.ExitTimeMs,
						ServerTimeMs: serverNowMs,
					}:
					default:
					}
				}
			}
		}
	}
}