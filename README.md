# MDB: Log-Structured Merge Tree Storage Engine

MDB implements a persistent key-value store in Go using a Log-Structured Merge (LSM) architecture. The engine combines a WAL-protected memtable, immutable SSTables with Bloom filters, and background size-tiered compaction to deliver high write throughput while keeping read latency predictable. The repo also ships with benchmarking scenarios and an interactive REPL.

## Repository Highlights
- `pkg/` – Core engine packages (memtable, WAL, SSTables, compaction, Bloom filters).
- `demo/` – Deterministic workload runner plus the interactive shell for live exploration.

## Requirements
- Go 1.24+ (project uses the Go 1.24 toolchain declared in `go.mod`).
- Linux or macOS (tested on Linux; paths assume a POSIX shell).

## Quick Start
```bash
# Clone and enter the repo
git clone https://github.com/mihn1/mdb.git
cd mdb

# Run the scenario suite (basic, read/write benches, etc.)
cd demo
go run .
```
The runner resets its data directory between scenarios unless you pass `--keep_data`.

## Run The Interactive Demo
Launch the REPL to seed a database and issue commands by hand:
```bash
cd demo
go run ./interactive_demo --reset --bloom=true --preload=10000
```
Key flags:
- `--db_path`: where the demo stores its database (defaults to `./demo_runs/interactive`).
- `--preload`: number of deterministic keys to seed on startup.
- `--bloom`: toggle Bloom filters so you can compare hit/miss behavior.
- `--reset`: wipe the target directory before seeding (recommended on first run).

Once running, use commands like `put key value`, `get key`, `del key`, `preload 5000`, and `stats` to watch memtable sizes, flush counts, and SSTable growth in real time.
