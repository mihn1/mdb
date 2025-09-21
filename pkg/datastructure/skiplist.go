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
	}
}

func (sl *SkipList) Search(key []byte) (any, bool) {
	sl.mu.RLock()
	defer sl.mu.RUnlock()
	path, found := sl.findLessThanOrEqualPath(key)
	if found {
		return path[len(path)-1][0].value, true
	}
	return nil, false
}

func (sl *SkipList) Insert(key []byte, value any) error {
	sl.mu.Lock()
	defer sl.mu.Unlock()
	path, found := sl.findLessThanOrEqualPath(key)
	if found {
		lastTower := path[len(path)-1]
		// Key already exists, update the value
		lastTower[0].value = value // Only update value at the lowest level
		return nil
	}

	// Insert new tower
	level := sl.randomLevel()
	i := 0
	newTower := make([]*slNode, level)

	for i < level {
		// Pop the last element from path
		prevTower := path[len(path)-1]
		path = path[:len(path)-1]
		// Insert the new node at this level
		for _, prevNode := range prevTower[i:] {
			newNode := &slNode{
				key:   key,
				value: value,
				next:  prevNode.next,
				level: level,
			}
			newTower[i] = newNode
			prevNode.next = newTower
			i++
			if i >= level {
				break
			}
		}
	}

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

// Return the path to get the node having greater or equal compared key, and a bool indicating whether the key exists
func (sl *SkipList) findLessThanOrEqualPath(key []byte) ([][]*slNode, bool) {
	path := make([][]*slNode, 0, sl.maxLevel/2)
	curTower := sl.head

	for {
		path = append(path, curTower)

		// Traverse from the highest level to the lowest level
		for i := curTower[0].level - 1; i >= -1; i-- {
			if i < 0 {
				// Reached the lowest level, key doesn't exist
				return path, false
			}
			curNode := curTower[i]
			nextTower := curNode.next
			if nextTower[i] == sl.tail[i] {
				continue
			}
			cmpResult := sl.cmp.Compare(key, nextTower[i].key)
			if cmpResult < 0 || nextTower[i] == sl.tail[i] {
				// next node is larger then the key
				// move down one level
				continue
			} else if cmpResult == 0 {
				// key found -> return the path
				path = append(path, nextTower)
				return path, true
			}
			// key is larger then the next tower -> safe to break here and move to next tower
			curTower = nextTower
			break
		}

		// curTower = curTower[0].next
		if curTower[0] == nil || curTower[0] == sl.tail[0] {
			break
		}
	}

	// key doesn't exist
	return path, false
}

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
