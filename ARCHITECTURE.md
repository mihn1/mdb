%%{init: {
  "themeVariables": { "fontSize": "20px" }
}}%%
flowchart LR
  %% Styles
  classDef write fill:#e8f5e9,stroke:#2e7d32,stroke-width:1px;
  classDef read fill:#e3f2fd,stroke:#1565c0,stroke-width:1px;
  classDef shared fill:#f5f5f5,stroke:#616161,stroke-width:1px;
  classDef optional fill:#eef,stroke:#88f,stroke-width:1px;

  %% Client and API (shared)
  subgraph Client
    app[App / CLI / Tests]:::shared
  end
  app --> api[DB API: Get, Put, Delete, Iterate]:::shared

  %% Write path nodes
  subgraph Write[Write path]
    wal[WAL - append-only]:::write
    l0[SSTable L0]:::write
    l1[L1]:::write
    l2[L2+]:::write
  end

  %% Read path nodes
  subgraph Read[Read path]
  probe[[Candidate SSTable]]:::read
  bf{Bloom filter}:::optional
  cache{Block cache}:::optional
  sst[SSTable seek]:::read
  ret[Return value]:::shared
  nf[Not found]:::shared
  end

  %% Shared MemTable
  mem[MemTable]:::shared

  %% Write edges (solid)
  api -->|Write| wal
  wal --> mem
  mem -->|Flush when full| l0
  l0 --> l1 --> l2

  %% Read edges (dotted)
  api -.->|Read| mem
  mem -.->|Hit| ret
  %% On miss in MemTable, begin per-SSTable candidate loop
  mem -.->|Miss| probe

  %% Per-SSTable read flow
  probe -.-> bf
  bf -.->|No| probe
  bf -.->|Maybe| cache
  cache -.->|Hit| ret
  cache -.->|Miss| sst
  sst -.->|Found| ret
  sst -.->|Miss:<br/>next candidate| probe
  %% When there are no more candidates
  probe -.->|No more<br/>candidates| nf

  %% Background compaction (dotted)
  subgraph Background
    comp[Compaction worker]:::shared
  end
  comp -.-> l0
  comp -.-> l1
  comp -.-> l2