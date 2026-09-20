package inference

import (
	"context"
	"sync"
)

type ProgressEvent struct {
	Kind    string `json:"kind"`
	Phase   string `json:"phase"`
	Label   string `json:"label,omitempty"`
	Active  int    `json:"active"`
	Started int    `json:"started"`
	Done    int    `json:"done"`
	Failed  int    `json:"failed"`
}

type ProgressReporter func(ProgressEvent)

type progressContextKey struct{}

type progressTracker struct {
	mu       sync.Mutex
	reporter ProgressReporter
	counts   map[string]*progressCount
}

type progressCount struct {
	active  int
	started int
	done    int
	failed  int
}

func WithProgress(ctx context.Context, reporter ProgressReporter) context.Context {
	if reporter == nil {
		return ctx
	}
	return context.WithValue(ctx, progressContextKey{}, &progressTracker{
		reporter: reporter,
		counts:   map[string]*progressCount{},
	})
}

func BeginProgress(ctx context.Context, kind string, label string) func(error) {
	tracker, _ := ctx.Value(progressContextKey{}).(*progressTracker)
	if tracker == nil || kind == "" {
		return func(error) {}
	}
	start := tracker.update(kind, "start", label, false)
	tracker.reporter(start)
	return func(err error) {
		done := tracker.update(kind, "done", label, err != nil)
		tracker.reporter(done)
	}
}

func (t *progressTracker) update(kind string, phase string, label string, failed bool) ProgressEvent {
	t.mu.Lock()
	defer t.mu.Unlock()
	count := t.counts[kind]
	if count == nil {
		count = &progressCount{}
		t.counts[kind] = count
	}
	if phase == "start" {
		count.started++
		count.active++
	} else {
		if count.active > 0 {
			count.active--
		}
		count.done++
		if failed {
			count.failed++
		}
	}
	return ProgressEvent{
		Kind:    kind,
		Phase:   phase,
		Label:   label,
		Active:  count.active,
		Started: count.started,
		Done:    count.done,
		Failed:  count.failed,
	}
}
