package common

import (
	"errors"

	"github.com/mihn1/mdb/pkg/utils"
)

type SkipList struct {
	head     *node
	tail     *node
	maxLevel int
	size     uint64 // Number of bytes for keys and values estimated in the skiplist
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
		cmp:      cmp,
		_preds:   make([]*node, maxLevel),
	}
}

func (sl *SkipList) Get(key []byte) ([]byte, bool) {
	foundNode := sl.FindNode(key)
	if foundNode != nil {
		return foundNode.value, true
	}

	return nil, false
}

func (sl *SkipList) Put(key []byte, value []byte) error {
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
				sl.size += uint64(len(value) - len(nextNode.value))
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

	// Update size
	sl.size += uint64(len(key) + len(value))
	return nil
}

func (sl *SkipList) Delete(key []byte) (bool, error) {
	node := sl.FindNode(key)
	if node != nil {
		level := len(node.next)
		for i := range level {
			// Update forward and previous pointers
			node.prev[i].next[i] = node.next[i]
			node.next[i].prev[i] = node.prev[i]
		}
		sl.size -= uint64(len(node.key) + len(node.value))
		return true, nil
	}

	return false, nil
}

// The caller should retain the lock before calling this function
func (sl *SkipList) FindNode(key []byte) *node {
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

// Return the approximate size in bytes
func (sl *SkipList) Size() uint64 {
	return sl.size
}

func (sl *SkipList) Reader() Reader {
	return newSkipListIterator(sl)
}

type skipListIterator struct {
	sl      *SkipList
	current *node
}

func newSkipListIterator(sl *SkipList) *skipListIterator {
	return &skipListIterator{
		sl:      sl,
		current: sl.head.next[0], // Start at the first element
	}
}

func (it *skipListIterator) Valid() bool {
	return it.current != it.sl.tail && it.current != it.sl.head
}

func (it *skipListIterator) Key() ([]byte, error) {
	if !it.Valid() {
		return nil, errors.New("iterator not valid")
	}
	return it.current.key, nil
}

func (it *skipListIterator) Value() ([]byte, error) {
	if !it.Valid() {
		return nil, errors.New("iterator not valid")
	}
	return it.current.value, nil
}

func (it *skipListIterator) Next() {
	if it.current != it.sl.tail {
		it.current = it.current.next[0]
	}
}

func (it *skipListIterator) Seek(key []byte) {
	foundNode := it.sl.FindNode(key)
	if foundNode != nil {
		it.current = foundNode
	} else {
		it.current = it.sl.tail
	}
}

func (it *skipListIterator) SeekToFirst() {
	it.current = it.sl.head.next[0]
}
