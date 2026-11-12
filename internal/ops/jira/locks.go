package jira

import (
	"context"
	"sync"
)

// keyedLocks hands out one lock per key. A key's lock is dropped once no
// caller holds or waits for it, so the map stays as small as the work in
// flight.
type keyedLocks struct {
	mu    sync.Mutex
	locks map[string]*keyedLock
}

type keyedLock struct {
	held chan struct{}
	refs int
}

// lock waits for key's lock or for ctx to end, and returns the unlock.
func (k *keyedLocks) lock(ctx context.Context, key string) (func(), error) {
	k.mu.Lock()
	if k.locks == nil {
		k.locks = map[string]*keyedLock{}
	}
	l := k.locks[key]
	if l == nil {
		l = &keyedLock{held: make(chan struct{}, 1)}
		k.locks[key] = l
	}
	l.refs++
	k.mu.Unlock()
	select {
	case l.held <- struct{}{}:
		return func() {
			<-l.held
			k.release(key, l)
		}, nil
	case <-ctx.Done():
		k.release(key, l)
		return nil, ctx.Err()
	}
}

func (k *keyedLocks) release(key string, l *keyedLock) {
	k.mu.Lock()
	defer k.mu.Unlock()
	l.refs--
	if l.refs == 0 {
		delete(k.locks, key)
	}
}

func (k *keyedLocks) size() int {
	k.mu.Lock()
	defer k.mu.Unlock()
	return len(k.locks)
}
