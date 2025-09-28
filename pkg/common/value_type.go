package common

// ValueType represents the type of value (value or tombstone)
type ValueType byte

const (
	// TypeValue indicates a regular value
	TypeValue ValueType = 0x1
	// TypeTombstone indicates a deleted key (tombstone)
	TypeTombstone ValueType = 0x0
)
