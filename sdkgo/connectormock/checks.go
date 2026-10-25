// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package connectormock

import "github.com/superdurable/dex-connectors-library/sdkgo"

// Option configures request and answer checks for a Query or Mutation mock.
// Only options built by this package can implement it.
type Option[IN, OUT any] interface {
	apply(*scriptedOperation[IN, OUT])
}

type optionFunc[IN, OUT any] func(*scriptedOperation[IN, OUT])

func (option optionFunc[IN, OUT]) apply(operation *scriptedOperation[IN, OUT]) { option(operation) }

// WithRequestCheck runs check before selecting a scripted case. A returned
// Failure selects defect without failing the test. The mock fills empty
// Provider and Operation fields from its definition. The callback runs under
// the mock lock and must not call the mock.
func WithRequestCheck[IN, OUT any](check func(IN) *sdkgo.Failure) Option[IN, OUT] {
	if check == nil {
		panic("connector mock request check is nil")
	}
	return optionFunc[IN, OUT](func(operation *scriptedOperation[IN, OUT]) { operation.requestCheck = check })
}

// WithAnswerCheck validates a scripted answer on branch against its request.
// A mismatch fails the test and selects defect. Other branches and retries do
// not carry a generated answer and bypass this check. The callback runs under
// the mock lock and must not call the mock.
func WithAnswerCheck[IN, OUT any](branch sdkgo.BranchID, check func(IN, OUT) error) Option[IN, OUT] {
	if branch == "" || check == nil {
		panic("connector mock answer check requires a branch and function")
	}
	return optionFunc[IN, OUT](func(operation *scriptedOperation[IN, OUT]) {
		if !operation.declaredBranches[branch] {
			operation.t.Fatalf("connector mock %s: answer check branch %q is not declared", operation.describe(), branch)
		}
		operation.answerBranch, operation.answerCheck = branch, check
	})
}
