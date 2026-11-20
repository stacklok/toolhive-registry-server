package digestset

import (
	"crypto/sha256"
	"strconv"
	"testing"
)

func TestSetEvictsOldest(t *testing.T) {
	t.Parallel()
	const capacity = 8
	d := New(capacity)
	digest := func(i int) Digest { return sha256.Sum256([]byte(strconv.Itoa(i))) }
	for i := range capacity + 1 {
		d.Add(digest(i))
	}
	d.Add(digest(capacity)) // re-adding a present digest is a no-op
	if d.Contains(digest(0)) {
		t.Fatal("oldest digest was not evicted")
	}
	for i := 1; i <= capacity; i++ {
		if !d.Contains(digest(i)) {
			t.Fatalf("digest %d missing", i)
		}
	}
	if d.Len() != capacity || len(d.seen) != capacity {
		t.Fatalf("size %d/%d", d.Len(), len(d.seen))
	}
}
