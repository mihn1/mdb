# MDB Scenario Runner

Consolidated demos for MDB live in this directory. The runner focuses on deterministic scenarios that exercise correctness and sustained load so we can compare future optimizations against a stable baseline.

## Quick start

```bash
cd demo
go run .
```

The default invocation runs every registered scenario inside `./demo_runs`. The runner resets scenario data between executions unless you pass `--keep_data`.

### Interactive demo

```bash
cd demo
go run ./interactive_demo --reset
```

The interactive shell seeds ~10,000 synthetic keys on a fresh database (controllable via `--preload`) and drops you into an `mdb>` prompt. Use commands like `get`, `put`, `del`, `preload <count>`, and `stats` to explore the engine in real time. Flags such as `--db_path`, `--bloom`, and `--reset` control where the data lives and whether Bloom filters are enabled.

## Available flags

- `--scenario=<name|all>`: choose `basic`, `high_load`, or `all` (default).
- `--base_dir=<path>`: where scenario databases and artifacts are stored (`./demo_runs` by default).
- `--keep_data`: skip cleanup so you can inspect the generated files afterward.
- `--report=<path>`: write a JSON summary that captures metrics for downstream dashboards.
- `--seed=<n>`: control randomness for reproducible stress runs.
- `--timeout=<duration>`: cancel scenarios that exceed the provided limit (for example `--timeout=45s`).

## Scenarios

### basic
- Sequentially writes 1,500 key/value pairs and verifies them in reverse order.
- Uses a 8 KB memtable so flushes and SSTable creation are easy to observe.
- Reports verification errors, total flushes, final SSTable count, and derived write/read throughput.

### high_load
- Launches 24 writers and 12 readers against a shared database.
- Writers push 800 keys each with randomized values; readers sample keys while writers run.
- Uses a 24 KB memtable to force multiple flush/compaction cycles.
- Surfaces write/read throughput, total flushes, SSTables, and a sample of any request errors encountered.

## JSON reports

When `--report` is supplied the runner emits output similar to:

```json
{
  "generated_at": "2024-07-01T17:22:11Z",
  "results": [
    {
      "name": "basic",
      "duration_ms": 180,
      "items_written": 1500,
      "items_read": 1500,
      "flushes": 6,
      "sstables": 6
    },
    {
      "name": "high_load",
      "duration_ms": 920,
      "items_written": 9600,
      "items_read": 34800,
      "flushes": 22,
      "sstables": 19
    }
  ]
}
```

These artifacts make it easy to chart trends and keep regression tests aligned with the week-10 optimization plan.