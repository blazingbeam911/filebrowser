// Package thumbjob is a fork-only queue of background jobs that fill the thumbnail cache for
// every previewable file under a folder, so browsing it later doesn't wait for thumbnails.
//
// Jobs run one at a time, in the order they were queued, and live in memory only: a restart
// loses them, and running one again skips what is already cached. What a job does with each
// file (listing, stat, generating) is the Backend's; this package keeps the queue, the counts,
// cancelling, progress events and log lines.
package thumbjob

import (
	"context"
	"fmt"
	"strconv"
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
	Path   string `json:"path"`   // folder as the user sees it
	User   string `json:"user"`   // who started it; gets the progress events
	// Prefix is the folder's index path ("/a/b/"), the range the Backend lists.
	Prefix string `json:"-"`
	// Data is the Backend's own (e.g. the user to act as).
	Data any `json:"-"`
}

// Backend lists a job's files and processes one file.
type Backend interface {
	Count(spec Spec) (int, error)
	// Page returns up to limit file paths after `after`, in a stable order.
	Page(spec Spec, after string, limit int) ([]string, error)
	Process(ctx context.Context, spec Spec, path string) (Outcome, error)
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
	// SecondsPerGenerated is the average time of the files that needed thumbnails made.
	SecondsPerGenerated float64 `json:"secondsPerGenerated,omitempty"`
}

const (
	MaxFailures     = 1000 // failures kept in a job's list and logged one by one
	maxFinished     = 20   // finished jobs kept for the panel
	pageSize        = 500
	notifyEvery     = 2 * time.Second
	logEveryAtMost  = 30 * time.Second // progress lines: at each 1%, but no closer together
	logEveryAtLeast = 5 * time.Minute
)

// FileTimeout bounds one file, counted from when it starts processing.
var FileTimeout = 60 * time.Second

type job struct {
	st          Status
	cancel      bool
	genTime     time.Duration
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
}

// New starts a manager. notify, if not nil, gets each job's status as it changes (at most
// every couple of seconds while it runs, and at every change of state).
func New(backend Backend, notify func(user string, st Status)) *Manager {
	m := &Manager{backend: backend, notify: notify, wake: make(chan struct{}, 1)}
	go m.work()
	return m
}

// Add queues a job and returns its status.
func (m *Manager) Add(spec Spec) Status {
	m.mu.Lock()
	m.nextID++
	j := &job{st: Status{ID: strconv.Itoa(m.nextID), Spec: spec, State: StateQueued, QueuedAt: time.Now()}}
	m.queue = append(m.queue, j)
	st := m.snapshot(j)
	m.mu.Unlock()
	logger.Infof("[Thumbnails] job %s queued: source %q, path %q, by %s", st.ID, spec.Source, spec.Path, spec.User)
	select {
	case m.wake <- struct{}{}:
	default:
	}
	m.send(st)
	return st
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
	case StateRunning:
		if !j.cancel {
			logger.Infof("[Thumbnails] job %s: cancel requested, stopping after the current file", j.st.ID)
		}
		j.cancel = true
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

func (m *Manager) addFinished(j *job) {
	m.finished = append(m.finished, j)
	if len(m.finished) > maxFinished {
		m.finished = m.finished[len(m.finished)-maxFinished:]
	}
}

// snapshot copies a job's status, with the derived figures. Caller holds m.mu.
func (m *Manager) snapshot(j *job) Status {
	st := j.st
	st.CancelRequested = j.cancel && st.State == StateRunning
	st.Failures = append([]Failure(nil), j.st.Failures...)
	if st.Generated > 0 {
		st.SecondsPerGenerated = j.genTime.Seconds() / float64(st.Generated)
	}
	if st.State == StateRunning && st.StartedAt != nil && st.Done > 0 && st.Total > st.Done {
		perFile := time.Since(*st.StartedAt).Seconds() / float64(st.Done)
		st.SecondsLeft = perFile * float64(st.Total-st.Done)
	}
	return st
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

// run works through one job's files.
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
		dur := now.Sub(*st.StartedAt).Round(time.Second)
		if errMsg != "" {
			logger.Errorf("[Thumbnails] job %s failed after %v: %s", id, dur, errMsg)
		} else {
			logger.Infof("[Thumbnails] job %s %s in %v: %d of %d files, %d generated, %d already cached, %d skipped, %d failed",
				id, state, dur, st.Done, st.Total, st.Generated, st.Cached, st.Skipped, st.Failed)
		}
		m.send(st)
	}

	total, err := m.backend.Count(spec)
	if err != nil {
		finish(StateFailed, fmt.Sprintf("listing files: %v", err))
		return
	}
	m.mu.Lock()
	j.st.Total = total
	j.lastLog = time.Now()
	st := m.snapshot(j)
	m.mu.Unlock()
	logger.Infof("[Thumbnails] job %s started: source %q, path %q, %d previewable files", id, spec.Source, spec.Path, total)
	m.send(st)

	after := spec.Prefix
	for {
		if m.cancelled(j) {
			finish(StateCancelled, "")
			return
		}
		paths, err := m.backend.Page(spec, after, pageSize)
		if err != nil {
			finish(StateFailed, fmt.Sprintf("listing files: %v", err))
			return
		}
		if len(paths) == 0 {
			finish(StateDone, "")
			return
		}
		for _, p := range paths {
			if m.cancelled(j) {
				finish(StateCancelled, "")
				return
			}
			m.processOne(j, p)
		}
		after = paths[len(paths)-1]
	}
}

func (m *Manager) cancelled(j *job) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return j.cancel
}

// processOne runs the backend on one file under its own timeout and records the outcome.
func (m *Manager) processOne(j *job, path string) {
	start := time.Now()
	outcome, err := m.safeProcess(j.st.Spec, path)
	elapsed := time.Since(start)

	m.mu.Lock()
	j.st.Done++
	switch outcome {
	case Generated:
		j.st.Generated++
		j.genTime += elapsed
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
		logger.Infof("[Thumbnails] job %s: %d of %d files (%d%%), %d generated, %d cached, %d skipped, %d failed, about %s left",
			st.ID, st.Done, st.Total, step, st.Generated, st.Cached, st.Skipped, st.Failed, left)
	}
	if notify {
		m.send(st)
	}
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
