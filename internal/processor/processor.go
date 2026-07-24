// Package processor provides bounded, asynchronous in-process event
// processing.
package processor

import (
	"context"
	"errors"
	"sync"

	"github.com/akashkumar7902/async-event-service/internal/stats"
)

var (
	// ErrQueueFull indicates that the bounded input queue has no free slot.
	ErrQueueFull = errors.New("event queue is full")

	// ErrShuttingDown indicates that the processor no longer accepts work.
	ErrShuttingDown = errors.New("event processor is shutting down")
)

// Options configures a Processor.
type Options struct {
	WorkerCount   int
	QueueCapacity int
	Store         *stats.Store
}

type workItem struct {
	eventType string
}

// Processor asynchronously increments event counters using a bounded queue
// and a fixed worker pool.
//
// The admission lock protects the acceptance decision and channel closure as
// one lifecycle operation. This prevents a concurrent submission from sending
// to a closed channel.
type Processor struct {
	queue chan workItem
	store *stats.Store

	admissionMu sync.RWMutex
	accepting   bool
	closeOnce   sync.Once

	workerWG sync.WaitGroup
	drained  chan struct{}
}

// New validates options, starts exactly WorkerCount workers, and returns a
// processor ready to accept events.
func New(options Options) (*Processor, error) {
	if options.WorkerCount <= 0 {
		return nil, errors.New("worker count must be positive")
	}
	if options.QueueCapacity <= 0 {
		return nil, errors.New("queue capacity must be positive")
	}
	if options.Store == nil {
		return nil, errors.New("statistics store is required")
	}

	p := &Processor{
		queue:     make(chan workItem, options.QueueCapacity),
		store:     options.Store,
		accepting: true,
		drained:   make(chan struct{}),
	}

	p.workerWG.Add(options.WorkerCount)
	for range options.WorkerCount {
		go p.worker()
	}

	go func() {
		p.workerWG.Wait()
		close(p.drained)
	}()

	return p, nil
}

// TrySubmit attempts to admit eventType without waiting for queue capacity.
//
// A nil result means the event entered the local queue. ErrQueueFull means it
// did not. Once shutdown begins, TrySubmit returns ErrShuttingDown.
func (p *Processor) TrySubmit(eventType string) error {
	p.admissionMu.RLock()
	defer p.admissionMu.RUnlock()

	if !p.accepting {
		return ErrShuttingDown
	}

	select {
	case p.queue <- workItem{eventType: eventType}:
		return nil
	default:
		return ErrQueueFull
	}
}

// StopAccepting closes admission without closing the input channel. Call
// CloseInput after all producers have stopped.
func (p *Processor) StopAccepting() {
	p.admissionMu.Lock()
	p.accepting = false
	p.admissionMu.Unlock()
}

// CloseInput atomically stops admission and closes the input channel. It is
// safe to call multiple times and concurrently.
func (p *Processor) CloseInput() {
	// this prevents a panic when closing second time
	p.closeOnce.Do(func() {
		p.admissionMu.Lock()
		p.accepting = false
		close(p.queue)
		p.admissionMu.Unlock()
	})
}

// Wait waits for all workers to finish draining the closed input queue. It
// does not itself stop admission or close the queue.
func (p *Processor) Wait(ctx context.Context) error {
	select {
	case <-p.drained:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// QueueDepth returns an approximate queue length for debugging.
func (p *Processor) QueueDepth() int {
	return len(p.queue)
}

func (p *Processor) worker() {
	defer p.workerWG.Done()

	for item := range p.queue {
		p.store.Increment(item.eventType)
	}
}
