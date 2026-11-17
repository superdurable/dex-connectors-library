// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sdkgo

import (
	"sync"

	"github.com/superdurable/dex-connectors-library/sdkgo/internal/stepattempt"
	"github.com/superdurable/dex/sdk-go/dex"
)

// callOccurrences counts the calls of each operation in one Step attempt.
type callOccurrences struct {
	mu     sync.Mutex
	counts map[OperationRef]int
}

var stepCallOccurrences = stepattempt.NewRegistry(func(dex.Context) *callOccurrences {
	return &callOccurrences{counts: map[OperationRef]int{}}
})

// nextCallOccurrence returns the one-based ordinal of this call of operation in ctx's attempt.
func nextCallOccurrence(ctx dex.Context, operation OperationRef) int {
	occurrences := stepCallOccurrences.For(ctx)
	occurrences.mu.Lock()
	defer occurrences.mu.Unlock()
	occurrences.counts[operation]++
	return occurrences.counts[operation]
}
