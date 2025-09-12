package cache

// BlockCache provides LRU caching for SSTable blocks
type BlockCache struct {
	// TODO: Implement LRU cache
}

// Create a new block cache
func New(capacity int) *BlockCache {
	return &BlockCache{}
}

// Get a block from cache
func (c *BlockCache) Get(key string) ([]byte, bool) {
	// TODO: Implement LRU get
	return nil, false
}

// Put a block in cache
func (c *BlockCache) Put(key string, value []byte) {
	// TODO: Implement LRU put with eviction
}
