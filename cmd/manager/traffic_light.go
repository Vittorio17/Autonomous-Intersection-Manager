package main

import (
	"math"
	"sort"
	"sync"
	"time"

	pb "github.com/Vittorio17/Autonomous-Intersection-Manager/proto"
)

type LightColor string

const (
	LightGreen  LightColor = "GREEN"
	LightYellow LightColor = "YELLOW"
	LightRed    LightColor = "RED"
)

type SignalPhase int

const (
	PhaseNSGreen SignalPhase = iota
	PhaseNSYellow
	PhaseEWGreen
	PhaseEWYellow
)

const (
	defaultGreenDuration  = 12 * time.Second
	defaultYellowDuration = 3 * time.Second
	minQueueGapM          = 3.0 // Distanza minima paraurti-paraurti in coda (m)
	yellowCommitEtaSec    = 1.5 // Oltre questo ETA al giallo il veicolo deve fermarsi
)

type laneVehicleState struct {
	VehicleID      string
	Lane           pb.Lane
	Direction      pb.Direction
	DistanceM      float64
	Speed          float64
	Eta            float64
	VehicleLengthM float64
	IsEmergency    bool
}

// TrafficLightController implementa un semaforo classico a cicli fissi con gestione delle code per corsia.
type TrafficLightController struct {
	mu             sync.Mutex
	phase          SignalPhase
	phaseStartedAt time.Time
	greenDuration  time.Duration
	yellowDuration time.Duration
	vehicles       map[string]laneVehicleState
}

func NewTrafficLightController(now time.Time) *TrafficLightController {
	return &TrafficLightController{
		phase:          PhaseNSGreen,
		phaseStartedAt: now,
		greenDuration:  defaultGreenDuration,
		yellowDuration: defaultYellowDuration,
		vehicles:       make(map[string]laneVehicleState),
	}
}

func (tl *TrafficLightController) RemoveVehicle(id string) {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	delete(tl.vehicles, id)
}

func (tl *TrafficLightController) advancePhaseLocked(now time.Time) {
	for {
		var dur time.Duration
		switch tl.phase {
		case PhaseNSGreen, PhaseEWGreen:
			dur = tl.greenDuration
		case PhaseNSYellow, PhaseEWYellow:
			dur = tl.yellowDuration
		}

		if now.Sub(tl.phaseStartedAt) < dur {
			break
		}
		tl.phaseStartedAt = tl.phaseStartedAt.Add(dur)
		tl.phase = (tl.phase + 1) % 4
	}
}

// CurrentLight restituisce il colore attuale del semaforo per la corsia specificata.
func (tl *TrafficLightController) CurrentLight(lane pb.Lane, now time.Time) LightColor {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	tl.advancePhaseLocked(now)
	return tl.lightForLaneLocked(lane)
}

func (tl *TrafficLightController) lightForLaneLocked(lane pb.Lane) LightColor {
	isNS := lane == pb.Lane_LANE_NORTH || lane == pb.Lane_LANE_SOUTH
	isEW := lane == pb.Lane_LANE_EAST || lane == pb.Lane_LANE_WEST

	switch tl.phase {
	case PhaseNSGreen:
		if isNS {
			return LightGreen
		}
	case PhaseNSYellow:
		if isNS {
			return LightYellow
		}
	case PhaseEWGreen:
		if isEW {
			return LightGreen
		}
	case PhaseEWYellow:
		if isEW {
			return LightYellow
		}
	}
	return LightRed
}

func oppositeLane(lane pb.Lane) pb.Lane {
	switch lane {
	case pb.Lane_LANE_NORTH:
		return pb.Lane_LANE_SOUTH
	case pb.Lane_LANE_SOUTH:
		return pb.Lane_LANE_NORTH
	case pb.Lane_LANE_EAST:
		return pb.Lane_LANE_WEST
	case pb.Lane_LANE_WEST:
		return pb.Lane_LANE_EAST
	default:
		return pb.Lane_LANE_UNKNOWN
	}
}

// Evaluate calcola il comando semaforico per un veicolo, tenendo conto del colore della luce,
// della coda sulla corsia e della precedenza nelle svolte a sinistra.
// Restituisce: status, targetETA, stopDistanceM (-1 se via libera).
func (tl *TrafficLightController) Evaluate(req *pb.VehicleRequest, now time.Time) (pb.CommandStatus, float64, float64) {
	tl.mu.Lock()
	defer tl.mu.Unlock()

	tl.advancePhaseLocked(now)

	vLen := req.VehicleLengthM
	if vLen <= 0 {
		vLen = defaultVehicleLengthM
	}

	tl.vehicles[req.VehicleId] = laneVehicleState{
		VehicleID:      req.VehicleId,
		Lane:           req.OriginLane,
		Direction:      req.Direction,
		DistanceM:      req.DistanceM,
		Speed:          req.Speed,
		Eta:            req.Eta,
		VehicleLengthM: vLen,
		IsEmergency:    req.IsEmergency,
	}

	// Se il veicolo ha già superato la stop-line (sta attraversando fisicamente), completa il passaggio
	if req.DistanceM <= 0 {
		return pb.CommandStatus_STATUS_ACK_LOCK, 0, -1
	}

	// Calcola la distanza di arresto dovuta ad eventuali veicoli davanti sulla stessa corsia
	stopDist := tl.computeQueueStopDistanceLocked(req.VehicleId, req.OriginLane, req.DistanceM)

	light := tl.lightForLaneLocked(req.OriginLane)

	// 1. Luce ROSSA -> arresto obbligatorio alla stop-line (o dietro l'ultimo veicolo in coda)
	if light == LightRed {
		return pb.CommandStatus_STATUS_REJECT, 0, stopDist
	}

	// 2. Luce GIALLA -> passa solo chi è già a ridosso della stop-line (ETA <= 1.5s) senza coda davanti
	if light == LightYellow {
		if stopDist > 0.1 || req.Eta > yellowCommitEtaSec {
			return pb.CommandStatus_STATUS_REJECT, 0, stopDist
		}
		return pb.CommandStatus_STATUS_ACK_LOCK, req.Eta, -1
	}

	// 3. Luce VERDE -> verifica se c'è un veicolo fermo o lento immediatamente davanti in coda
	if stopDist > 0.5 && (req.DistanceM-stopDist) < 1.5 {
		return pb.CommandStatus_STATUS_ADJUST_SPEED, 0, stopDist
	}

	// 4. Luce VERDE + Svolta a sinistra: deve cedere il passo al traffico opposto dritto/destra
	if req.Direction == pb.Direction_DIR_LEFT {
		opp := oppositeLane(req.OriginLane)
		for _, other := range tl.vehicles {
			if other.Lane == opp && other.Direction != pb.Direction_DIR_LEFT {
				// Se il veicolo opposto sta attraversando (DistanceM <= 0) o arriverà entro 2.5s
				if other.DistanceM <= 0 || (other.DistanceM > 0 && other.Eta < 2.5) {
					return pb.CommandStatus_STATUS_REJECT, 0, stopDist
				}
			}
		}
	}

	return pb.CommandStatus_STATUS_ACK_LOCK, req.Eta, -1
}

// computeQueueStopDistanceLocked calcola la posizione della stop-line effettiva considerando i veicoli davanti.
func (tl *TrafficLightController) computeQueueStopDistanceLocked(selfID string, lane pb.Lane, selfDist float64) float64 {
	var ahead []laneVehicleState
	for id, v := range tl.vehicles {
		if id == selfID {
			continue
		}
		// Considera i veicoli sulla stessa corsia che si trovano davanti (DistanceM < selfDist)
		// e non hanno ancora liberato completamente la stop-line
		if v.Lane == lane && v.DistanceM < selfDist && v.DistanceM >= -v.VehicleLengthM {
			ahead = append(ahead, v)
		}
	}

	if len(ahead) == 0 {
		return 0.0 // Stop-line fisica dell'incrocio
	}

	// Ordina dal veicolo più vicino alla stop-line fino a quello immediatamente davanti a noi
	sort.Slice(ahead, func(i, j int) bool {
		return ahead[i].DistanceM < ahead[j].DistanceM
	})

	effectiveStop := 0.0
	for _, v := range ahead {
		// Il veicolo davanti si fermerà almeno a effectiveStop (o alla sua posizione attuale se già più indietro)
		leaderPos := math.Max(v.DistanceM, effectiveStop)
		effectiveStop = leaderPos + v.VehicleLengthM + minQueueGapM
	}

	return effectiveStop
}