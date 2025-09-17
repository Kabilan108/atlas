package worker

import (
	"context"
	"sync"
)

type Job func(ctx context.Context) error

type Pool struct {
	jobs    chan Job
	results chan error
	wg      sync.WaitGroup
	ctx     context.Context
	cancel  context.CancelFunc
}

func NewPool(ctx context.Context, maxWorkers int) *Pool {
	ctx, cancel := context.WithCancel(ctx)

	pool := &Pool{
		jobs:    make(chan Job, maxWorkers*2),
		results: make(chan error, maxWorkers*2),
		ctx:     ctx,
		cancel:  cancel,
	}

	for i := 0; i < maxWorkers; i++ {
		pool.wg.Add(1)
		go pool.worker(i)
	}

	return pool
}

func (p *Pool) worker(id int) {
	defer p.wg.Done()

	for {
		select {
		case job, ok := <-p.jobs:
			if !ok {
				return
			}

			err := job(p.ctx)

			select {
			case p.results <- err:
			case <-p.ctx.Done():
				return
			}

		case <-p.ctx.Done():
			return
		}
	}
}

func (p *Pool) Submit(job Job) {
	select {
	case p.jobs <- job:
	case <-p.ctx.Done():
	}
}

func (p *Pool) Close() {
	close(p.jobs)
}

func (p *Pool) Wait() {
	p.wg.Wait()
	close(p.results)
}

func (p *Pool) Cancel() {
	p.cancel()
}

func (p *Pool) Results() <-chan error {
	return p.results
}
