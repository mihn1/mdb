package compaction

import "github.com/mihn1/mdb/pkg/sstable"

// Compactor handles background compaction
type Compactor struct {
	// TODO: Add fields for compaction strategy
}

// Create a new compactor
func New() *Compactor {
	return &Compactor{}
}

// Merge SSTables and removes obsolete entries
func (c *Compactor) Compact(tables []*sstable.Table) ([]*sstable.Table, error) {
	// TODO: Implement compaction
	return nil, nil
}

// Determine if compaction is needed
func (c *Compactor) ShouldCompact(level int, tableCount int) bool {
	// TODO: Implement compaction trigger logic
	return false
}
