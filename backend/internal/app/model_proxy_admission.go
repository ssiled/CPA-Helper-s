package app

import (
	"context"
	"sync"
	"time"
)

const (
	defaultModelProxyMaxConcurrency = 64
	defaultModelProxyQueueSize      = 32
	defaultModelProxyQueueTimeoutMS = 2000
)

type modelProxyAdmissionWaiter struct {
	ready   chan struct{}
	granted bool
}

type modelProxyAdmissionController struct {
	mu      sync.Mutex
	limit   int
	active  int
	waiters []*modelProxyAdmissionWaiter
}

func newModelProxyAdmissionController() *modelProxyAdmissionController {
	return &modelProxyAdmissionController{}
}

func (c *modelProxyAdmissionController) acquire(ctx context.Context, limit, queueSize, timeoutMS int) (func(), string) {
	if limit <= 0 {
		return func() {}, ""
	}
	if ctx == nil {
		ctx = context.Background()
	}

	c.mu.Lock()
	c.limit = limit
	c.promoteLocked()
	if c.active < c.limit && len(c.waiters) == 0 {
		c.active++
		c.mu.Unlock()
		return c.releaseFunc(), ""
	}
	if queueSize <= 0 || len(c.waiters) >= queueSize {
		c.mu.Unlock()
		return nil, "queue_full"
	}
	waiter := &modelProxyAdmissionWaiter{ready: make(chan struct{})}
	c.waiters = append(c.waiters, waiter)
	c.mu.Unlock()

	timer := time.NewTimer(time.Duration(timeoutMS) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-waiter.ready:
		return c.releaseFunc(), ""
	case <-ctx.Done():
		return c.cancelWaiter(waiter, "request_canceled")
	case <-timer.C:
		return c.cancelWaiter(waiter, "queue_timeout")
	}
}

func (c *modelProxyAdmissionController) cancelWaiter(waiter *modelProxyAdmissionWaiter, reason string) (func(), string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if waiter.granted {
		if c.active > 0 {
			c.active--
		}
		c.promoteLocked()
		return nil, reason
	}
	for index, queued := range c.waiters {
		if queued == waiter {
			c.waiters = append(c.waiters[:index], c.waiters[index+1:]...)
			break
		}
	}
	return nil, reason
}

func (c *modelProxyAdmissionController) releaseFunc() func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			c.mu.Lock()
			if c.active > 0 {
				c.active--
			}
			c.promoteLocked()
			c.mu.Unlock()
		})
	}
}

func (c *modelProxyAdmissionController) promoteLocked() {
	for c.limit > 0 && c.active < c.limit && len(c.waiters) > 0 {
		waiter := c.waiters[0]
		c.waiters = c.waiters[1:]
		waiter.granted = true
		c.active++
		close(waiter.ready)
	}
}
