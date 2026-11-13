# Autonomous Intersection Manager (AIM)

A distributed V2X (Vehicle-to-Everything) simulation engine. AIM replaces
traditional time-based traffic lights with a real-time, reservation-based
protocol: autonomous vehicles negotiate access to a four-way intersection with a
central manager over a bidirectional gRPC stream.

Built with **Go** and **gRPC**, the project is meant to demonstrate how vehicles
can dynamically reserve time slots in order to maximise throughput and avoid
collisions.

---

## How it works

```
┌─────────────────────┐        gRPC bidirectional stream        ┌──────────────────────────┐
│  cmd/spawner        │ ──────────────────────────────────────> │  cmd/manager             │
│  random traffic     │                                         │  IntersectionService     │
└─────────┬───────────┘                                         │  VehicleRegistry         │
          │ uses                                                │  ReserveOrSuggest        │
          ▼                                                     └──────────────────────────┘
┌─────────────────────┐                                                     ▲
│ internal/vehicle    │ ────────────────────────────────────────────────────┘
│ Simulate()          │
└─────────────────────┘
          ▲
          │
┌─────────┴───────────┐
│  cmd/agent          │  single manually configured vehicle
└─────────────────────┘
```

1. Every vehicle opens a `Negotiate` stream with the manager.
2. At each simulation tick (about one second) the vehicle sends its telemetry:
   `vehicle_id`, `speed`, `eta`, `origin_lane`, `direction`.
3. The manager checks the request against the vehicles already registered.
   Conflicting trajectories are separated by a safety margin of 2 seconds.
4. The manager answers with one of:
   - `STATUS_ACK_LOCK` — the slot is free and has been reserved;
   - `STATUS_REJECT` — the slot is busy; `target_eta` contains the next free time;
   - `STATUS_ADJUST_SPEED` — reserved for future use, currently unused.
5. On `STATUS_REJECT` the vehicle recomputes its speed to arrive at `target_eta`.
6. When the vehicle reaches the intersection it closes the stream and the manager
   removes it from the registry.

### Conflict rules

`arePathsConflicting` currently implements a simplified geometry:

| Situation | Conflict |
|---|---|
| Same origin lane | Yes |
| Opposite lanes, both going straight | No |
| Any other combination | Yes |

Extending this matrix to all real turn movements is tracked in issue #22.

---

## Repository layout

| Path | Description |
|---|---|
| `cmd/manager/main.go` | gRPC server, vehicle registry, conflict logic |
| `cmd/manager/main_test.go` | End-to-end and concurrency tests |
| `cmd/agent/main.go` | Manually configured single vehicle |
| `cmd/spawner/main.go` | Random traffic generator for stress tests |
| `internal/vehicle/vehicle.go` | Shared gRPC client / simulation loop |
| `proto/intersection.proto` | gRPC contract |
| `proto/intersection.pb.go` | Generated protobuf messages |
| `proto/intersection_grpc.pb.go` | Generated gRPC stubs |
| `proto.bat` | Windows helper to regenerate the stubs |
| `docker-compose.yml` | Manager + spawner stack |
| `.github/workflows/ci.yml` | Build, vet and race tests on GitHub Actions |

---

## Quick start

### Docker

```bash
docker compose up --build
```

This starts:

- `manager` on port `50051`;
- `spawner`, which creates a new random vehicle every 1.5 seconds.

Follow the manager logs with:

```bash
docker compose logs -f manager
```

### Local (Go 1.26+)

Terminal 1 — manager:

```bash
go run ./cmd/manager
```

Terminal 2 — traffic:

```bash
go run ./cmd/spawner
```

Or run a single manually configured vehicle:

```bash
MANAGER_ADDR=localhost:50051 \
VEHICLE_ID=CAR_TEST \
START_SPEED=40 \
START_DISTANCE=400 \
ORIGIN_LANE=NORTH \
DIRECTION=STRAIGHT \
go run ./cmd/agent
```

On Windows PowerShell use `$env:NAME = "value"` before `go run`.

---

## Configuration

### `cmd/agent`

| Variable | Default | Description |
|---|---|---|
| `MANAGER_ADDR` | `localhost:50051` | Manager address |
| `VEHICLE_ID` | `CAR_MANUAL` | Vehicle identifier |
| `START_SPEED` | `40.0` | Initial speed in m/s |
| `START_DISTANCE` | `400.0` | Distance from the intersection in metres |
| `ORIGIN_LANE` | `UNKNOWN` | `NORTH`, `SOUTH`, `EAST`, `WEST` |
| `DIRECTION` | `UNKNOWN` | `STRAIGHT`, `LEFT`, `RIGHT` |

### `cmd/spawner`

| Variable | Default | Description |
|---|---|---|
| `MANAGER_ADDR` | `localhost:50051` | Manager address |

---

## Tests

```bash
go test ./...
go test -race ./...
```

The test suite currently covers:

- a full end-to-end negotiation over an in-memory gRPC connection;
- concurrent reservations of the same slot by 10 goroutines;
- two simultaneous gRPC clients competing for the same ETA.

The manager test uses a real TCP port (`50052`) for the concurrent gRPC test, so
that port must be free when the tests run.

---

## Regenerating the protobuf stubs

Requirements: `protoc`, `protoc-gen-go`, `protoc-gen-go-grpc`.

```bash
protoc \
  --go_out=. --go_opt=paths=source_relative \
  --go-grpc_out=. --go-grpc_opt=paths=source_relative \
  proto/intersection.proto
```

On Windows you can run `proto.bat` instead.

---

## Open issues / roadmap

- **#22** — Extend the trajectory conflict matrix to all turn movements.
- **#23** — Realistic acceleration/deceleration for the vehicle agent.
- **#24** — Preemption and priority for emergency vehicles.
- **#25** — Structured logging with `log/slog` and negotiation metrics.
- **#26** — Configurable traffic scenarios for the spawner (bursts, manoeuvre
  mix, deterministic seed).

---

## Known limitations

- `ReserveOrSuggest` does not currently exclude the requesting vehicle from the
  conflict scan. A vehicle that renegotiates on the same stream can therefore
  conflict with its own previous reservation. A multi-tick regression test is
  still missing.
- Reservations are kept for the whole trip (from the initial distance to the
  intersection), not only while the vehicle occupies the intersection.
- Rejected requests are not queued, so the protocol is not strictly FIFO.
- The simulation tick is not compensated for gRPC latency or scheduling delays.
- gRPC uses insecure credentials: the project is intended for local simulation
  only.

# Calcolo ETA

Se il veicolo sta ancora rallentando (o accelerando) per raggiungere $v_{\text{target}}$, inviare al Manager ETA = d / v sarebbe impreciso perché $v$ cambierà nei prossimi secondi. Calcoliamo invece il tempo reale diviso in due fasi:Tempo per raggiungere $v_{\text{target}}$:$$t_{\text{ramp}} = \frac{\vert{}v_{\text{target}} - v\vert{}}{a}$$(dove $a = \text{maxAccel}$ se $v_{\text{target}} > v$, oppure $a = \text{maxDecel}$ se $v_{\text{target}} < v$).Distanza percorsa durante la rampa:$$d_{\text{ramp}} = \frac{v + v_{\text{target}}}{2} \cdot t_{\text{ramp}}$$Due casi possibili:Caso 1 ($d_{\text{ramp}} \ge d$): Il veicolo raggiunge l'incrocio mentre sta ancora accelerando o frenando. Risolviamo l'equazione del moto $d = v \cdot t + \frac{1}{2} a_{\text{signed}} t^2$:$$\text{ETA} = \frac{-v + \sqrt{v^2 + 2 \cdot a_{\text{signed}} \cdot d}}{a_{\text{signed}}}$$Caso 2 ($d_{\text{ramp}} < d$): Il veicolo completa la rampa in $t_{\text{ramp}}$ e percorre la distanza rimanente $(d - d_{\text{ramp}})$ a velocità costante $v_{\text{target}}$:$$\text{ETA} = t_{\text{ramp}} + \frac{d - d_{\text{ramp}}}{v_{\text{target}}}$$