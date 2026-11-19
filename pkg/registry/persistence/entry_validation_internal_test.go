package persistence

import (
	"crypto/sha256"
	"strconv"
	"testing"
)

func TestDigestSetEvictsOldest(t *testing.T) {
	t.Parallel()
	d := &digestSet{seen: map[[sha256.Size]byte]struct{}{}}
	digest := func(i int) [sha256.Size]byte { return sha256.Sum256([]byte(strconv.Itoa(i))) }
	for i := range validatedSnapshotCapacity + 1 {
		d.add(digest(i))
	}
	d.add(digest(validatedSnapshotCapacity)) // re-adding a present digest is a no-op
	if d.contains(digest(0)) {
		t.Fatal("oldest digest was not evicted")
	}
	for i := 1; i <= validatedSnapshotCapacity; i++ {
		if !d.contains(digest(i)) {
			t.Fatalf("digest %d missing", i)
		}
	}
	if len(d.seen) != validatedSnapshotCapacity || len(d.order) != validatedSnapshotCapacity {
		t.Fatalf("size %d/%d", len(d.seen), len(d.order))
	}
}
