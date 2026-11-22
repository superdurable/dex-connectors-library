// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sdkgo_test

import (
	"fmt"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

type ReviewPostInput struct {
	Draft string `json:"draft"`
}

type ReviewPostOutput struct {
	MessageID string `json:"messageId"`
	Draft     string `json:"draft"`
}

// reviewPostMutation is an application Mutation that posts a review draft through postReply and returns
// its own result.
type reviewPostMutation struct {
	postReply sdkgo.Mutation[PostReplyInput, PostReplyOutput]
}

// Definition keeps the wrapped operation's identity, so the Call ID is the one a direct call gets.
func (wrapper reviewPostMutation) Definition() sdkgo.MutationDefinition {
	return wrapper.postReply.Definition()
}

func (wrapper reviewPostMutation) IdempotencyKey(id sdkgo.CallID, input ReviewPostInput) sdkgo.IdempotencyKey {
	return wrapper.postReply.IdempotencyKey(id, PostReplyInput{Text: input.Draft})
}

func (wrapper reviewPostMutation) Invoke(call sdkgo.Call, input ReviewPostInput) sdkgo.MutationAttempt[ReviewPostOutput] {
	attempt := wrapper.postReply.Invoke(call, PostReplyInput{Text: input.Draft})
	review := ReviewPostOutput{MessageID: attempt.Value().MessageID, Draft: input.Draft}
	switch attempt.Kind() {
	case sdkgo.AttemptKindBranch:
		return sdkgo.NewMutationBranch(attempt.Branch(), review, attempt.Failure(), attempt.Receipt())
	case sdkgo.AttemptKindUncertain:
		return sdkgo.NewMutationUncertain(review, *attempt.Failure(), attempt.Receipt())
	case sdkgo.AttemptKindRetry:
		return sdkgo.NewMutationRetry[ReviewPostOutput](*attempt.Failure(), attempt.RetryAfter())
	default:
		return sdkgo.MutationAttempt[ReviewPostOutput]{}
	}
}

// Example_wrapMutation wraps a connector Mutation in an application Mutation that calls the wrapped
// Invoke under its own Call, reads the attempt, and returns its own result for the same branch.
func Example_wrapMutation() {
	operation := reviewPostMutation{postReply: postReplyMutation{}}
	ctx := singledispatchtest.NewContext("review-flow", "post-review-step")
	for _, draft := range []string{"Looks good.", ""} {
		result, err := sdkgo.RunMutation(ctx, operation, sdkgo.ConnectionRef{Provider: "example-chat", Name: "work"}, ReviewPostInput{Draft: draft})
		if err != nil {
			fmt.Println("retry:", err)
			continue
		}
		if result.Failure != nil {
			fmt.Println(result.Branch, result.Failure.Message)
			continue
		}
		fmt.Println(result.Branch, result.Value.MessageID, result.Value.Draft)
	}
	// Output:
	// posted m-1 Looks good.
	// providerRejected reply text is empty
}
