// request_queue.go - Cola de solicitudes a la API de SPIDI con rate limiting y retry.
package queue

import (
	"context"
	"sync"
	"time"
)

// RateLimiter controla la frecuencia de solicitudes.
type RateLimiter struct {
	tokens   int
	max      int
	interval time.Duration
	mu       sync.Mutex
	refillCh chan struct{}
}

// NewRateLimiter crea un rate limiter con n tokens por intervalo.
func NewRateLimiter(max int, interval time.Duration) *RateLimiter {
	rl := &RateLimiter{
		tokens:   max,
		max:      max,
		interval: interval,
		refillCh: make(chan struct{}, 1),
	}
	go rl.refill()
	return rl
}

func (rl *RateLimiter) refill() {
	ticker := time.NewTicker(rl.interval)
	defer ticker.Stop()
	for range ticker.C {
		rl.mu.Lock()
		rl.tokens = rl.max
		rl.mu.Unlock()
		select {
		case rl.refillCh <- struct{}{}:
		default:
		}
	}
}

// Wait bloquea hasta que haya un token disponible o el contexto expire.
func (rl *RateLimiter) Wait(ctx context.Context) error {
	for {
		rl.mu.Lock()
		if rl.tokens > 0 {
			rl.tokens--
			rl.mu.Unlock()
			return nil
		}
		rl.mu.Unlock()

		select {
		case <-rl.refillCh:
			continue
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// RequestQueue maneja solicitudes a la API con retry.
type RequestQueue struct {
	limiter    *RateLimiter
	maxRetries int
	baseDelay  time.Duration
}

// NewRequestQueue crea una nueva cola de solicitudes.
func NewRequestQueue(requestsPerSec int, maxRetries int) *RequestQueue {
	interval := time.Duration(1000000000 / requestsPerSec) * time.Nanosecond
	if interval < time.Millisecond {
		interval = time.Millisecond
	}
	return &RequestQueue{
		limiter:    NewRateLimiter(requestsPerSec, time.Second),
		maxRetries: maxRetries,
		baseDelay:  500 * time.Millisecond,
	}
}

// Do ejecuta fn con rate limiting y retry exponencial.
func (rq *RequestQueue) Do(ctx context.Context, fn func() error) error {
	var lastErr error
	for attempt := 0; attempt <= rq.maxRetries; attempt++ {
		if err := rq.limiter.Wait(ctx); err != nil {
			return err
		}

		if err := fn(); err != nil {
			lastErr = err
			if attempt < rq.maxRetries {
				delay := rq.baseDelay << attempt // exponential backoff: 500ms, 1s, 2s, 4s...
				select {
				case <-time.After(delay):
					continue
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			continue
		}
		return nil
	}
	return lastErr
}
