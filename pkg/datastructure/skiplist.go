package datastructure

import (
	"sync"

	"github.com/mihn1/mdb/pkg/utils"
)

type SkipList struct {
	head     *node
	tail     *node
	maxLevel int
	mu       sync.RWMutex
	cmp      Comparator
	_preds   []*node // Reusable slice for path tracking when insertion to avoid allocations
}

type node struct {
	key   []byte
	value []byte
	next  []*node // Array of forward pointers
	prev  []*node // Array of previous pointers
	// level int        // The level of the node (can be inferred from length of next and prev slices)
}

func NewSkipList(maxLevel int, cmp Comparator) *SkipList {
	utils.AssertMsg(maxLevel > 0, "maxLevel must be greater than 0")
	utils.AssertMsg(cmp != nil, "comparator must not be nil")

	tail := &node{
		prev: make([]*node, maxLevel),
	}
	head := &node{
		next: make([]*node, maxLevel),
	}
	for i := range maxLevel {
		head.next[i] = tail
		tail.prev[i] = head
	}

	return &SkipList{
		head:     head,
		tail:     tail,
		maxLevel: maxLevel,
		mu:       sync.RWMutex{},
		cmp:      cmp,
		_preds:   make([]*node, maxLevel),
	}
}

func (sl *SkipList) Get(key []byte) ([]byte, bool) {
	sl.mu.RLock()
	defer sl.mu.RUnlock()

	foundNode := sl.findNode(key)
	if foundNode != nil {
		return foundNode.value, true
	}

	return nil, false
}

func (sl *SkipList) Put(key []byte, value []byte) error {
	sl.mu.Lock()
	defer sl.mu.Unlock()

	cur := sl.head
	// Traverse from the highest node to the lowest
	for lvl := sl.maxLevel - 1; lvl >= 0; lvl-- {
		for {
			nextNode := cur.next[lvl]
			if nextNode == sl.tail {
				// Drop down
				break
			}

			cmp := sl.cmp.Compare(key, nextNode.key)
			if cmp == 0 {
				// Found the node, update it the return
				nextNode.value = value
				return nil
			} else if cmp > 0 {
				// Move cur to the right and add nextNode to path, and keep the same level
				cur = nextNode
				continue
			}
			// The key is smaller than the nextNode, keep going down
			break
		}
		sl._preds[lvl] = cur // Add current node to path
	}

	// Put the new node for the new key
	newLevel := utils.RandomLevel(sl.maxLevel)
	newNode := &node{
		key:   key,
		value: value,
		next:  make([]*node, newLevel),
		prev:  make([]*node, newLevel),
	}
	for lvl := range newLevel {
		// Update previous pointers
		pred := sl._preds[lvl]
		pred.next[lvl].prev[lvl] = newNode
		newNode.prev[lvl] = pred

		// Update forward pointers for the pred and the new nodes
		newNode.next[lvl] = pred.next[lvl]
		pred.next[lvl] = newNode
	}

	return nil
}

func (sl *SkipList) Delete(key []byte) (bool, error) {
	sl.mu.Lock()
	defer sl.mu.Unlock()

	node := sl.findNode(key)
	if node != nil {
		level := len(node.next)
		for i := range level {
			// Update forward and previous pointers
			node.prev[i].next[i] = node.next[i]
			node.next[i].prev[i] = node.prev[i]
		}
		return true, nil
	}

	return false, nil
}

// The caller should retain the lock before calling this function
func (sl *SkipList) findNode(key []byte) *node {
	cur := sl.head
	// Traverse from the highest node to the lowest
	for lvl := sl.maxLevel - 1; lvl >= 0; lvl-- {
		for {
			nextNode := cur.next[lvl]
			if nextNode == sl.tail {
				// Drop down
				break
			}

			cmp := sl.cmp.Compare(key, nextNode.key)
			if cmp == 0 {
				return nextNode
			} else if cmp > 0 {
				// move node to the next tower (to the right) and keep the same level
				cur = nextNode
				continue
			}
			// If key is smaller than the next node -> keep dropping down
			break
		}
	}

	return nil
}
