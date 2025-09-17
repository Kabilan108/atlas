package worker

import (
	"context"
	"errors"
	"sync"
)

// Task represents a unit of work executed by the pool.
type Task func(context.Context) error

// Pool executes tasks with bounded concurrency and cancels remaining work when a task fails.
type Pool struct {
	parent context.Context
	ctx    context.Context
	cancel context.CancelFunc

	tasks chan Task
	wg    sync.WaitGroup

	errOnce sync.Once
	err     error
}

// New constructs a pool with the requested concurrency. Concurrency values below 1 default to 1.
func New(ctx context.Context, concurrency int) *Pool {
	if ctx == nil {
		panic("worker: context is required")
	}
	if concurrency < 1 {
		concurrency = 1
	}

	cctx, cancel := context.WithCancel(ctx)
	p := &Pool{
		parent: ctx,
		ctx:    cctx,
		cancel: cancel,
		tasks:  make(chan Task),
	}

	for i := 0; i < concurrency; i++ {
		p.wg.Add(1)
		go p.worker()
	}

	return p
}

// Submit schedules a task for execution. Returns an error if the context is done before the task is accepted.
func (p *Pool) Submit(task Task) error {
	if task == nil {
		return errors.New("worker: task is nil")
	}

	select {
	case <-p.ctx.Done():
		return p.ctx.Err()
	default:
	}

	select {
	case p.tasks <- task:
		return nil
	case <-p.ctx.Done():
		return p.ctx.Err()
	}
}

// Wait blocks until all submitted tasks complete or the pool context ends.
func (p *Pool) Wait() error {
	close(p.tasks)
	p.wg.Wait()
	p.cancel()

	if p.err != nil {
		return p.err
	}

	if err := p.parent.Err(); err != nil {
		return err
	}

	if err := p.ctx.Err(); err != nil && err != context.Canceled {
		return err
	}

	return nil
}

// Fail lets workers propagate their first error.
func (p *Pool) fail(err error) {
	if err == nil {
		return
	}
	p.errOnce.Do(func() {
		p.err = err
		p.cancel()
	})
}

func (p *Pool) worker() {
	defer p.wg.Done()
	for task := range p.tasks {
		if task == nil {
			continue
		}

		if p.ctx.Err() != nil {
			return
		}

		if err := task(p.ctx); err != nil {
			p.fail(err)
		}
	}
}
