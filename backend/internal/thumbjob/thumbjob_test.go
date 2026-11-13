package thumbjob

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeBackend serves files[prefix] as sorted paths; check and process decide each file's
// outcome (no check = every file goes to Process); busy is what Busy reports.
type fakeBackend struct {
	mu      sync.Mutex
	files   map[string][]string
	check   func(path string) (Outcome, bool)
	process func(ctx context.Context, spec Spec, path string) (Outcome, error)
	seen    []string // "jobpath:file" in processing order
	busy    atomic.Bool
	prepare func(ctx context.Context, spec Spec) error
}

func (b *fakeBackend) Check(_ context.Context, _ Spec, path string) (Outcome, bool) {
	if b.check != nil {
		return b.check(path)
	}
	return Failed, false
}

func (b *fakeBackend) Busy() bool { return b.busy.Load() }

func (b *fakeBackend) Prepare(ctx context.Context, spec Spec) error {
	if b.prepare != nil {
		return b.prepare(ctx, spec)
	}
	return nil
}

func (b *fakeBackend) Count(spec Spec) (int, error) {
	n := 0
	for _, prefix := range spec.Prefixes {
		if prefix == "/broken/" {
			return 0, errors.New("db gone")
		}
		b.mu.Lock()
		n += len(b.files[prefix])
		b.mu.Unlock()
	}
	return n, nil
}

func (b *fakeBackend) Page(_ Spec, prefix, after string, limit int) ([]string, error) {
	b.mu.Lock()
	all := append([]string(nil), b.files[prefix]...)
	b.mu.Unlock()
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
	b.seen = append(b.seen, spec.Prefixes[0]+":"+path)
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
	st := m.Add(Spec{Source: "s", Path: "/a", User: "admin", Prefixes: []string{"/a/"}})
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
	a := m.Add(Spec{Prefixes: []string{"/a/"}})
	waitFor(t, m, a.ID, StateRunning)
	bj := m.Add(Spec{Prefixes: []string{"/b/"}})
	c := m.Add(Spec{Prefixes: []string{"/c/"}})
	if st, _ := m.Get(bj.ID); st.State != StateQueued {
		t.Fatalf("second job is %s while the first runs", st.State)
	}
	list := m.List()
	if len(list) != 3 || list[0].ID != a.ID || list[1].ID != bj.ID || list[2].ID != c.ID {
		t.Fatalf("list order %v", list)
	}
	close(release)
	waitFor(t, m, c.ID, StateDone)
	// Within a job, files run in parallel; jobs don't overlap.
	var jobsInOrder []string
	for _, p := range b.processed() {
		job := p[:strings.Index(p, ":")]
		if len(jobsInOrder) == 0 || jobsInOrder[len(jobsInOrder)-1] != job {
			jobsInOrder = append(jobsInOrder, job)
		}
	}
	if got := strings.Join(jobsInOrder, ","); got != "/a/,/b/,/c/" || len(b.processed()) != 5 {
		t.Fatalf("job order %s, processed %v", got, b.processed())
	}
	if list := m.List(); list[0].ID != c.ID { // finished: newest first
		t.Fatalf("finished order %v", list)
	}
}

// Cancelling the running job lets the current file finish and processes no more; cancelling a
// queued one removes it before it starts.
func TestCancel(t *testing.T) {
	defer setWorkers(1, 1)()
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
	a := m.Add(Spec{Prefixes: []string{"/a/"}})
	queued := m.Add(Spec{Prefixes: []string{"/b/"}})
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

func setWorkers(check, generate int) (restore func()) {
	oldC, oldG := CheckWorkers, GenerateWorkers
	CheckWorkers, GenerateWorkers = check, generate
	return func() { CheckWorkers, GenerateWorkers = oldC, oldG }
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
	st := waitFor(t, m, m.Add(Spec{Prefixes: []string{"/a/"}}).ID, StateDone)
	if st.Failed != 2 || st.Generated != 1 {
		t.Fatalf("counts %+v", st)
	}
	reasons := map[string]string{} // files run in parallel, so failures come in any order
	for _, f := range st.Failures {
		reasons[f.Path] = f.Reason
	}
	if !strings.Contains(reasons["/a/f0000.png"], "timed out") || !strings.Contains(reasons["/a/f0001.png"], "panic: decoder bug") {
		t.Fatalf("reasons %+v", st.Failures)
	}
}

// A job whose file list can't be read fails with the reason; the next job still runs.
func TestListingErrorFailsJob(t *testing.T) {
	b := &fakeBackend{files: map[string][]string{"/a/": filesUnder("/a/", 1)}}
	m := New(b, nil)
	bad := m.Add(Spec{Prefixes: []string{"/broken/"}})
	good := m.Add(Spec{Prefixes: []string{"/a/"}})
	if st := waitFor(t, m, bad.ID, StateFailed); !strings.Contains(st.Error, "db gone") {
		t.Fatalf("error %q", st.Error)
	}
	waitFor(t, m, good.ID, StateDone)
}

// The rate measured by a job is kept for estimating the next one.
func TestRateIsKept(t *testing.T) {
	defer setWorkers(1, 1)() // one at a time, so wall time per file = the 5 ms each takes
	b := &fakeBackend{files: map[string][]string{"/a/": filesUnder("/a/", 3)}}
	b.process = func(context.Context, Spec, string) (Outcome, error) {
		time.Sleep(5 * time.Millisecond)
		return Generated, nil
	}
	m := New(b, nil)
	if m.SecondsPerGenerated() != 0 {
		t.Fatal("rate known before any job")
	}
	waitFor(t, m, m.Add(Spec{Prefixes: []string{"/a/"}}).ID, StateDone)
	if r := m.SecondsPerGenerated(); r < 0.004 || r > 1 {
		t.Fatalf("rate %v s per file", r)
	}
}

// Files the quick check decides never reach Process; at most GenerateWorkers files are in
// Process at once, while the checks run in parallel.
func TestCheckAndGenerateLimits(t *testing.T) {
	defer setWorkers(8, 2)()
	var inProcess, maxInProcess, inCheck, maxInCheck atomic.Int64
	bump := func(cur, peak *atomic.Int64) {
		n := cur.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
	}
	b := &fakeBackend{files: map[string][]string{"/a/": filesUnder("/a/", 200)}}
	b.check = func(path string) (Outcome, bool) {
		bump(&inCheck, &maxInCheck)
		time.Sleep(2 * time.Millisecond)
		inCheck.Add(-1)
		if strings.HasSuffix(path, "0.png") || strings.HasSuffix(path, "5.png") {
			return Generated + 99, false // undecided: the outcome is ignored
		}
		return Cached, true
	}
	b.process = func(context.Context, Spec, string) (Outcome, error) {
		bump(&inProcess, &maxInProcess)
		time.Sleep(3 * time.Millisecond)
		inProcess.Add(-1)
		return Generated, nil
	}
	m := New(b, nil)
	st := waitFor(t, m, m.Add(Spec{Prefixes: []string{"/a/"}}).ID, StateDone)
	if st.Done != 200 || st.Cached != 160 || st.Generated != 40 {
		t.Fatalf("counts %+v", st)
	}
	if got := len(b.processed()); got != 40 {
		t.Fatalf("Process ran for %d files, want the 40 the check left", got)
	}
	if maxInProcess.Load() != 2 {
		t.Fatalf("at most %d files in Process at once, want 2", maxInProcess.Load())
	}
	if maxInCheck.Load() < 3 {
		t.Fatalf("checks didn't run in parallel (at most %d at once)", maxInCheck.Load())
	}
}

// A panic in the quick check sends the file on to Process.
func TestCheckPanicFallsBackToProcess(t *testing.T) {
	b := &fakeBackend{files: map[string][]string{"/a/": filesUnder("/a/", 3)}}
	b.check = func(string) (Outcome, bool) { panic("stat bug") }
	m := New(b, nil)
	st := waitFor(t, m, m.Add(Spec{Prefixes: []string{"/a/"}}).ID, StateDone)
	if st.Generated != 3 || len(b.processed()) != 3 {
		t.Fatalf("counts %+v", st)
	}
}

// While the backend is busy the job starts no file and shows as paused; it goes on when the
// backend is no longer busy. Paused time is reported and kept out of the rate.
func TestPausesWhileBusy(t *testing.T) {
	oldPoll := busyPoll
	busyPoll = 2 * time.Millisecond
	defer func() { busyPoll = oldPoll }()
	b := &fakeBackend{files: map[string][]string{"/a/": filesUnder("/a/", 20)}}
	b.busy.Store(true)
	var mu sync.Mutex
	var events []Status
	m := New(b, func(_ string, st Status) {
		mu.Lock()
		events = append(events, st)
		mu.Unlock()
	})
	id := m.Add(Spec{Prefixes: []string{"/a/"}, User: "admin"}).ID
	deadline := time.Now().Add(5 * time.Second)
	for {
		st, _ := m.Get(id)
		if st.Paused {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("job never paused: %+v", st)
		}
		time.Sleep(time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	if st, _ := m.Get(id); st.Done != 0 || len(b.processed()) != 0 {
		t.Fatalf("files ran while busy: %+v", st)
	}
	b.busy.Store(false)
	st := waitFor(t, m, id, StateDone)
	if st.Generated != 20 || st.Paused || st.PausedSeconds < 0.04 {
		t.Fatalf("after the pause %+v", st)
	}
	mu.Lock()
	defer mu.Unlock()
	sawPaused, sawResumed := false, false
	for _, e := range events {
		if e.Paused {
			sawPaused = true
		} else if sawPaused && e.State == StateRunning {
			sawResumed = true
		}
	}
	if !sawPaused || !sawResumed {
		t.Fatalf("events: paused %v, resumed %v", sawPaused, sawResumed)
	}
}

// Cancelling a paused job ends it without processing anything more.
func TestCancelWhilePaused(t *testing.T) {
	oldPoll := busyPoll
	busyPoll = 2 * time.Millisecond
	defer func() { busyPoll = oldPoll }()
	b := &fakeBackend{files: map[string][]string{"/a/": filesUnder("/a/", 20)}}
	b.busy.Store(true)
	m := New(b, nil)
	id := m.Add(Spec{Prefixes: []string{"/a/"}}).ID
	waitFor(t, m, id, StateRunning)
	time.Sleep(20 * time.Millisecond)
	m.Cancel(id)
	st := waitFor(t, m, id, StateCancelled)
	if st.Done != 0 || len(b.processed()) != 0 || st.Paused {
		t.Fatalf("cancelled paused job %+v", st)
	}
}

// The per-file timeout starts after a pause: a file that waits longer than FileTimeout for
// browsing to end doesn't time out.
func TestTimeoutExcludesPause(t *testing.T) {
	defer setWorkers(1, 1)()
	oldPoll, oldTimeout := busyPoll, FileTimeout
	busyPoll, FileTimeout = 2*time.Millisecond, 40*time.Millisecond
	defer func() { busyPoll, FileTimeout = oldPoll, oldTimeout }()
	b := &fakeBackend{files: map[string][]string{"/a/": filesUnder("/a/", 2)}}
	b.check = func(path string) (Outcome, bool) {
		if path == "/a/f0001.png" {
			b.busy.Store(true) // browsing starts between the check and Process
			go func() { time.Sleep(100 * time.Millisecond); b.busy.Store(false) }()
		}
		return Failed, false
	}
	b.process = func(ctx context.Context, _ Spec, _ string) (Outcome, error) {
		select {
		case <-ctx.Done():
			return Failed, ctx.Err()
		case <-time.After(10 * time.Millisecond):
			return Generated, nil
		}
	}
	m := New(b, nil)
	st := waitFor(t, m, m.Add(Spec{Prefixes: []string{"/a/"}}).ID, StateDone)
	if st.Generated != 2 || st.Failed != 0 || st.PausedSeconds < 0.05 {
		t.Fatalf("counts %+v", st)
	}
}

// blockingBackend returns a backend whose Process waits on release for files under /hold/.
func blockingBackend(files map[string][]string) (*fakeBackend, chan struct{}) {
	release := make(chan struct{})
	b := &fakeBackend{files: files}
	b.process = func(_ context.Context, _ Spec, path string) (Outcome, error) {
		if strings.HasPrefix(path, "/hold/") {
			<-release
		}
		return Generated, nil
	}
	return b, release
}

// A job with several folders counts and works through all of them, folder by folder.
func TestSeveralFoldersInOneJob(t *testing.T) {
	restore := setWorkers(1, 1)
	defer restore()
	b := &fakeBackend{files: map[string][]string{"/a/": filesUnder("/a/", 3), "/b/": filesUnder("/b/", 2)}}
	m := New(b, nil)
	st := waitFor(t, m, m.Add(Spec{Path: "/", Paths: []string{"/a", "/b"}, Prefixes: []string{"/a/", "/b/"}}).ID, StateDone)
	if st.Total != 5 || st.Done != 5 || st.Generated != 5 {
		t.Fatalf("status %+v, want 5 files done", st)
	}
	seen := b.processed()
	if len(seen) != 5 || !strings.HasSuffix(seen[2], "/a/f0002.png") || !strings.HasSuffix(seen[3], "/b/f0000.png") {
		t.Fatalf("processed %v, want /a/ then /b/", seen)
	}
}

// Prepare runs before the files are counted (so what it adds is in the job), the job shows as
// preparing meanwhile, and a Prepare error fails the job.
func TestPrepareRunsFirst(t *testing.T) {
	b := &fakeBackend{files: map[string][]string{}}
	entered := make(chan struct{})
	release := make(chan struct{})
	b.prepare = func(_ context.Context, spec Spec) error {
		if spec.Prefixes[0] == "/bad/" {
			return errors.New("folder vanished")
		}
		close(entered)
		<-release
		b.mu.Lock()
		b.files["/new/"] = filesUnder("/new/", 4)
		b.mu.Unlock()
		return nil
	}
	m := New(b, nil)
	id := m.Add(Spec{Refresh: true, Prefixes: []string{"/new/"}}).ID
	<-entered
	if st, _ := m.Get(id); st.State != StateRunning || !st.Preparing {
		t.Fatalf("while preparing: %+v", st)
	}
	close(release)
	st := waitFor(t, m, id, StateDone)
	if st.Preparing || st.Total != 4 || st.Generated != 4 {
		t.Fatalf("after preparing: %+v, want the 4 new files", st)
	}
	bad := waitFor(t, m, m.Add(Spec{Refresh: true, Prefixes: []string{"/bad/"}}).ID, StateFailed)
	if !strings.Contains(bad.Error, "refreshing the index: folder vanished") {
		t.Fatalf("failed job error %q", bad.Error)
	}
}

// A request already covered by a queued job of the same user and source (the same folders, or
// folders inside them) returns that job; anything else gets its own.
func TestMergeIntoQueuedJob(t *testing.T) {
	b, release := blockingBackend(map[string][]string{"/hold/": {"/hold/x.png"}})
	defer close(release)
	m := New(b, nil)
	running := m.Add(Spec{User: "u", Source: "s", Prefixes: []string{"/hold/"}})
	waitFor(t, m, running.ID, StateRunning)

	q := m.Add(Spec{User: "u", Source: "s", Refresh: true, Prefixes: []string{"/a/", "/b/"}})
	for _, tc := range []struct {
		name  string
		spec  Spec
		merge bool
	}{
		{"same folders", Spec{User: "u", Source: "s", Refresh: true, Prefixes: []string{"/b/", "/a/"}}, true},
		{"inside, no refresh", Spec{User: "u", Source: "s", Prefixes: []string{"/a/x/"}}, true},
		{"the running job's folder", Spec{User: "u", Source: "s", Prefixes: []string{"/hold/"}}, false},
		{"other user", Spec{User: "v", Source: "s", Refresh: true, Prefixes: []string{"/a/"}}, false},
		{"other source", Spec{User: "u", Source: "t", Refresh: true, Prefixes: []string{"/a/"}}, false},
		{"one folder not covered", Spec{User: "u", Source: "s", Refresh: true, Prefixes: []string{"/a/", "/c/"}}, false},
	} {
		got := m.Add(tc.spec)
		if (got.ID == q.ID) != tc.merge || got.Merged != tc.merge {
			t.Errorf("%s: job %s merged=%v, want merged=%v into %s", tc.name, got.ID, got.Merged, tc.merge, q.ID)
		}
	}
	// Refresh asked for, the queued job doesn't refresh: not covered.
	plain := m.Add(Spec{User: "w", Source: "s", Prefixes: []string{"/p/"}})
	if got := m.Add(Spec{User: "w", Source: "s", Refresh: true, Prefixes: []string{"/p/"}}); got.ID == plain.ID {
		t.Error("a refresh request merged into a job that doesn't refresh")
	}
}

// Finished jobs are kept per user: one user's many jobs don't push out another's.
func TestFinishedKeptPerUser(t *testing.T) {
	m := New(&fakeBackend{files: map[string][]string{}}, nil)
	var last Status
	for i := 0; i < maxFinished+5; i++ {
		last = m.Add(Spec{User: "busy", Prefixes: []string{fmt.Sprintf("/b%d/", i)}})
		waitFor(t, m, last.ID, StateDone)
		if i == 2 {
			waitFor(t, m, m.Add(Spec{User: "quiet", Prefixes: []string{"/q/"}}).ID, StateDone)
		}
	}
	n := map[string]int{}
	for _, st := range m.List() {
		n[st.User]++
	}
	if n["busy"] != maxFinished || n["quiet"] != 1 {
		t.Fatalf("finished jobs kept %v, want %d busy and 1 quiet", n, maxFinished)
	}
}

// The queue survives a restart: queued and running jobs are saved, queued again in their old
// order with their ids (ahead of jobs added since), ids go on from where they were, a job the
// restorer can't rebuild yet is retried, and one it rejects is dropped.
func TestPersistAcrossRestart(t *testing.T) {
	prevEvery := restoreEvery
	restoreEvery = 5 * time.Millisecond
	defer func() { restoreEvery = prevEvery }()
	dir := t.TempDir()
	path := filepath.Join(dir, "thumbjobs.json")

	b1, release := blockingBackend(map[string][]string{"/hold/": {"/hold/x.png"}})
	m1 := New(b1, nil)
	m1.Persist(path, nil)
	t.Cleanup(m1.stopSaving)
	running := m1.Add(Spec{User: "u", Source: "s", Path: "/hold", Prefixes: []string{"/hold/"}})
	waitFor(t, m1, running.ID, StateRunning)
	a := m1.Add(Spec{User: "u", Source: "s", Path: "/", Paths: []string{"/a", "/b"}, Refresh: true, Prefixes: []string{"/a/", "/b/"}})
	gone := m1.Add(Spec{User: "gone", Source: "s", Path: "/g", Prefixes: []string{"/g/"}})
	later := m1.Add(Spec{User: "late", Source: "s", Path: "/l", Prefixes: []string{"/l/"}})
	cancelled := m1.Add(Spec{User: "u", Source: "s", Path: "/c", Prefixes: []string{"/c/"}})
	m1.Cancel(cancelled.ID)

	// The "restart": the file as it is now, read by a new manager (m1 goes on writing its own).
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("save file mode: %v %v", info.Mode(), err)
	}
	close(release)
	path2 := filepath.Join(dir, "restarted.json")
	if err := os.WriteFile(path2, data, 0o600); err != nil {
		t.Fatal(err)
	}

	b2, release2 := blockingBackend(map[string][]string{"/hold/": {"/hold/x.png"}})
	defer close(release2)
	m2 := New(b2, nil)
	t.Cleanup(m2.stopSaving)
	// A job added before the saved ones are back: it must wait behind them.
	first := m2.Add(Spec{User: "new", Source: "s", Path: "/hold", Prefixes: []string{"/hold/"}})
	waitFor(t, m2, first.ID, StateRunning)
	early := m2.Add(Spec{User: "new", Source: "s", Path: "/e", Prefixes: []string{"/e/"}})
	var lateTries atomic.Int32
	m2.Persist(path2, func(spec Spec) (Spec, error) {
		switch spec.User {
		case "gone":
			return Spec{}, errors.New("no such user")
		case "late":
			if lateTries.Add(1) < 3 {
				return Spec{}, ErrNotReady
			}
		}
		var prefixes []string
		paths := spec.Paths
		if len(paths) == 0 {
			paths = []string{spec.Path}
		}
		for _, p := range paths {
			prefixes = append(prefixes, strings.TrimSuffix(p, "/")+"/")
		}
		spec.Prefixes = prefixes
		return spec, nil
	})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := m2.Get(later.ID); ok {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	var queued []string
	for _, st := range m2.List() {
		if st.State == StateQueued {
			queued = append(queued, st.ID)
			if st.ID != early.ID && !st.Restored {
				t.Errorf("job %s back from the save file without Restored", st.ID)
			}
		}
	}
	want := []string{running.ID, a.ID, later.ID, early.ID}
	if fmt.Sprint(queued) != fmt.Sprint(want) {
		t.Fatalf("queued after the restart %v, want %v (saved ones first, in order; %s dropped, %s cancelled)", queued, want, gone.ID, cancelled.ID)
	}
	if st, _ := m2.Get(a.ID); len(st.Prefixes) != 2 || !st.Refresh || len(st.Paths) != 2 {
		t.Fatalf("restored job %+v lost its folders or refresh", st)
	}
	if _, ok := m2.Get(gone.ID); ok {
		t.Fatal("a job the restorer rejected was queued")
	}
	if n, _ := strconv.Atoi(m2.Add(Spec{User: "new", Prefixes: []string{"/n/"}}).ID); n <= mustAtoi(t, cancelled.ID) {
		t.Fatalf("new job id %d reuses a saved one (last %s)", n, cancelled.ID)
	}
	// The new manager's file now holds what it has queued, and not the dropped job.
	data2, _ := os.ReadFile(path2)
	var q savedQueue
	if err := json.Unmarshal(data2, &q); err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, j := range q.Jobs {
		ids[j.ID] = true
	}
	if !ids[a.ID] || !ids[later.ID] || ids[gone.ID] || ids[cancelled.ID] {
		t.Fatalf("save file after the restart has %v", ids)
	}
}

// stopSaving makes m stop writing its save file (waiting for a save in progress), so a test's
// temp dir can go while m's worker still runs.
func (m *Manager) stopSaving() {
	m.saveMu.Lock()
	defer m.saveMu.Unlock()
	m.mu.Lock()
	m.savePath = ""
	m.mu.Unlock()
}

func mustAtoi(t *testing.T, s string) int {
	t.Helper()
	n, err := strconv.Atoi(s)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// A missing or unreadable save file starts an empty queue.
func TestPersistBadFile(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{bad, filepath.Join(dir, "missing.json")} {
		m := New(&fakeBackend{files: map[string][]string{}}, nil)
		m.Persist(p, func(Spec) (Spec, error) { t.Fatal("restorer called"); return Spec{}, nil })
		if n := len(m.List()); n != 0 {
			t.Fatalf("%s: %d jobs", p, n)
		}
	}
}
