# MDB – LSM-Tree Storage Engine in Go

A high-performance, persistent key–value storage engine in Go based on a Log-Structured Merge-Tree (LSM-tree) architecture popularized by LevelDB.

## Project Overview

Build a storage engine from scratch in Go using LSM-trees to optimize write throughput while maintaining solid read performance and good space efficiency via compaction.

## Architecture Diagram


## Motivation

Traditional B-tree engines excel at read-heavy workloads but struggle with write-intensive scenarios. LSM-trees address this by:

- Optimizing writes with append-only operations
- Preserving good read performance via efficient indexing
- Improving space utilization through background compaction
- Enabling high concurrency with minimal locking

This architecture powers production systems such as Cassandra, RocksDB, and LevelDB.

## Core Technical Components

### 1. In-Memory Layer (MemTable)

- Skip list or self-balancing binary search tree
- O(log n) inserts and lookups
- Automatically flushed to disk when a size limit is exceeded

### 2. Persistent Storage (SSTable)

- Each immutable SSTable stores a sorted range of key–value pairs
- Data organized into blocks (optionally compressed) plus index blocks
- Updates/deletes append newer entries or tombstones that shadow older ones
- Multi-level layout (level N+1 is larger than level N)
- Read path: check MemTable, then probe SSTables from lower to higher levels

### 3. Write-Ahead Log (WAL)

- Append every write before inserting into the MemTable (crash recovery)
- Recovery by replaying unflushed log records

### 4. Background Compaction

- Background workers merge overlapping SSTables and discard obsolete entries

### 5. Performance Optimizations (optional)

- Per-SSTable Bloom filter to avoid unnecessary disk reads
- LRU block cache for hot data blocks
- Compression

## Project Scope

### Included Features

- Core operations: Get, Put, Delete, Iterator
- Multi-level LSM-tree with background compaction
- Optional optimizations: Bloom filter, compression, snapshot

### Simplifications (graduate project)

- Single-node engine (no distributed features)
- Basic size-tiered compaction strategy
- Only essential optimizations

## Deliverables

- Complete Go implementation of the engine
- Test suite and benchmarking framework
- Performance analysis across configurations
- Technical documentation of architecture and design choices
- Working demo application (CLI or minimal web UI)

## Weekly Implementation Plan (12 weeks)

### Weeks 1–2: Foundation and Architecture

Goal: Set up project structure and baseline architecture.

- Design overall architecture and module components
- Write project description and design documentation
- Initialize project with proper Go modules
- Implement basic Options/configuration system
- Define core DB operations: Put/Get/Delete/Iterate

### Week 3: MemTable Implementation

Goal: Build the core in-memory sorted structure.

- Implement skip list or self-balancing tree (e.g., AVL or red–black)
- Create a MemTable supporting Put/Get/Delete/Iterate
- Rotate to a new MemTable when the size limit is exceeded
- Ensure thread-safe writes

### Week 4: SSTable Format and Writer

Goal: Persistent storage format.

- Design the SSTable file format (data and metadata sections)
- Implement an SSTable builder to flush a MemTable to disk
- Organize data in blocks with index blocks
- Add basic compression

### Week 5: SSTable Reader

Goal: Reading from disk and basic DB operations.

- Implement efficient key lookup using binary search
- Create an optional block cache for performance
- Implement Get/Delete/Iterator across SSTables
- Add basic tests

### Week 6: Write-Ahead Log (WAL)

Goal: Durability and crash recovery.

- Implement a WAL writer (append-only log)
- Write path: WAL append → MemTable write
- Implement crash recovery by replaying WAL entries
- Clean up logs when a MemTable is flushed/rotated

### Week 7: Core Database Functionality

Goal: Integrate components and main operations.

- Integrate MemTable, WAL, and SSTables
- Implement basic snapshot support (optional)
- Add batch write capability

### Weeks 8–9: Background Compaction

Goal: Compaction framework and multi-level compaction.

- Design an initial size-tiered compaction policy
- Implement a background compaction worker
- Add level-based compaction and handle overlapping key ranges
- Expose compaction statistics, logging, and monitoring

### Week 10: Bloom Filters and Optimizations

Goal: Query optimization features.

- Implement Bloom filter creation and querying
- Integrate Bloom filters with SSTable reads
- Measure performance improvements

### Week 11: Advanced Features and Demo

Goal: Production-ready features and demonstration.

- Harden error handling
- Add configuration options
- Create detailed logging and diagnostics
- Build a demo application (CLI or minimal web UI)

### Week 12: Testing, Benchmarking, and Documentation

Goal: Validation and documentation.

- Benchmark performance against a LevelDB baseline
- Stress test with concurrent reads/writes
- Complete documentation and usage examples
- Final project report with performance analysis

## Key Milestones

- Week 2: Final design and implementation plan
- Week 5: Persistent read/write operations working
- Week 7: Complete database interface with core operations
- Week 9: Background compaction fully operational
- Week 12: Tested engine with demo UI and comprehensive documentation
