# Autonomous-Intersection-Manager
A distributed V2X (Vehicle-to-Everything) simulation engine. AIM replaces traditional time-based traffic lights with a real-time, reservation-based consensus protocol. Built with Go and gRPC, this project demonstrates how autonomous vehicles can dynamically negotiate intersection access to maximize throughput and ensure zero collisions.

gRPC è un server altamente concorrente. Ogni volta che un veicolo chiama Negotiate, gRPC crea una Goroutine separata (un thread leggero) per gestire quello stream. Se due auto si connettono contemporaneamente, due Goroutine proveranno a scrivere nella stessa mappa nello stesso millisecondo. In Go, le scritture concorrenti su una mappa causano un panic istantaneo (il server crasha). Il Mutex (Mutual Exclusion) è un semaforo che dice alle Goroutine: "Solo una alla volta può toccare questa mappa".


