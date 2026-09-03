// Package taskrun provides small in-memory helpers for controlling durable
// background runs. Persisted task state remains in each domain's database
// model; this package only owns cancellation functions for the current
// process.
package taskrun

import (
	"context"
	"sync"
)

// Controller associates durable numeric run IDs with the cancellation
// functions of their currently live goroutines. A process restart naturally
// clears this map, allowing the domain layer to resume from its saved
// checkpoint instead of treating in-memory state as durable.
type Controller struct {
	mu      sync.RWMutex
	cancels map[uint]context.CancelFunc
}

// NewController creates an empty run controller.
func NewController() *Controller {
	return &Controller{cancels: make(map[uint]context.CancelFunc)}
}

// Set marks runID as live and records how to request its cancellation.
func (controller *Controller) Set(runID uint, cancel context.CancelFunc) {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	controller.cancels[runID] = cancel
}

// Cancel requests cancellation for the currently live run. It returns false
// when the run belongs to a previous process or is not currently executing.
func (controller *Controller) Cancel(runID uint) bool {
	controller.mu.RLock()
	cancel := controller.cancels[runID]
	controller.mu.RUnlock()
	if cancel == nil {
		return false
	}
	cancel()
	return true
}

// Active reports whether runID has a live goroutine in this process.
func (controller *Controller) Active(runID uint) bool {
	controller.mu.RLock()
	defer controller.mu.RUnlock()
	return controller.cancels[runID] != nil
}

// Clear removes the completed, paused or failed run from live control.
func (controller *Controller) Clear(runID uint) {
	controller.mu.Lock()
	delete(controller.cancels, runID)
	controller.mu.Unlock()
}
