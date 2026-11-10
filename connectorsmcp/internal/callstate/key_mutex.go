// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package callstate

import (
	"context"
	"fmt"
	"sync"
)

// keyMutexRegistry serializes goroutines per key; flock may not, within one process.
type keyMutexRegistry struct {
	mu      sync.Mutex
	entries map[string]*keyMutex
}

type keyMutex struct {
	token      chan struct{}
	references int
}

func newKeyMutexRegistry() *keyMutexRegistry {
	return &keyMutexRegistry{entries: map[string]*keyMutex{}}
}

// acquire waits for the key's mutex until ctx ends and returns its release.
func (registry *keyMutexRegistry) acquire(ctx context.Context, stateKey string) (func(), error) {
	registry.mu.Lock()
	entry := registry.entries[stateKey]
	if entry == nil {
		entry = &keyMutex{token: make(chan struct{}, 1)}
		registry.entries[stateKey] = entry
	}
	entry.references++
	registry.mu.Unlock()
	select {
	case entry.token <- struct{}{}:
		return func() {
			<-entry.token
			registry.dereference(stateKey)
		}, nil
	case <-ctx.Done():
		registry.dereference(stateKey)
		return nil, fmt.Errorf("%w: %w", ErrLockWaitCanceled, context.Cause(ctx))
	}
}

func (registry *keyMutexRegistry) dereference(stateKey string) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	entry := registry.entries[stateKey]
	entry.references--
	if entry.references == 0 {
		delete(registry.entries, stateKey)
	}
}
