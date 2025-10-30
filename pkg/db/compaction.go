package db

import (
	"container/heap"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync/atomic"
	"time"

	"github.com/mihn1/mdb/pkg/common"
	"github.com/mihn1/mdb/pkg/sstable"
)

// Compactor coordinates size-tiered compaction of SSTables.
type Compactor struct {
	opts *common.Options
}

// NewCompactor creates a compactor using the default fan-in of four tables per merge.
func NewCompactor(opts *common.Options) *Compactor {
	return &Compactor{opts: opts}
}

func (c *Compactor) Compact(db *DB) error {
	// Keep picking and executing compaction plans until no tier needs compaction
	for {
		plan := c.pickCompactionTarget(db)
		if plan == nil {
			return nil
		}
		if err := c.executeCompaction(db, plan); err != nil {
			db.tablesMu.Lock()
			db.releaseCompacting(plan.metas)
			db.tablesMu.Unlock()
			return err
		}
	}
}

type compactionPlan struct {
	level int
	metas []*tableMeta
}

// Pick compaction targets based on size-tiered strategy
// and remove them from the DB's table list.
func (c *Compactor) pickCompactionTarget(db *DB) *compactionPlan {
	db.tablesMu.Lock()
	defer db.tablesMu.Unlock()
	if len(db.tables) == 0 {
		return nil
	}
	levelGroups := make(map[int][]*tableMeta)
	for _, meta := range db.tables {
		if meta.compacting {
			continue
		}
		levelGroups[meta.Level] = append(levelGroups[meta.Level], meta)
	}
	levels := make([]int, 0, len(levelGroups))
	for lvl := range levelGroups {
		levels = append(levels, lvl)
	}
	sort.Ints(levels)
	for _, lvl := range levels {
		metas := levelGroups[lvl]
		if len(metas) < c.opts.CompactionFanIn {
			continue
		}
		sort.Slice(metas, func(i, j int) bool {
			if metas[i].Seq == metas[j].Seq {
				return metas[i].CreatedAt.Before(metas[j].CreatedAt)
			}
			return metas[i].Seq < metas[j].Seq
		})
		selected := make([]*tableMeta, c.opts.CompactionFanIn)
		copy(selected, metas[:c.opts.CompactionFanIn])
		db.markCompacting(selected)
		return &compactionPlan{level: lvl, metas: selected}
	}
	return nil
}

type compactionInput struct {
	meta   *tableMeta
	file   *os.File
	reader common.Reader
}

func (c *Compactor) executeCompaction(db *DB, plan *compactionPlan) error {
	inputs := make([]compactionInput, len(plan.metas))
	tablesDir := db.getTablesDir()
	for i, meta := range plan.metas {
		path := filepath.Join(tablesDir, meta.FileName)
		f, err := os.Open(path)
		if err != nil {
			c.closeInputs(inputs[:i])
			return err
		}
		tbl, err := sstable.Open(f, db.opts)
		if err != nil {
			f.Close()
			c.closeInputs(inputs[:i])
			return err
		}
		reader := tbl.NewReader()
		reader.SeekToFirst()
		inputs[i] = compactionInput{
			meta:   meta,
			file:   f,
			reader: reader,
		}
	}
	newMeta, err := c.mergeInputs(db, plan.level+1, inputs)
	c.closeInputs(inputs)
	if err != nil {
		return err
	}
	paths := make([]string, len(plan.metas))
	for i, meta := range plan.metas {
		paths[i] = filepath.Join(tablesDir, meta.FileName)
	}
	db.tablesMu.Lock()
	db.removeTables(plan.metas)
	if newMeta != nil {
		db.addAndTidyTables(newMeta)
	}
	db.tablesMu.Unlock()
	for _, path := range paths {
		_ = os.Remove(path)
	}
	return nil
}

func (c *Compactor) closeInputs(inputs []compactionInput) {
	for i := range inputs {
		if inputs[i].file != nil {
			inputs[i].file.Close()
		}
	}
}

type heapItem struct {
	key   []byte
	seq   uint64
	index int
}

type iteratorHeap struct {
	items []*heapItem
	cmp   common.Comparator
}

func (h iteratorHeap) Len() int { return len(h.items) }

func (h iteratorHeap) Less(i, j int) bool {
	cmp := h.cmp.Compare(h.items[i].key, h.items[j].key)
	if cmp != 0 {
		return cmp < 0
	}
	return h.items[i].seq > h.items[j].seq
}

func (h iteratorHeap) Swap(i, j int) {
	h.items[i], h.items[j] = h.items[j], h.items[i]
}

func (h *iteratorHeap) Push(x any) {
	h.items = append(h.items, x.(*heapItem))
}

func (h *iteratorHeap) Pop() any {
	old := h.items
	n := len(old)
	item := old[n-1]
	h.items = old[:n-1]
	return item
}

func (h *iteratorHeap) Peek() *heapItem {
	if len(h.items) == 0 {
		return nil
	}
	return h.items[0]
}

func (c *Compactor) mergeInputs(db *DB, level int, inputs []compactionInput) (*tableMeta, error) {
	heapItems := &iteratorHeap{cmp: db.opts.Comparator}
	for idx := range inputs {
		inputs[idx].reader.SeekToFirst()
		if inputs[idx].reader.Valid() {
			key, err := inputs[idx].reader.Key()
			if err != nil {
				return nil, err
			}
			heap.Push(heapItems, &heapItem{
				key:   append([]byte(nil), key...),
				seq:   inputs[idx].meta.Seq,
				index: idx,
			})
		}
	}
	if heapItems.Len() == 0 {
		return nil, nil
	}
	dir := db.getTablesDir()
	seq := atomic.AddUint64(&db.globalSeq, 1) - 1
	ts := time.Now().UnixNano()
	filePath := filepath.Join(dir, fmt.Sprintf("sstable-%d-%d-%d.sst", level, seq, ts))
	tmpPath := filePath + ".tmp"
	file, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return nil, err
	}
	builder, err := sstable.NewTableBuilder(file, db.opts)
	if err != nil {
		file.Close()
		os.Remove(tmpPath)
		return nil, err
	}
	wroteEntries := false
	for heapItems.Len() > 0 {
		item := heap.Pop(heapItems).(*heapItem)
		input := inputs[item.index]
		key := item.key
		value, err := input.reader.Value()
		if err != nil {
			file.Close()
			os.Remove(tmpPath)
			return nil, err
		}
		duplicates := []*heapItem{item}
		for {
			next := heapItems.Peek()
			if next == nil || db.opts.Comparator.Compare(next.key, key) != 0 {
				break
			}
			duplicates = append(duplicates, heap.Pop(heapItems).(*heapItem))
		}
		if len(value) == 0 || value[len(value)-1] != byte(common.TypeTombstone) {
			if err := builder.Add(key, value); err != nil {
				file.Close()
				os.Remove(tmpPath)
				return nil, err
			}
			wroteEntries = true
		}
		for _, dup := range duplicates {
			iter := inputs[dup.index].reader
			iter.Next()
			if iter.Valid() {
				nextKey, err := iter.Key()
				if err != nil {
					file.Close()
					os.Remove(tmpPath)
					return nil, err
				}
				heap.Push(heapItems, &heapItem{
					key:   append([]byte(nil), nextKey...),
					seq:   inputs[dup.index].meta.Seq,
					index: dup.index,
				})
			}
		}
	}
	if !wroteEntries {
		file.Close()
		os.Remove(tmpPath)
		return nil, nil
	}
	if err := builder.Finish(); err != nil {
		file.Close()
		os.Remove(tmpPath)
		if errors.Is(err, sstable.ErrEmptyTable) {
			return nil, nil
		}
		return nil, err
	}
	if err := os.Rename(tmpPath, filePath); err != nil {
		return nil, err
	}
	info, err := os.Stat(filePath)
	if err != nil {
		return nil, err
	}
	return &tableMeta{
		FileName:  filepath.Base(filePath),
		Size:      info.Size(),
		Level:     level,
		Seq:       seq,
		CreatedAt: time.Now(),
	}, nil
}
