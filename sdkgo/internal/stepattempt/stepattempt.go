// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package stepattempt keeps in-process state shared by every call of one Dex Step attempt.
package stepattempt

import (
	"context"
	"reflect"
	"sync"

	"github.com/superdurable/dex/sdk-go/dex"
)

// Registry holds one value per Step context and attempt. A value is released when its
// context ends or a later attempt of the same context replaces it.
type Registry[T any] struct {
	create    func(dex.Context) *T
	mu        sync.Mutex
	byContext map[any]*entry[T]
}

type entry[T any] struct {
	attempt int32
	value   *T
}

// stepIdentity keys a Step context whose dynamic type cannot be a map key.
type stepIdentity struct {
	flowID          string
	stepExecutionID string
}

// NewRegistry constructs a registry whose create function seeds a new attempt's value.
func NewRegistry[T any](create func(dex.Context) *T) *Registry[T] {
	if create == nil {
		panic("stepattempt: create function is required")
	}
	return &Registry[T]{create: create, byContext: map[any]*entry[T]{}}
}

// For returns the value of ctx's current attempt, creating it under the registry lock.
func (registry *Registry[T]) For(ctx dex.Context) *T {
	key := contextKey(ctx)
	attempt := ctx.Attempt()
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if existing, isPresent := registry.byContext[key]; isPresent && existing.attempt == attempt {
		return existing.value
	}
	created := &entry[T]{attempt: attempt, value: registry.create(ctx)}
	registry.byContext[key] = created
	context.AfterFunc(ctx, func() { registry.release(key, created) })
	return created.value
}

// Lookup reports the value registered for ctx without creating one.
func (registry *Registry[T]) Lookup(ctx dex.Context) (*T, bool) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	existing, isPresent := registry.byContext[contextKey(ctx)]
	if !isPresent {
		return nil, false
	}
	return existing.value, true
}

func (registry *Registry[T]) release(key any, released *entry[T]) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if registry.byContext[key] == released {
		delete(registry.byContext, key)
	}
}

func contextKey(ctx dex.Context) any {
	if reflect.TypeOf(ctx).Comparable() {
		return ctx
	}
	return stepIdentity{flowID: ctx.FlowID(), stepExecutionID: ctx.StepExecutionID()}
}
