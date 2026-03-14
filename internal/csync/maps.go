package csync

import (
	"encoding/json"
	"iter"
	"maps"
	"sync"
)

// Map is a concurrent map implementation that provides thread-safe access.
type Map[K comparable, V any] struct {
	inner map[K]V
	mu    sync.RWMutex
}

// NewMap creates a new thread-safe map with the specified key and value types.
func NewMap[K comparable, V any]() *Map[K, V] {
	return &Map[K, V]{
		inner: make(map[K]V),
	}
}

// NewMapFrom creates a new thread-safe map from an existing map.
func NewMapFrom[K comparable, V any](m map[K]V) *Map[K, V] {
	return &Map[K, V]{
		inner: m,
	}
}

// NewLazyMap creates a new lazy-loaded map. The provided load function is
// executed in a separate goroutine to populate the map. If the load function
// panics, the panic is recovered and the map will be initialized as empty.
func NewLazyMap[K comparable, V any](load func() map[K]V) *Map[K, V] {
	m := &Map[K, V]{}
	m.mu.Lock()
	go func() {
		defer m.mu.Unlock()
		defer func() {
			if r := recover(); r != nil {
				// On panic, ensure the map is at least initialized to empty.
				if m.inner == nil {
					m.inner = make(map[K]V)
				}
			}
		}()
		m.inner = load()
	}()
	return m
}

// Reset replaces the inner map with the new one.
func (m *Map[K, V]) Reset(input map[K]V) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.inner = input
}

// Set sets the value for the specified key in the map.
func (m *Map[K, V]) Set(key K, value V) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.inner[key] = value
}

// Del deletes the specified key from the map.
func (m *Map[K, V]) Del(key K) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.inner, key)
}

// Get gets the value for the specified key from the map.
func (m *Map[K, V]) Get(key K) (V, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.inner[key]
	return v, ok
}

// Len returns the number of items in the map.
func (m *Map[K, V]) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.inner)
}

// GetOrSet gets and returns the key if it exists, otherwise, it executes the
// given function, sets its return value for the given key, and returns it.
// This operation is atomic - the function will only be called once for a given
// key even under concurrent access.
func (m *Map[K, V]) GetOrSet(key K, fn func() V) V {
	// First try with read lock for the common case where key exists.
	m.mu.RLock()
	if v, ok := m.inner[key]; ok {
		m.mu.RUnlock()
		return v
	}
	m.mu.RUnlock()

	// Key doesn't exist, acquire write lock and check again.
	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check after acquiring write lock to avoid race condition.
	if v, ok := m.inner[key]; ok {
		return v
	}

	// Key still doesn't exist, compute and store the value.
	value := fn()
	if m.inner == nil {
		m.inner = make(map[K]V)
	}
	m.inner[key] = value
	return value
}

// Take gets an item and then deletes it.
func (m *Map[K, V]) Take(key K) (V, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.inner[key]
	delete(m.inner, key)
	return v, ok
}

// Copy returns a copy of the inner map.
func (m *Map[K, V]) Copy() map[K]V {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return maps.Clone(m.inner)
}

// IsEmpty returns true if the map has no items. O(1) operation.
func (m *Map[K, V]) IsEmpty() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.inner) == 0
}

// Keys returns a slice of all keys in the map. More efficient than Seq2
// when you only need keys.
func (m *Map[K, V]) Keys() []K {
	m.mu.RLock()
	defer m.mu.RUnlock()
	keys := make([]K, 0, len(m.inner))
	for k := range m.inner {
		keys = append(keys, k)
	}
	return keys
}

// Range iterates over the map while holding the read lock. This is more
// efficient than Seq2 when you don't need to modify the map during iteration.
// WARNING: Do not call any Map methods from within fn or you will deadlock.
func (m *Map[K, V]) Range(fn func(K, V) bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for k, v := range m.inner {
		if !fn(k, v) {
			return
		}
	}
}

// Seq2 returns an iter.Seq2 that yields key-value pairs from the map.
// This creates a snapshot copy of the map, so modifications during iteration
// are safe but won't be reflected. For read-only iteration, prefer Range().
func (m *Map[K, V]) Seq2() iter.Seq2[K, V] {
	dst := m.Copy()
	return func(yield func(K, V) bool) {
		for k, v := range dst {
			if !yield(k, v) {
				return
			}
		}
	}
}

// Seq returns an iter.Seq that yields values from the map.
// This creates a snapshot copy of the map. For read-only iteration, prefer Range().
func (m *Map[K, V]) Seq() iter.Seq[V] {
	return func(yield func(V) bool) {
		for _, v := range m.Seq2() {
			if !yield(v) {
				return
			}
		}
	}
}

var (
	_ json.Unmarshaler = &Map[string, any]{}
	_ json.Marshaler   = &Map[string, any]{}
)

func (Map[K, V]) JSONSchemaAlias() any { //nolint
	m := map[K]V{}
	return m
}

// UnmarshalJSON implements json.Unmarshaler.
func (m *Map[K, V]) UnmarshalJSON(data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.inner = make(map[K]V)
	return json.Unmarshal(data, &m.inner)
}

// MarshalJSON implements json.Marshaler.
func (m *Map[K, V]) MarshalJSON() ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return json.Marshal(m.inner)
}
