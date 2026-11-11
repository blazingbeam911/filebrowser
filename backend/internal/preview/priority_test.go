package preview

import (
	"context"
	"testing"
	"time"
)

// waitUntil polls cond for up to 2 s.
func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for: %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// A browse request waiting for an image slot makes BrowsingBusy true, and it stays true for
// ForegroundGrace after the request ends; the job's own (Background) requests don't count.
func TestBrowsingBusy(t *testing.T) {
	oldGrace := ForegroundGrace
	ForegroundGrace = 80 * time.Millisecond
	defer func() { ForegroundGrace = oldGrace }()
	foregroundLastEnd.Store(0)

	// One image slot, already taken, so every GeneratePreviewWithMD5 call waits for it.
	svc := &Service{imageSem: make(chan struct{}, 1)}
	svc.imageSem <- struct{}{}
	useService(t, svc)
	file := writeImageFile(t, t.TempDir(), "a.png", "image/png", []byte("x"))

	if BrowsingBusy() {
		t.Fatal("busy before any request")
	}
	start := func(ctx context.Context) (context.CancelFunc, chan struct{}) {
		ctx, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() {
			_, _ = GeneratePreviewWithMD5(ctx, file, "small", "", 0, "hash")
			close(done)
		}()
		return cancel, done
	}

	cancel, done := start(Background(context.Background()))
	time.Sleep(20 * time.Millisecond)
	if BrowsingBusy() {
		t.Fatal("the job's own request counted as browsing")
	}
	cancel()
	<-done

	cancel, done = start(context.Background())
	waitUntil(t, "busy while a browse request waits for a slot", BrowsingBusy)
	cancel()
	<-done
	ended := time.Now()
	if !BrowsingBusy() {
		t.Fatal("not busy right after the browse request ended (grace)")
	}
	waitUntil(t, "idle after the grace time", func() bool { return !BrowsingBusy() })
	if since := time.Since(ended); since < 70*time.Millisecond {
		t.Fatalf("idle %v after the request, before the %v grace", since, ForegroundGrace)
	}
}

// CachedFromStat: every size present (and not the empty marker) = cached.
func TestCachedFromStat(t *testing.T) {
	svc := NewPreviewGenerator(2, t.TempDir())
	useService(t, svc)
	file := writeImageFile(t, t.TempDir(), "a.png", "image/png", []byte("x"))
	hash := cacheHashFor(file)
	sizes := []string{"small", "large"}
	check := func() bool { return CachedFromStat(file.RealPath, file.Size, file.ModTime, sizes) }
	ctx := context.Background()
	store := func(size string, n int) {
		if err := svc.fileCache.Store(ctx, CacheKey(hash, size, 0), make([]byte, n)); err != nil {
			t.Fatal(err)
		}
	}
	if check() {
		t.Fatal("cached with nothing stored")
	}
	store("small", 500)
	if check() {
		t.Fatal("cached with only the small size stored")
	}
	store("large", minPreviewSize-1)
	if check() {
		t.Fatal("cached with the large size an empty marker")
	}
	store("large", 500)
	if !check() {
		t.Fatal("not cached with both sizes stored")
	}
	if CachedFromStat(file.RealPath, file.Size, file.ModTime.Add(time.Nanosecond), sizes) {
		t.Fatal("cached for another modification time")
	}
}
