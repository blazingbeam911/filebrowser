package thumbjob

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeBackend serves files[prefix] as sorted paths; process decides each file's outcome.
type fakeBackend struct {
	mu      sync.Mutex
	files   map[string][]string
	process func(ctx context.Context, spec Spec, path string) (Outcome, error)
	seen    []string // "jobpath:file" in processing order
}

func (b *fakeBackend) Count(spec Spec) (int, error) {
	if spec.Prefix == "/broken/" {
		return 0, errors.New("db gone")
	}
	return len(b.files[spec.Prefix]), nil
}

func (b *fakeBackend) Page(spec Spec, after string, limit int) ([]string, error) {
	all := append([]string(nil), b.files[spec.Prefix]...)
	sort.Strings(all)
	var out []string
	for _, p := range all {
		if p > after && len(out) < limit {
			out = append(out, p)
		}
	}
	return out, nil
}

func (b *fakeBackend) Process(ctx context.Context, spec Spec, path string) (Outcome, error) {
	b.mu.Lock()
	b.seen = append(b.seen, spec.Prefix+":"+path)
	b.mu.Unlock()
	if b.process != nil {
		return b.process(ctx, spec, path)
	}
	return Generated, nil
}

func (b *fakeBackend) processed() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.seen...)
}

func filesUnder(prefix string, n int) []string {
	var out []string
	for i := 0; i < n; i++ {
		out = append(out, fmt.Sprintf("%sf%04d.png", prefix, i))
	}
	return out
}

func waitFor(t *testing.T, m *Manager, id string, states ...State) Status {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		st, ok := m.Get(id)
		if !ok {
			t.Fatalf("job %s not found", id)
		}
		for _, s := range states {
			if st.State == s {
				return st
			}
		}
		time.Sleep(2 * time.Millisecond)
	}
	st, _ := m.Get(id)
	t.Fatalf("job %s: state %s, want %v", id, st.State, states)
	return st
}

// Outcomes are counted over several pages of files; the job ends done.
func TestJobCountsOutcomesAcrossPages(t *testing.T) {
	b := &fakeBackend{files: map[string][]string{"/a/": filesUnder("/a/", 1203)}}
	b.process = func(_ context.Context, _ Spec, path string) (Outcome, error) {
		switch {
		case strings.HasSuffix(path, "0.png"):
			return Cached, nil
		case strings.HasSuffix(path, "1.png"):
			return Skipped, nil
		case strings.HasSuffix(path, "2.png"):
			return Failed, errors.New("bad image")
		}
		return Generated, nil
	}
	var mu sync.Mutex
	var events []Status
	m := New(b, func(user string, st Status) {
		mu.Lock()
		events = append(events, st)
		mu.Unlock()
	})
	st := m.Add(Spec{Source: "s", Path: "/a", User: "admin", Prefix: "/a/"})
	if st.State != StateQueued {
		t.Fatalf("new job state %s", st.State)
	}
	st = waitFor(t, m, st.ID, StateDone)
	if st.Total != 1203 || st.Done != 1203 || st.Cached != 121 || st.Skipped != 121 || st.Failed != 121 || st.Generated != 840 {
		st.Failures = nil
		t.Fatalf("counts %+v", st)
	}
	if len(st.Failures) != 121 || st.Failures[0].Reason != "bad image" {
		t.Fatalf("failures: %d, first %+v", len(st.Failures), st.Failures[0])
	}
	if got := len(b.processed()); got != 1203 {
		t.Fatalf("processed %d files", got)
	}
	mu.Lock()
	defer mu.Unlock()
	last := events[len(events)-1]
	if last.State != StateDone || last.User != "admin" || last.Failures != nil {
		t.Fatalf("last event %+v (want done, for admin, without the failure list)", last)
	}
}

// Jobs run one at a time, in the order they were queued.
func TestJobsRunInQueueOrder(t *testing.T) {
	release := make(chan struct{})
	b := &fakeBackend{files: map[string][]string{
		"/a/": filesUnder("/a/", 2), "/b/": filesUnder("/b/", 2), "/c/": filesUnder("/c/", 1),
	}}
	b.process = func(_ context.Context, spec Spec, path string) (Outcome, error) {
		if path == "/a/f0000.png" {
			<-release
		}
		return Generated, nil
	}
	m := New(b, nil)
	a := m.Add(Spec{Prefix: "/a/"})
	waitFor(t, m, a.ID, StateRunning)
	bj := m.Add(Spec{Prefix: "/b/"})
	c := m.Add(Spec{Prefix: "/c/"})
	if st, _ := m.Get(bj.ID); st.State != StateQueued {
		t.Fatalf("second job is %s while the first runs", st.State)
	}
	list := m.List()
	if len(list) != 3 || list[0].ID != a.ID || list[1].ID != bj.ID || list[2].ID != c.ID {
		t.Fatalf("list order %v", list)
	}
	close(release)
	waitFor(t, m, c.ID, StateDone)
	want := "/a/:/a/f0000.png,/a/:/a/f0001.png,/b/:/b/f0000.png,/b/:/b/f0001.png,/c/:/c/f0000.png"
	if got := strings.Join(b.processed(), ","); got != want {
		t.Fatalf("order %s", got)
	}
	if list := m.List(); list[0].ID != c.ID { // finished: newest first
		t.Fatalf("finished order %v", list)
	}
}

// Cancelling the running job lets the current file finish and processes no more; cancelling a
// queued one removes it before it starts.
func TestCancel(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	b := &fakeBackend{files: map[string][]string{"/a/": filesUnder("/a/", 5), "/b/": filesUnder("/b/", 5)}}
	b.process = func(_ context.Context, _ Spec, path string) (Outcome, error) {
		if path == "/a/f0001.png" {
			close(started)
			<-release
		}
		return Generated, nil
	}
	m := New(b, nil)
	a := m.Add(Spec{Prefix: "/a/"})
	queued := m.Add(Spec{Prefix: "/b/"})
	<-started
	if st, ok := m.Cancel(queued.ID); !ok || st.State != StateCancelled {
		t.Fatalf("cancel queued: %+v %v", st, ok)
	}
	if st, _ := m.Cancel(a.ID); st.State != StateRunning || !st.CancelRequested {
		t.Fatalf("cancel running: %+v", st)
	}
	close(release)
	st := waitFor(t, m, a.ID, StateCancelled)
	if st.Done != 2 || st.Generated != 2 {
		t.Fatalf("cancelled job counts %+v, want 2 done (the current file finished)", st)
	}
	time.Sleep(20 * time.Millisecond)
	for _, p := range b.processed() {
		if strings.HasPrefix(p, "/b/") {
			t.Fatalf("cancelled queued job ran: %s", p)
		}
	}
	if got := len(b.processed()); got != 2 {
		t.Fatalf("processed %d files after cancel", got)
	}
	if _, ok := m.Cancel("nope"); ok {
		t.Fatal("cancel of an unknown job succeeded")
	}
}

// A file that runs past the timeout, and one that panics, count as failures and the job goes on.
func TestTimeoutAndPanicAreFailures(t *testing.T) {
	old := FileTimeout
	FileTimeout = 30 * time.Millisecond
	defer func() { FileTimeout = old }()
	b := &fakeBackend{files: map[string][]string{"/a/": filesUnder("/a/", 3)}}
	b.process = func(ctx context.Context, _ Spec, path string) (Outcome, error) {
		switch path {
		case "/a/f0000.png":
			<-ctx.Done()
			return Failed, ctx.Err()
		case "/a/f0001.png":
			panic("decoder bug")
		}
		return Generated, nil
	}
	m := New(b, nil)
	st := waitFor(t, m, m.Add(Spec{Prefix: "/a/"}).ID, StateDone)
	if st.Failed != 2 || st.Generated != 1 {
		t.Fatalf("counts %+v", st)
	}
	if !strings.Contains(st.Failures[0].Reason, "timed out") || !strings.Contains(st.Failures[1].Reason, "panic: decoder bug") {
		t.Fatalf("reasons %+v", st.Failures)
	}
}

// A job whose file list can't be read fails with the reason; the next job still runs.
func TestListingErrorFailsJob(t *testing.T) {
	b := &fakeBackend{files: map[string][]string{"/a/": filesUnder("/a/", 1)}}
	m := New(b, nil)
	bad := m.Add(Spec{Prefix: "/broken/"})
	good := m.Add(Spec{Prefix: "/a/"})
	if st := waitFor(t, m, bad.ID, StateFailed); !strings.Contains(st.Error, "db gone") {
		t.Fatalf("error %q", st.Error)
	}
	waitFor(t, m, good.ID, StateDone)
}

// The rate measured by a job is kept for estimating the next one.
func TestRateIsKept(t *testing.T) {
	b := &fakeBackend{files: map[string][]string{"/a/": filesUnder("/a/", 3)}}
	b.process = func(context.Context, Spec, string) (Outcome, error) {
		time.Sleep(5 * time.Millisecond)
		return Generated, nil
	}
	m := New(b, nil)
	if m.SecondsPerGenerated() != 0 {
		t.Fatal("rate known before any job")
	}
	waitFor(t, m, m.Add(Spec{Prefix: "/a/"}).ID, StateDone)
	if r := m.SecondsPerGenerated(); r < 0.004 || r > 1 {
		t.Fatalf("rate %v s per file", r)
	}
}
