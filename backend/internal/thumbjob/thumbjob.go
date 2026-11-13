// Package thumbjob is a fork-only queue of background jobs that fill the thumbnail cache for
// every previewable file under a folder, so browsing it later doesn't wait for thumbnails.
//
// Jobs run one at a time, in the order they were queued. With Persist, queued and running jobs
// are saved to a file whenever the queue changes and queued again at the next start (a job that
// was running starts over; running one again skips what is already cached). A job can cover
// several folders, and can first have the Backend prepare them (Prepare: e.g. refresh new
// folders in the index). Within a job, CheckWorkers
// files are looked at in parallel (the Backend's quick Check), and at most GenerateWorkers of
// them are processed (thumbnails made) at once. While the Backend reports Busy (someone is
// browsing and needs thumbnails made), the job starts nothing new until it isn't. What a job
// does with each file (listing, stat, generating) is the Backend's; this package keeps the
// queue, the workers, the counts, cancelling, pausing, progress events and log lines.
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
	"time"

	"github.com/gtsteffaniak/go-logger/logger"
)

// State is where a job is in its life.
type State string

const (
	StateQueued    State = "queued"
	StateRunning   State = "running"
	StateDone      State = "done"
	StateCancelled State = "cancelled"
	StateFailed    State = "failed" // the job itself couldn't run (e.g. the file list failed)
)

// Outcome is what processing one file came to.
type Outcome int

const (
	Generated Outcome = iota // at least one thumbnail was made
	Cached                   // nothing to make: already cached, or browsing sends the file itself
	Skipped                  // nothing can be made: no longer there, or no preview for it
	Failed                   // making a thumbnail failed (error, timeout, panic)
)

// Spec is what a job works on.
type Spec struct {
	Source string `json:"source"` // source name
	// Path is the folder as the user sees it; for a job with several folders, the folder they
	// are all in.
	Path  string   `json:"path"`
	Paths []string `json:"paths,omitempty"` // the folders, when the job has more than one
	User  string   `json:"user"`            // who started it; gets the progress events
	// Refresh: the Backend's Prepare brings the folders into the index first (they may be new).
	Refresh bool `json:"refresh,omitempty"`
	// Prefixes are the folders' index paths ("/a/b/"), none inside another: the ranges the
	// Backend lists, in this order.
	Prefixes []string `json:"-"`
	// Data is the Backend's own (e.g. the user to act as).
	Data any `json:"-"`
}

// Backend lists a job's files and processes one file.
type Backend interface {
	// Prepare runs when the job starts, before Count (e.g. refreshing the folders in the index).
	Prepare(ctx context.Context, spec Spec) error
	Count(spec Spec) (int, error)
	// Page returns up to limit file paths under prefix (one of spec.Prefixes) after `after`, in
	// a stable order.
	Page(spec Spec, prefix, after string, limit int) ([]string, error)
	// Check is a quick look at one file (no decoding): ok = false when it can't tell, and the
	// file then goes to Process.
	Check(ctx context.Context, spec Spec, path string) (outcome Outcome, ok bool)
	Process(ctx context.Context, spec Spec, path string) (Outcome, error)
	// Busy reports whether jobs should hold off starting files (someone is browsing).
	Busy() bool
}

// Failure is one file that failed.
type Failure struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// Status is a snapshot of a job.
type Status struct {
	ID string `json:"id"`
	Spec
	State           State      `json:"state"`
	Error           string     `json:"error,omitempty"`
	CancelRequested bool       `json:"cancelRequested,omitempty"`
	Total           int        `json:"total"`
	Done            int        `json:"done"` // files processed so far, whatever their outcome
	Generated       int        `json:"generated"`
	Cached          int        `json:"cached"`
	Skipped         int        `json:"skipped"`
	Failed          int        `json:"failed"`
	Failures        []Failure  `json:"failures,omitempty"` // the first MaxFailures failures
	QueuedAt        time.Time  `json:"queuedAt"`
	StartedAt       *time.Time `json:"startedAt,omitempty"`
	FinishedAt      *time.Time `json:"finishedAt,omitempty"`
	// SecondsLeft estimates the time left from the rate so far (running jobs only, 0 = unknown).
	SecondsLeft float64 `json:"secondsLeft,omitempty"`
	// SecondsPerGenerated is the job's time so far (not counting pauses) per file that needed
	// thumbnails made: an upper bound, since checking the other files is in it too.
	SecondsPerGenerated float64 `json:"secondsPerGenerated,omitempty"`
	// Paused is true while the job waits for browsing to finish.
	Paused bool `json:"paused,omitempty"`
	// PausedSeconds is the job's time spent paused so far.
	PausedSeconds float64 `json:"pausedSeconds,omitempty"`
	// Preparing is true while the Backend prepares the job's folders (Prepare), before counting.
	Preparing bool `json:"preparing,omitempty"`
	// Merged is set on the status Add returns when the request was already covered by a queued
	// job, which is the one returned.
	Merged bool `json:"merged,omitempty"`
	// Restored is true for a job queued again from the saved queue at start.
	Restored bool `json:"restored,omitempty"`
}

const (
	MaxFailures     = 1000 // failures kept in a job's list and logged one by one
	maxFinished     = 20   // finished jobs kept for the panel, per user
	pageSize        = 500
	notifyEvery     = 2 * time.Second
	logEveryAtMost  = 30 * time.Second // progress lines: at each 1%, but no closer together
	logEveryAtLeast = 5 * time.Minute
)

// FileTimeout bounds one file, counted from when it starts processing (after any pause).
var FileTimeout = 60 * time.Second

var (
	// CheckWorkers is how many files a job looks at in parallel.
	CheckWorkers = 8
	// GenerateWorkers is how many files a job processes (makes thumbnails for) at once.
	GenerateWorkers = 2
	// busyPoll is how often a paused job checks whether it can go on.
	busyPoll = 100 * time.Millisecond
)

type job struct {
	st          Status
	cancel      bool
	stop        context.CancelFunc // cancels the running job's workers; nil until it runs
	waiting     int                // workers waiting while the backend is busy
	pausedSince time.Time          // when waiting went from 0 to 1
	paused      time.Duration      // finished pauses
	lastNotify  time.Time
	lastLog     time.Time
	lastLogStep int
}

// Manager is the job queue. Its worker starts with New and runs for the life of the process.
type Manager struct {
	backend Backend
	notify  func(user string, st Status)

	mu       sync.Mutex
	nextID   int
	queue    []*job
	running  *job
	finished []*job // oldest first
	// lastSecondsPerGenerated is the latest measured rate, for estimating new jobs.
	lastSecondsPerGenerated float64
	wake                    chan struct{}

	// Persist: the file the queue is saved to ("" = not saved), the saved jobs not queued yet
	// (their Backend wasn't ready), and a lock that keeps saves in order.
	savePath   string
	unrestored []savedJob
	saveMu     sync.Mutex
}

// New starts a manager. notify, if not nil, gets each job's status as it changes (at most
// every couple of seconds while it runs, and at every change of state).
func New(backend Backend, notify func(user string, st Status)) *Manager {
	m := &Manager{backend: backend, notify: notify, wake: make(chan struct{}, 1)}
	go m.work()
	return m
}

// Add queues a job and returns its status. When a queued job of the same user and source
// already covers every folder of spec (and refreshes them, if spec asks for it), nothing is
// added and that job's status is returned, with Merged set.
func (m *Manager) Add(spec Spec) Status {
	m.mu.Lock()
	for _, q := range m.queue {
		if q.st.User == spec.User && q.st.Source == spec.Source && (q.st.Refresh || !spec.Refresh) &&
			covers(q.st.Prefixes, spec.Prefixes) {
			st := m.snapshot(q)
			m.mu.Unlock()
			logger.Infof("[Thumbnails] request for source %q, path %q by %s: already in queued job %s", spec.Source, spec.Path, spec.User, st.ID)
			st.Merged = true
			return st
		}
	}
	m.nextID++
	j := &job{st: Status{ID: strconv.Itoa(m.nextID), Spec: spec, State: StateQueued, QueuedAt: time.Now()}}
	m.queue = append(m.queue, j)
	st := m.snapshot(j)
	m.mu.Unlock()
	logger.Infof("[Thumbnails] job %s queued: source %q, path %q%s, by %s", st.ID, spec.Source, spec.Path, foldersText(spec), spec.User)
	m.save()
	m.wakeUp()
	m.send(st)
	return st
}

func (m *Manager) wakeUp() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

// covers reports whether every prefix in want is one of have or inside one of them.
func covers(have, want []string) bool {
	if len(want) == 0 {
		return false
	}
	for _, w := range want {
		ok := false
		for _, h := range have {
			if strings.HasPrefix(w, h) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

// foldersText is ", N folders" for a job with several folders, for log lines.
func foldersText(spec Spec) string {
	if len(spec.Prefixes) > 1 {
		return fmt.Sprintf(" (%d folders)", len(spec.Prefixes))
	}
	return ""
}

// List returns the running job, then queued jobs in order, then finished ones, newest first.
func (m *Manager) List() []Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Status
	if m.running != nil {
		out = append(out, m.snapshot(m.running))
	}
	for _, j := range m.queue {
		out = append(out, m.snapshot(j))
	}
	for i := len(m.finished) - 1; i >= 0; i-- {
		out = append(out, m.snapshot(m.finished[i]))
	}
	return out
}

// Get returns one job's status.
func (m *Manager) Get(id string) (Status, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if j := m.find(id); j != nil {
		return m.snapshot(j), true
	}
	return Status{}, false
}

// Cancel removes a queued job, or asks the running one to stop after its current file.
// Cancelling a finished job does nothing.
func (m *Manager) Cancel(id string) (Status, bool) {
	m.mu.Lock()
	j := m.find(id)
	if j == nil {
		m.mu.Unlock()
		return Status{}, false
	}
	switch j.st.State {
	case StateQueued:
		for i, q := range m.queue {
			if q == j {
				m.queue = append(m.queue[:i], m.queue[i+1:]...)
				break
			}
		}
		now := time.Now()
		j.st.State = StateCancelled
		j.st.FinishedAt = &now
		m.addFinished(j)
		logger.Infof("[Thumbnails] job %s cancelled before it started", j.st.ID)
		defer m.save() // after m.mu is released below
	case StateRunning:
		if !j.cancel {
			logger.Infof("[Thumbnails] job %s: cancel requested, stopping after the files in progress", j.st.ID)
		}
		j.cancel = true
		if j.stop != nil {
			j.stop()
		}
	}
	st := m.snapshot(j)
	m.mu.Unlock()
	m.send(st)
	return st, true
}

// SecondsPerGenerated returns the latest measured time per file that needed thumbnails,
// or 0 when no job has measured one since the server started.
func (m *Manager) SecondsPerGenerated() float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastSecondsPerGenerated
}

func (m *Manager) find(id string) *job {
	if m.running != nil && m.running.st.ID == id {
		return m.running
	}
	for _, j := range m.queue {
		if j.st.ID == id {
			return j
		}
	}
	for _, j := range m.finished {
		if j.st.ID == id {
			return j
		}
	}
	return nil
}

// addFinished keeps a finished job, dropping that user's oldest when they have more than
// maxFinished. Caller holds m.mu.
func (m *Manager) addFinished(j *job) {
	m.finished = append(m.finished, j)
	n := 0
	for i := len(m.finished) - 1; i >= 0; i-- {
		if m.finished[i].st.User != j.st.User {
			continue
		}
		n++
		if n > maxFinished {
			m.finished = append(m.finished[:i], m.finished[i+1:]...)
		}
	}
}

// snapshot copies a job's status, with the derived figures. Caller holds m.mu.
func (m *Manager) snapshot(j *job) Status {
	st := j.st
	st.CancelRequested = j.cancel && st.State == StateRunning
	st.Failures = append([]Failure(nil), j.st.Failures...)
	paused := j.pausedTotal(time.Now())
	st.Paused = j.waiting > 0
	st.PausedSeconds = paused.Seconds()
	if st.StartedAt != nil {
		end := time.Now()
		if st.FinishedAt != nil {
			end = *st.FinishedAt
		}
		working := (end.Sub(*st.StartedAt) - paused).Seconds()
		if st.Generated > 0 && working > 0 {
			st.SecondsPerGenerated = working / float64(st.Generated)
		}
		if st.State == StateRunning && st.Done > 0 && st.Total > st.Done && working > 0 {
			st.SecondsLeft = working / float64(st.Done) * float64(st.Total-st.Done)
		}
	}
	return st
}

// pausedTotal is the job's paused time up to now, including a pause in progress. Caller holds m.mu.
func (j *job) pausedTotal(now time.Time) time.Duration {
	if j.waiting > 0 {
		return j.paused + now.Sub(j.pausedSince)
	}
	return j.paused
}

// send passes a status to notify, without the failure list (the panel fetches that), so
// events stay small.
func (m *Manager) send(st Status) {
	if m.notify != nil && st.User != "" {
		st.Failures = nil
		m.notify(st.User, st)
	}
}

func (m *Manager) work() {
	for {
		m.mu.Lock()
		if len(m.queue) == 0 {
			m.mu.Unlock()
			<-m.wake
			continue
		}
		j := m.queue[0]
		m.queue = m.queue[1:]
		m.running = j
		now := time.Now()
		j.st.State = StateRunning
		j.st.StartedAt = &now
		m.mu.Unlock()
		m.run(j)
	}
}

// run works through one job's files: this goroutine reads the file list a page at a time and
// hands the files to CheckWorkers workers.
func (m *Manager) run(j *job) {
	spec := j.st.Spec
	id := j.st.ID
	finish := func(state State, errMsg string) {
		m.mu.Lock()
		now := time.Now()
		j.st.State = state
		j.st.Error = errMsg
		j.st.FinishedAt = &now
		m.running = nil
		m.addFinished(j)
		st := m.snapshot(j)
		if st.SecondsPerGenerated > 0 {
			m.lastSecondsPerGenerated = st.SecondsPerGenerated
		}
		m.mu.Unlock()
		m.save()
		dur := now.Sub(*st.StartedAt).Round(time.Second)
		if errMsg != "" {
			logger.Errorf("[Thumbnails] job %s failed after %v: %s", id, dur, errMsg)
		} else {
			logger.Infof("[Thumbnails] job %s %s in %v: %d of %d files, %d generated, %d already cached, %d skipped, %d failed%s",
				id, state, dur, st.Done, st.Total, st.Generated, st.Cached, st.Skipped, st.Failed, pausedText(st))
		}
		m.send(st)
	}

	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	m.mu.Lock()
	j.stop = stop
	if j.cancel { // cancelled between leaving the queue and here
		stop()
	}
	j.st.Preparing = spec.Refresh
	st := m.snapshot(j)
	m.mu.Unlock()
	if spec.Refresh {
		m.send(st)
		began := time.Now()
		err := m.backend.Prepare(ctx, spec)
		m.mu.Lock()
		j.st.Preparing = false
		m.mu.Unlock()
		if err != nil {
			finish(StateFailed, fmt.Sprintf("refreshing the index: %v", err))
			return
		}
		logger.Infof("[Thumbnails] job %s: refreshed %d folder(s) in the index in %v", id, len(spec.Prefixes), time.Since(began).Round(time.Millisecond))
		if m.cancelled(j) {
			finish(StateCancelled, "")
			return
		}
	} else if err := m.backend.Prepare(ctx, spec); err != nil {
		finish(StateFailed, fmt.Sprintf("preparing: %v", err))
		return
	}

	total, err := m.backend.Count(spec)
	if err != nil {
		finish(StateFailed, fmt.Sprintf("listing files: %v", err))
		return
	}
	m.mu.Lock()
	j.st.Total = total
	j.lastLog = time.Now()
	st = m.snapshot(j)
	m.mu.Unlock()
	logger.Infof("[Thumbnails] job %s started: source %q, path %q%s, %d previewable files", id, spec.Source, spec.Path, foldersText(spec), total)
	m.send(st)

	paths := make(chan string)
	tokens := make(chan struct{}, max(GenerateWorkers, 1))
	var wg sync.WaitGroup
	for range max(CheckWorkers, 1) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range paths {
				m.processOne(ctx, j, p, tokens)
			}
		}()
	}

	listErr := m.feed(ctx, spec, paths)
	close(paths)
	wg.Wait()
	switch {
	case listErr != nil:
		finish(StateFailed, fmt.Sprintf("listing files: %v", listErr))
	case m.cancelled(j):
		finish(StateCancelled, "")
	default:
		finish(StateDone, "")
	}
}

// feed sends the job's files on paths, folder by folder, until the lists end or ctx is cancelled.
func (m *Manager) feed(ctx context.Context, spec Spec, paths chan<- string) error {
	for _, prefix := range spec.Prefixes {
		after := prefix
		for {
			if ctx.Err() != nil {
				return nil
			}
			page, err := m.backend.Page(spec, prefix, after, pageSize)
			if err != nil {
				return err
			}
			if len(page) == 0 {
				break
			}
			for _, p := range page {
				select {
				case paths <- p:
				case <-ctx.Done():
					return nil
				}
			}
			after = page[len(page)-1]
		}
	}
	return nil
}

func pausedText(st Status) string {
	if st.PausedSeconds < 1 {
		return ""
	}
	return fmt.Sprintf(", paused %v for browsing", (time.Duration(st.PausedSeconds) * time.Second).Round(time.Second))
}

// waitIdle returns once the backend isn't busy (at once if it isn't), false if ctx ended first.
// While any worker waits here the job shows as paused.
func (m *Manager) waitIdle(ctx context.Context, j *job) bool {
	if ctx.Err() != nil {
		return false
	}
	if !m.backend.Busy() {
		return true
	}
	m.setWaiting(j, +1)
	defer m.setWaiting(j, -1)
	t := time.NewTicker(busyPoll)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-t.C:
			if !m.backend.Busy() {
				return ctx.Err() == nil
			}
		}
	}
}

// setWaiting counts a worker into or out of the paused state, sending an event when the job
// pauses or resumes.
func (m *Manager) setWaiting(j *job, delta int) {
	m.mu.Lock()
	before := j.waiting > 0
	now := time.Now()
	if delta > 0 && j.waiting == 0 {
		j.pausedSince = now
	}
	j.waiting += delta
	if delta < 0 && j.waiting == 0 {
		j.paused += now.Sub(j.pausedSince)
	}
	changed := before != (j.waiting > 0)
	st := m.snapshot(j)
	m.mu.Unlock()
	if changed {
		m.send(st)
	}
}

func (m *Manager) cancelled(j *job) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return j.cancel
}

// processOne looks at one file and, when the quick check can't tell, processes it under its
// own timeout (holding one of the generate tokens); then it records the outcome. A file whose
// turn comes after the job was cancelled isn't counted.
func (m *Manager) processOne(ctx context.Context, j *job, path string, tokens chan struct{}) {
	if !m.waitIdle(ctx, j) {
		return
	}
	spec := j.st.Spec
	outcome, ok := m.safeCheck(ctx, spec, path)
	var err error
	if !ok {
		select {
		case tokens <- struct{}{}:
		case <-ctx.Done():
			return
		}
		if !m.waitIdle(ctx, j) {
			<-tokens
			return
		}
		outcome, err = m.safeProcess(spec, path)
		<-tokens
	}

	m.mu.Lock()
	j.st.Done++
	switch outcome {
	case Generated:
		j.st.Generated++
	case Cached:
		j.st.Cached++
	case Skipped:
		j.st.Skipped++
	default:
		j.st.Failed++
		if len(j.st.Failures) < MaxFailures {
			j.st.Failures = append(j.st.Failures, Failure{Path: path, Reason: errText(err)})
		}
	}
	failedN := j.st.Failed
	now := time.Now()
	step := 0
	if j.st.Total > 0 {
		step = j.st.Done * 100 / j.st.Total
	}
	logProgress := now.Sub(j.lastLog) >= logEveryAtLeast ||
		(step > j.lastLogStep && now.Sub(j.lastLog) >= logEveryAtMost)
	if logProgress {
		j.lastLog = now
		j.lastLogStep = step
	}
	notify := now.Sub(j.lastNotify) >= notifyEvery
	if notify {
		j.lastNotify = now
	}
	st := m.snapshot(j)
	if st.SecondsPerGenerated > 0 {
		m.lastSecondsPerGenerated = st.SecondsPerGenerated
	}
	m.mu.Unlock()

	if outcome == Failed {
		switch {
		case failedN <= MaxFailures:
			logger.Warningf("[Thumbnails] job %s: %s: %s", st.ID, path, errText(err))
		case failedN == MaxFailures+1:
			logger.Warningf("[Thumbnails] job %s: more than %d failures, not logging each one any more", st.ID, MaxFailures)
		}
	}
	if logProgress {
		left := "unknown"
		if st.SecondsLeft > 0 {
			left = (time.Duration(st.SecondsLeft) * time.Second).String()
		}
		logger.Infof("[Thumbnails] job %s: %d of %d files (%d%%), %d generated, %d cached, %d skipped, %d failed, about %s left%s",
			st.ID, st.Done, st.Total, step, st.Generated, st.Cached, st.Skipped, st.Failed, left, pausedText(st))
	}
	if notify {
		m.send(st)
	}
}

// safeCheck calls the backend's quick check; a panic sends the file on to Process.
func (m *Manager) safeCheck(ctx context.Context, spec Spec, path string) (outcome Outcome, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			outcome, ok = Failed, false
		}
	}()
	return m.backend.Check(ctx, spec, path)
}

// safeProcess calls the backend with the per-file timeout, turning a panic into a failure.
func (m *Manager) safeProcess(spec Spec, path string) (outcome Outcome, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), FileTimeout)
	defer cancel()
	defer func() {
		if r := recover(); r != nil {
			outcome, err = Failed, fmt.Errorf("panic: %v", r)
		}
	}()
	outcome, err = m.backend.Process(ctx, spec, path)
	if err != nil {
		outcome = Failed
		if ctx.Err() == context.DeadlineExceeded {
			err = fmt.Errorf("timed out after %v: %w", FileTimeout, err)
		}
	}
	return outcome, err
}

func errText(err error) string {
	if err == nil {
		return "unknown error"
	}
	return err.Error()
}

// ErrNotReady, from a Restorer, means the saved job can't be rebuilt yet (e.g. its source's
// index isn't loaded): it is tried again a little later.
var ErrNotReady = errors.New("not ready yet")

// Restorer rebuilds a saved job's spec at start (Prefixes, Data, checks), from the fields that
// are saved: Source, Path, Paths, User, Refresh.
type Restorer func(Spec) (Spec, error)

// savedJob is a queued or running job as written to the save file.
type savedJob struct {
	ID       string    `json:"id"`
	Source   string    `json:"source"`
	Path     string    `json:"path"`
	Paths    []string  `json:"paths,omitempty"`
	User     string    `json:"user"`
	Refresh  bool      `json:"refresh,omitempty"`
	QueuedAt time.Time `json:"queuedAt"`
}

type savedQueue struct {
	NextID int        `json:"nextId"`
	Jobs   []savedJob `json:"jobs"`
}

var (
	restoreEvery  = 5 * time.Second
	restoreGiveUp = time.Hour
)

// Persist makes the queue survive restarts: it reads the jobs saved in path by an earlier run,
// queues them again (in their old order, ahead of anything queued since) as restore rebuilds
// them, and from then on saves the queued and running jobs to path whenever the queue changes.
// Jobs restore can't rebuild yet (ErrNotReady) are retried every few seconds for an hour and
// stay in the file meanwhile; jobs it rejects are dropped. Call it once, at start.
func (m *Manager) Persist(path string, restore Restorer) {
	var saved savedQueue
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := json.Unmarshal(data, &saved); err != nil {
			logger.Errorf("[Thumbnails] saved queue %s unreadable, starting empty: %v", path, err)
			saved = savedQueue{}
		}
	case !errors.Is(err, os.ErrNotExist):
		logger.Errorf("[Thumbnails] reading the saved queue %s: %v", path, err)
	}
	m.mu.Lock()
	m.savePath = path
	if saved.NextID > m.nextID {
		m.nextID = saved.NextID
	}
	for _, sj := range saved.Jobs {
		if n, err := strconv.Atoi(sj.ID); err == nil && n > m.nextID {
			m.nextID = n
		}
	}
	m.unrestored = saved.Jobs
	m.mu.Unlock()
	if len(saved.Jobs) == 0 {
		m.save()
		return
	}
	logger.Infof("[Thumbnails] %d saved job(s) to queue again", len(saved.Jobs))
	go m.restoreLoop(restore)
}

func (m *Manager) restoreLoop(restore Restorer) {
	deadline := time.Now().Add(restoreGiveUp)
	for {
		left := m.restoreOnce(restore)
		if left == 0 {
			return
		}
		if time.Now().After(deadline) {
			logger.Errorf("[Thumbnails] %d saved job(s) still can't be queued after %v; they stay saved for the next start", left, restoreGiveUp)
			return
		}
		time.Sleep(restoreEvery)
	}
}

// restoreOnce tries every saved job not queued yet and returns how many are left.
func (m *Manager) restoreOnce(restore Restorer) int {
	m.mu.Lock()
	pending := append([]savedJob(nil), m.unrestored...)
	m.mu.Unlock()
	var keep []savedJob
	var restored []*job
	for _, sj := range pending {
		spec, err := safeRestore(restore, Spec{Source: sj.Source, Path: sj.Path, Paths: sj.Paths, User: sj.User, Refresh: sj.Refresh})
		switch {
		case errors.Is(err, ErrNotReady):
			keep = append(keep, sj)
		case err != nil:
			logger.Warningf("[Thumbnails] saved job %s (source %q, path %q, by %s) dropped: %v", sj.ID, sj.Source, sj.Path, sj.User, err)
		default:
			restored = append(restored, &job{st: Status{ID: sj.ID, Spec: spec, State: StateQueued, QueuedAt: sj.QueuedAt, Restored: true}})
		}
	}
	m.mu.Lock()
	m.unrestored = keep
	// Restored jobs go ahead of jobs queued since the start, in their old order.
	m.queue = append(restored, m.queue...)
	sort.SliceStable(m.queue, func(a, b int) bool {
		ra, rb := m.queue[a].st.Restored, m.queue[b].st.Restored
		if ra != rb {
			return ra
		}
		if ra {
			return idLess(m.queue[a].st.ID, m.queue[b].st.ID)
		}
		return false
	})
	var sts []Status
	for _, j := range restored {
		sts = append(sts, m.snapshot(j))
	}
	m.mu.Unlock()
	for _, st := range sts {
		logger.Infof("[Thumbnails] job %s queued again from the saved queue: source %q, path %q%s, by %s", st.ID, st.Source, st.Path, foldersText(st.Spec), st.User)
		m.send(st)
	}
	if len(restored) > 0 || len(keep) != len(pending) {
		m.save()
		m.wakeUp()
	}
	return len(keep)
}

func safeRestore(restore Restorer, spec Spec) (out Spec, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return restore(spec)
}

func idLess(a, b string) bool {
	na, ea := strconv.Atoi(a)
	nb, eb := strconv.Atoi(b)
	if ea == nil && eb == nil {
		return na < nb
	}
	return a < b
}

// save writes the running and queued jobs (and saved ones not queued again yet) to the save
// file, replacing it whole. Callers must not hold m.mu. A failed save is logged; the queue in
// memory goes on.
func (m *Manager) save() {
	m.saveMu.Lock()
	defer m.saveMu.Unlock()
	m.mu.Lock()
	path := m.savePath
	if path == "" {
		m.mu.Unlock()
		return
	}
	q := savedQueue{NextID: m.nextID, Jobs: []savedJob{}}
	add := func(j *job) {
		q.Jobs = append(q.Jobs, savedJob{ID: j.st.ID, Source: j.st.Source, Path: j.st.Path, Paths: j.st.Paths,
			User: j.st.User, Refresh: j.st.Refresh, QueuedAt: j.st.QueuedAt})
	}
	if m.running != nil && !m.running.cancel {
		add(m.running)
	}
	for _, j := range m.queue {
		add(j)
	}
	q.Jobs = append(q.Jobs, m.unrestored...)
	m.mu.Unlock()

	data, err := json.MarshalIndent(q, "", "  ")
	if err == nil {
		err = writeFileAtomic(path, data)
	}
	if err != nil {
		logger.Errorf("[Thumbnails] saving the queue to %s: %v", path, err)
	}
}

func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(name, 0o600)
	}
	if err == nil {
		err = os.Rename(name, path)
	}
	if err != nil {
		_ = os.Remove(name)
	}
	return err
}
