package datastructure

import (
	"strconv"
	"sync"

	"github.com/mihn1/mdb/pkg/utils"
)

type DbInterator interface {
	Insert(key []byte, value any) error
	Search(key []byte) (any, bool)
	Delete(key []byte) error
}

type SkipList struct {
	head     []*slNode
	tail     []*slNode
	maxLevel int
	mu       sync.RWMutex
	cmp      Comparator
	_preds   [][]*slNode // Reusable slice for path tracking when insertion to avoid allocations
}

type slNode struct {
	key   []byte
	value any
	next  []*slNode // Array of forward pointers
	level int       // Height of the tower
}

func NewSkipList(maxLevel int, cmp Comparator) *SkipList {
	utils.AssertMsg(maxLevel > 0, "maxLevel must be greater than 0")
	utils.AssertMsg(cmp != nil, "comparator must not be nil")

	tail := make([]*slNode, maxLevel)
	head := make([]*slNode, maxLevel)

	for i := range maxLevel {
		head[i] = &slNode{
			next:  tail,
			level: maxLevel,
		}
		tail[i] = &slNode{
			next:  nil,
			level: maxLevel,
		}
	}

	return &SkipList{
		head:     head,
		tail:     tail,
		maxLevel: maxLevel,
		mu:       sync.RWMutex{},
		cmp:      cmp,
		_preds:   make([][]*slNode, maxLevel),
	}
}

func (sl *SkipList) Search(key []byte) (any, bool) {
	sl.mu.RLock()
	defer sl.mu.RUnlock()
	if tower, ok := sl.findEqual(key); ok {
		return tower[0].value, true
	}
	return nil, false
}

func (sl *SkipList) Insert(key []byte, value any) error {
	sl.mu.Lock()
	defer sl.mu.Unlock()
	// Find predecessors for each level via top-down scan
	cur := sl.head
	for lvl := sl.maxLevel - 1; lvl >= 0; lvl-- {
		for {
			nextTower := cur[lvl].next
			if nextTower[lvl] == sl.tail[lvl] {
				break
			}
			cmp := sl.cmp.Compare(key, nextTower[lvl].key)
			if cmp > 0 {
				cur = nextTower
				continue
			}
			if cmp == 0 {
				// Key exists, update value at level 0
				nextTower[0].value = value
				return nil
			}
			break
		}
		sl._preds[lvl] = cur
	}

	// Insert new tower with random height
	level := sl.randomLevel()
	newTower := make([]*slNode, level)
	for i := range level {
		prev := sl._preds[i]
		next := prev[i].next
		newNode := &slNode{
			key:   key,
			next:  next,
			level: level,
		}
		newTower[i] = newNode
		prev[i].next = newTower
	}
	newTower[0].value = value // Only set value at level 0

	return nil
}

func (sl *SkipList) Delete(key []byte) error {
	sl.mu.Lock()
	defer sl.mu.Unlock()

	// Unlink the target tower from all levels without relying on the path stack.
	cur := sl.head
	for lvl := sl.maxLevel - 1; lvl >= 0; lvl-- {
		for {
			nextTower := cur[lvl].next
			// Reached tail at this level
			if nextTower[lvl] == sl.tail[lvl] {
				break
			}
			cmp := sl.cmp.Compare(key, nextTower[lvl].key)
			if cmp > 0 {
				// advance horizontally on this level
				cur = nextTower
				continue
			}
			if cmp == 0 {
				// unlink at this level
				cur[lvl].next = nextTower[lvl].next
			}
			// cmp < 0 or after unlink: drop down a level
			break
		}
	}
	return nil
}

// Searches for a tower with the given key without
func (sl *SkipList) findEqual(key []byte) ([]*slNode, bool) {
	cur := sl.head
	for lvl := sl.maxLevel - 1; lvl >= 0; lvl-- {
		for {
			nextTower := cur[lvl].next
			// At tail for this level; drop down
			if nextTower[lvl] == sl.tail[lvl] {
				break
			}
			cmp := sl.cmp.Compare(key, nextTower[lvl].key)
			if cmp > 0 {
				cur = nextTower
				continue
			}
			if cmp == 0 {
				return nextTower, true
			}
			// cmp < 0: drop down
			break
		}
	}
	return nil, false
}

// Return the path to get the node having greater or equal compared key, and a bool indicating whether the key exists

func (sl *SkipList) randomLevel() int {
	level := 1
	for level < sl.maxLevel && utils.RandomBool() {
		level++
	}
	return level
}

func (sl *SkipList) PrettyPrint() string {
	sl.mu.RLock()
	defer sl.mu.RUnlock()

	result := ""
	curTower := sl.head[0].next
	for curTower[0] != sl.tail[0] {
		result += string(curTower[0].key) + ":"
		if valStr, ok := curTower[0].value.(string); ok {
			result += valStr
		} else {
			result += "?"
		}
		result += "[L" + strconv.Itoa(curTower[0].level) + "]"
		result += " -> "
		curTower = curTower[0].next
	}
	result += "nil"
	return result
}
