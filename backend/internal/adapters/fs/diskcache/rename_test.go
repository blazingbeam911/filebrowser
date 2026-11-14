package diskcache

import (
	"context"
	"testing"
)

func TestRename(t *testing.T) {
	ctx := context.Background()
	c, err := NewFileCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	load := func(key string) ([]byte, bool) {
		t.Helper()
		v, ok, err := c.Load(ctx, key)
		if err != nil {
			t.Fatal(err)
		}
		return v, ok
	}

	// Nothing under the old key: nothing happens.
	if moved, dropped, freed, err := c.Rename("absent", "new1"); err != nil || moved || dropped || freed != 0 {
		t.Fatalf("absent: moved=%v dropped=%v freed=%d err=%v", moved, dropped, freed, err)
	}
	if _, ok := load("new1"); ok {
		t.Fatal("absent: new key created")
	}

	// New key free: the value moves.
	if err := c.Store(ctx, "old2", []byte("thumb")); err != nil {
		t.Fatal(err)
	}
	if moved, dropped, freed, err := c.Rename("old2", "new2"); err != nil || !moved || dropped || freed != 0 {
		t.Fatalf("move: moved=%v dropped=%v freed=%d err=%v", moved, dropped, freed, err)
	}
	if v, ok := load("new2"); !ok || string(v) != "thumb" {
		t.Fatalf("move: new key holds %q (%v)", v, ok)
	}
	if _, ok := load("old2"); ok {
		t.Fatal("move: old key still there")
	}

	// Both present: the new value stays, the old one is deleted and counted.
	if err := c.Store(ctx, "old3", []byte("older")); err != nil {
		t.Fatal(err)
	}
	if err := c.Store(ctx, "new3", []byte("kept")); err != nil {
		t.Fatal(err)
	}
	if moved, dropped, freed, err := c.Rename("old3", "new3"); err != nil || moved || !dropped || freed != 5 {
		t.Fatalf("both: moved=%v dropped=%v freed=%d err=%v", moved, dropped, freed, err)
	}
	if v, _ := load("new3"); string(v) != "kept" {
		t.Fatalf("both: new key holds %q", v)
	}
	if _, ok := load("old3"); ok {
		t.Fatal("both: old key still there")
	}

	// An empty value (a failed attempt's marker) moves like any other.
	if err := c.Store(ctx, "old4", []byte{}); err != nil {
		t.Fatal(err)
	}
	if moved, _, _, err := c.Rename("old4", "new4"); err != nil || !moved {
		t.Fatalf("marker: moved=%v err=%v", moved, err)
	}
	if v, ok := load("new4"); !ok || len(v) != 0 {
		t.Fatalf("marker: new key holds %q (%v)", v, ok)
	}

	// An empty old value dropped next to a new one still counts as dropped.
	if err := c.Store(ctx, "old5", []byte{}); err != nil {
		t.Fatal(err)
	}
	if moved, dropped, freed, err := c.Rename("old5", "new3"); err != nil || moved || !dropped || freed != 0 {
		t.Fatalf("empty dropped: moved=%v dropped=%v freed=%d err=%v", moved, dropped, freed, err)
	}

	// Rename takes no scoped locks (Store took the others).
	c.scopedLocks.Lock()
	defer c.scopedLocks.Unlock()
	for _, k := range []string{"absent", "new1", "new2"} {
		if _, ok := c.scopedLocks.locks[k]; ok {
			t.Fatalf("lock created for %s", k)
		}
	}
}
