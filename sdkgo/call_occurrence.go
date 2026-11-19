// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sdkgo

import (
	"fmt"
	"sync"

	"github.com/superdurable/dex-connectors-library/sdkgo/internal/stepattempt"
	"github.com/superdurable/dex/sdk-go/dex"
)

// callOccurrences counts the calls of each operation in one Step attempt and tracks the
// Mutations still running.
type callOccurrences struct {
	mu               sync.Mutex
	counts           map[OperationRef]int
	runningMutations map[OperationRef]bool
}

var stepCallOccurrences = stepattempt.NewRegistry(func(dex.Context) *callOccurrences {
	return &callOccurrences{counts: map[OperationRef]int{}, runningMutations: map[OperationRef]bool{}}
})

// nextCallOccurrence returns the one-based ordinal of this call of operation in ctx's attempt.
func nextCallOccurrence(ctx dex.Context, operation OperationRef) int {
	occurrences := stepCallOccurrences.For(ctx)
	occurrences.mu.Lock()
	defer occurrences.mu.Unlock()
	occurrences.counts[operation]++
	return occurrences.counts[operation]
}

// beginMutationOccurrence refuses a call while an earlier one runs, since overlapping calls could swap ordinals.
// A refused call still consumes its ordinal, so later ordinals follow call order, not timing.
func beginMutationOccurrence(ctx dex.Context, operation OperationRef) (int, func(), error) {
	occurrences := stepCallOccurrences.For(ctx)
	occurrences.mu.Lock()
	defer occurrences.mu.Unlock()
	occurrences.counts[operation]++
	if occurrences.runningMutations[operation] {
		return 0, nil, fmt.Errorf("concurrent calls of connector operation %s %s in one Step are not supported; an earlier call of it is still running, so this call was not sent", operation.ConnectorID, operation.OperationID)
	}
	occurrences.runningMutations[operation] = true
	end := func() {
		occurrences.mu.Lock()
		defer occurrences.mu.Unlock()
		delete(occurrences.runningMutations, operation)
	}
	return occurrences.counts[operation], end, nil
}
