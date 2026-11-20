// Package digestset remembers a bounded number of content digests. The library
// uses it to skip repeating schema validation of content that already passed.
package digestset

import (
	"crypto/sha256"
	"slices"
	"sync"
)

// Digest is a SHA-256 content digest.
type Digest = [sha256.Size]byte

// Set remembers the most recent digests, evicting the oldest first. It is safe
// for concurrent use.
type Set struct {
	mu       sync.Mutex
	capacity int
	seen     map[Digest]struct{}
	order    []Digest
}

// New returns an empty set that holds at most capacity digests (at least one).
func New(capacity int) *Set {
	return &Set{capacity: max(capacity, 1), seen: map[Digest]struct{}{}}
}

// Contains reports whether digest is remembered.
func (d *Set) Contains(digest Digest) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, ok := d.seen[digest]
	return ok
}

// Add remembers digest, evicting the oldest digest when the set is full.
// Adding a remembered digest is a no-op.
func (d *Set) Add(digest Digest) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.seen[digest]; ok {
		return
	}
	if len(d.order) == d.capacity {
		delete(d.seen, d.order[0])
		d.order = slices.Delete(d.order, 0, 1)
	}
	d.seen[digest] = struct{}{}
	d.order = append(d.order, digest)
}

// Len returns the number of remembered digests.
func (d *Set) Len() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.order)
}
