// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package connectormock

import (
	"fmt"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// Option configures request and answer checks for a Query or Mutation mock.
// Only options built by this package can implement it.
type Option[IN, OUT any] interface {
	apply(*scriptedOperation[IN, OUT])
}

type optionFunc[IN, OUT any] func(*scriptedOperation[IN, OUT])

func (option optionFunc[IN, OUT]) apply(operation *scriptedOperation[IN, OUT]) { option(operation) }

// WithRequestCheck runs check before selecting a scripted case. A returned
// Failure selects defect without failing the test. The mock fills empty
// Provider and Operation fields from its definition; a Failure that is still
// invalid, such as one with an empty Kind or Message, fails the test as a
// check mistake. A mock takes one request check, and a second panics. The
// callback runs under the mock lock and must not call the mock.
func WithRequestCheck[IN, OUT any](check func(IN) *sdkgo.Failure) Option[IN, OUT] {
	if check == nil {
		panic("connector mock request check is nil")
	}
	return optionFunc[IN, OUT](func(operation *scriptedOperation[IN, OUT]) {
		if operation.requestCheck != nil {
			panic(fmt.Sprintf("connector mock %s: a second request check would replace the first; pass one WithRequestCheck", operation.describe()))
		}
		operation.requestCheck = check
	})
}

// WithAnswerCheck validates an answer on branch against its request, whether
// a scripted case or the default produced it. A mismatch fails the test and
// selects defect. Other branches and retries do not carry a generated answer
// and bypass this check. A mock takes one answer check, and a second panics.
// The callback runs under the mock lock and must not call the mock.
func WithAnswerCheck[IN, OUT any](branch sdkgo.BranchID, check func(IN, OUT) error) Option[IN, OUT] {
	if branch == "" || check == nil {
		panic("connector mock answer check requires a branch and function")
	}
	return optionFunc[IN, OUT](func(operation *scriptedOperation[IN, OUT]) {
		if !operation.declaredBranches[branch] {
			operation.t.Fatalf("connector mock %s: answer check branch %q is not declared", operation.describe(), branch)
		}
		if operation.answerCheck != nil {
			panic(fmt.Sprintf("connector mock %s: a second answer check would replace the first; pass one WithAnswerCheck", operation.describe()))
		}
		operation.answerBranch, operation.answerCheck = branch, check
	})
}
