package pubsub

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
)

const bufferSize = 64

type Broker[T any] struct {
	subs         map[chan Event[T]]struct{}
	mu           sync.RWMutex
	done         chan struct{}
	subCount     int
	maxEvents    int
	droppedCount atomic.Uint64 // Track dropped events for monitoring
}

func NewBroker[T any]() *Broker[T] {
	return NewBrokerWithOptions[T](bufferSize, 1000)
}

func NewBrokerWithOptions[T any](channelBufferSize, maxEvents int) *Broker[T] {
	b := &Broker[T]{
		subs:      make(map[chan Event[T]]struct{}),
		done:      make(chan struct{}),
		subCount:  0,
		maxEvents: maxEvents,
	}
	return b
}

func (b *Broker[T]) Shutdown() {
	select {
	case <-b.done: // Already closed
		return
	default:
		close(b.done)
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	for ch := range b.subs {
		delete(b.subs, ch)
		close(ch)
	}

	b.subCount = 0
}

func (b *Broker[T]) Subscribe(ctx context.Context) <-chan Event[T] {
	b.mu.Lock()
	defer b.mu.Unlock()

	select {
	case <-b.done:
		ch := make(chan Event[T])
		close(ch)
		return ch
	default:
	}

	sub := make(chan Event[T], bufferSize)
	b.subs[sub] = struct{}{}
	b.subCount++

	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("Panic in pubsub subscriber cleanup", "panic", r)
			}
		}()

		<-ctx.Done()

		b.mu.Lock()
		defer b.mu.Unlock()

		select {
		case <-b.done:
			return
		default:
		}

		delete(b.subs, sub)
		close(sub)
		b.subCount--
	}()

	return sub
}

func (b *Broker[T]) GetSubscriberCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.subCount
}

func (b *Broker[T]) Publish(t EventType, payload T) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	select {
	case <-b.done:
		return
	default:
	}

	event := Event[T]{Type: t, Payload: payload}

	for sub := range b.subs {
		select {
		case sub <- event:
		default:
			// Channel is full, subscriber is slow - drop this event.
			// Log periodically to avoid spam.
			dropped := b.droppedCount.Add(1)
			if dropped == 1 || dropped%100 == 0 {
				slog.Warn("Pubsub event dropped (slow subscriber)",
					"event_type", t,
					"total_dropped", dropped)
			}
		}
	}
}

// DroppedCount returns the total number of events dropped due to slow subscribers.
func (b *Broker[T]) DroppedCount() uint64 {
	return b.droppedCount.Load()
}
