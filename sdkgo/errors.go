// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sdkgo

import (
	"fmt"
	"time"
)

// FailureKind describes a provider or local fact. It does not decide retry policy.
type FailureKind string

const (
	// FailureValidation identifies invalid application or connector input.
	FailureValidation FailureKind = "VALIDATION"
	// FailureAuthentication identifies missing, expired, or invalid credentials.
	FailureAuthentication FailureKind = "AUTHENTICATION"
	// FailureAuthorization identifies credentials without required permission.
	FailureAuthorization FailureKind = "AUTHORIZATION"
	// FailureNotFound identifies a provider resource that does not exist.
	FailureNotFound FailureKind = "NOT_FOUND"
	// FailureConflict identifies provider state that conflicts with the request.
	FailureConflict FailureKind = "CONFLICT"
	// FailureRateLimit identifies provider throttling that may succeed after retry.
	FailureRateLimit FailureKind = "RATE_LIMIT"
	// FailureQuotaExhausted identifies exhausted provider credit, spend, or billing
	// quota. Unlike FailureRateLimit, waiting does not restore it; the account
	// owner must add credit or raise the limit.
	FailureQuotaExhausted FailureKind = "QUOTA_EXHAUSTED"
	// FailureAvailability identifies temporary provider unavailability.
	FailureAvailability FailureKind = "AVAILABILITY"
	// FailureProviderRejection identifies another conclusive provider refusal.
	FailureProviderRejection FailureKind = "PROVIDER_REJECTION"
	// FailureTransport identifies a request whose provider outcome may be unknown.
	FailureTransport FailureKind = "TRANSPORT"
	// FailureResponseTooLarge identifies a response beyond the configured safety limit.
	FailureResponseTooLarge FailureKind = "RESPONSE_TOO_LARGE"
	// FailureProtocol identifies a provider response that violates its documented contract.
	FailureProtocol FailureKind = "PROTOCOL"
	// FailureLocalDefect identifies invalid connector wiring or an internal defect.
	FailureLocalDefect FailureKind = "LOCAL_DEFECT"
)

var allFailureKinds = []FailureKind{
	FailureValidation, FailureAuthentication, FailureAuthorization, FailureNotFound,
	FailureConflict, FailureRateLimit, FailureQuotaExhausted, FailureAvailability,
	FailureProviderRejection, FailureTransport, FailureResponseTooLarge, FailureProtocol,
	FailureLocalDefect,
}

// Failure is safe to persist. It must never contain credentials or provider bodies.
type Failure struct {
	// Kind is the kind associated with this failure.
	Kind FailureKind `json:"kind"`
	// Provider names the external provider without exposing credentials.
	Provider string `json:"provider"`
	// Operation identifies the connector operation.
	Operation string `json:"operation"`
	// Message is the message associated with this failure.
	Message string `json:"message"`
}

// Validate returns an error when the failure has an unknown kind or lacks its
// provider, operation, or message. RunQuery and RunMutation select the defect
// branch for an attempt whose failure does not validate.
func (failure Failure) Validate() error {
	return failure.validate()
}

func (failure Failure) validate() error {
	isKnownKind := false
	for _, kind := range allFailureKinds {
		if failure.Kind == kind {
			isKnownKind = true
			break
		}
	}
	if !isKnownKind {
		return fmt.Errorf("failure kind is invalid")
	}
	if failure.Provider == "" {
		return fmt.Errorf("failure provider is required")
	}
	if failure.Operation == "" {
		return fmt.Errorf("failure operation is required")
	}
	if failure.Message == "" {
		return fmt.Errorf("failure message is required")
	}
	return nil
}

// RetryError is the only connector error that asks Dex to retry a Step.
type RetryError struct {
	// Failure is the retryable provider or connector failure.
	Failure Failure
}

// Error returns the safe human-readable failure message.
func (err *RetryError) Error() string {
	return fmt.Sprintf("connector %s %s retry (%s): %s", err.Failure.Provider, err.Failure.Operation, err.Failure.Kind, err.Failure.Message)
}

// AttemptKind classifies one QueryAttempt or MutationAttempt. Its values are the TypeScript SDK's attempt
// kind strings. The zero AttemptKind belongs to a zero attempt, which no constructor returns and which
// RunQuery and RunMutation treat as a defect.
type AttemptKind string

const (
	// AttemptKindBranch is a terminal provider result for a declared branch, from NewQueryBranch or
	// NewMutationBranch.
	AttemptKindBranch AttemptKind = "branch"
	// AttemptKindRetry is a retryable failure, from NewQueryRetry or NewMutationRetry.
	AttemptKindRetry AttemptKind = "retry"
	// AttemptKindUncertain is a write whose outcome cannot be confirmed, from NewMutationUncertain.
	AttemptKindUncertain AttemptKind = "uncertain"
)

type queryAttemptKind uint8

const (
	queryAttemptInvalid queryAttemptKind = iota
	queryAttemptBranch
	queryAttemptRetry
)

// QueryAttempt is constructed only through NewQuery* constructors. Its read accessors let an application
// operation that wraps a connector Query call the wrapped Invoke under its own Call and map the attempt.
type QueryAttempt[T any] struct {
	kind       queryAttemptKind
	branch     BranchID
	value      T
	receipt    Receipt
	failure    Failure
	hasFailure bool
	retryAfter time.Duration
}

// NewQueryBranch returns a terminal provider result for a declared branch.
func NewQueryBranch[T any](branch BranchID, value T, failure *Failure, receipt Receipt) QueryAttempt[T] {
	attempt := QueryAttempt[T]{kind: queryAttemptBranch, branch: branch, value: value, receipt: receipt}
	if failure != nil {
		attempt.failure = *failure
		attempt.hasFailure = true
	}
	return attempt
}

// NewQueryRetry records a retryable query failure and optional provider-requested delay.
func NewQueryRetry[T any](failure Failure, retryAfter time.Duration) QueryAttempt[T] {
	return QueryAttempt[T]{kind: queryAttemptRetry, failure: failure, retryAfter: retryAfter}
}

// Kind returns AttemptKindBranch or AttemptKindRetry, or the zero AttemptKind for a zero attempt.
func (attempt QueryAttempt[T]) Kind() AttemptKind {
	switch attempt.kind {
	case queryAttemptBranch:
		return AttemptKindBranch
	case queryAttemptRetry:
		return AttemptKindRetry
	default:
		return ""
	}
}

// Branch returns the branch a branch attempt selected, or "" for any other kind. RunQuery checks that the
// operation declares it.
func (attempt QueryAttempt[T]) Branch() BranchID {
	if attempt.kind != queryAttemptBranch {
		return ""
	}
	return attempt.branch
}

// Value returns the typed result of a branch attempt, or the zero T for any other kind.
func (attempt QueryAttempt[T]) Value() T {
	if attempt.kind != queryAttemptBranch {
		var zero T
		return zero
	}
	return attempt.value
}

// Receipt returns the receipt the operation supplied with a branch attempt, before RunQuery fills the
// call's identity and observation time, or the zero Receipt for any other kind.
func (attempt QueryAttempt[T]) Receipt() Receipt {
	if attempt.kind != queryAttemptBranch {
		return Receipt{}
	}
	return attempt.receipt
}

// Failure returns a copy of the attempt's failure: the optional failure of a branch attempt and the
// failure of a retry attempt. It returns nil when the attempt has none. Changing the copy does not change
// the attempt.
func (attempt QueryAttempt[T]) Failure() *Failure {
	if !attempt.hasQueryFailure() {
		return nil
	}
	failure := attempt.failure
	return &failure
}

// RetryAfter returns the provider-requested delay of a retry attempt, or zero for the Step retry policy's
// backoff and for any other kind.
func (attempt QueryAttempt[T]) RetryAfter() time.Duration {
	if attempt.kind != queryAttemptRetry {
		return 0
	}
	return attempt.retryAfter
}

func (attempt QueryAttempt[T]) hasQueryFailure() bool {
	return attempt.kind == queryAttemptRetry || (attempt.kind == queryAttemptBranch && attempt.hasFailure)
}

type mutationAttemptKind uint8

const (
	mutationAttemptInvalid mutationAttemptKind = iota
	mutationAttemptBranch
	mutationAttemptUncertain
	mutationAttemptRetry
)

// MutationAttempt is constructed only through NewMutation* constructors. Its read accessors let an
// application Mutation that wraps a connector Mutation, such as an agent tool that adds its own result,
// call the wrapped Invoke under its own Call and map the attempt. Call the wrapped Invoke with the Call
// the wrapper received, never through a nested RunMutation of the same operation, and return its Failure
// and Receipt with the mapped value, so the wrapped call keeps its Call ID, idempotency key, send cap,
// and Fence. A wrapper whose Definition returns the wrapped definition, as Example_wrapMutation does,
// also gets the Call ID of a direct call:
//
//	attempt := wrapper.postReply.Invoke(call, PostReplyInput{Text: input.Draft})
//	review := ReviewPostOutput{MessageID: attempt.Value().MessageID, Draft: input.Draft}
//	switch attempt.Kind() {
//	case sdkgo.AttemptKindBranch:
//		return sdkgo.NewMutationBranch(attempt.Branch(), review, attempt.Failure(), attempt.Receipt())
//	case sdkgo.AttemptKindUncertain:
//		return sdkgo.NewMutationUncertain(review, *attempt.Failure(), attempt.Receipt())
//	case sdkgo.AttemptKindRetry:
//		return sdkgo.NewMutationRetry[ReviewPostOutput](*attempt.Failure(), attempt.RetryAfter())
//	default:
//		return sdkgo.MutationAttempt[ReviewPostOutput]{}
//	}
type MutationAttempt[T any] struct {
	kind       mutationAttemptKind
	branch     BranchID
	value      T
	receipt    Receipt
	failure    Failure
	hasFailure bool
	retryAfter time.Duration
}

// NewMutationBranch returns a terminal provider result for a declared branch.
func NewMutationBranch[T any](branch BranchID, value T, failure *Failure, receipt Receipt) MutationAttempt[T] {
	attempt := MutationAttempt[T]{kind: mutationAttemptBranch, branch: branch, value: value, receipt: receipt}
	if failure != nil {
		attempt.failure = *failure
		attempt.hasFailure = true
	}
	return attempt
}

// NewMutationUncertain records a write whose outcome cannot be confirmed. Use it only when
// the request may have reached the provider; no idempotency key, stable deduplication ID,
// absolute replacement, upsert, or delete makes a repeat safe; and a read-back cannot
// confirm the outcome. Otherwise, when the outcome is unknown but a repeat is safe, return
// NewMutationRetry.
func NewMutationUncertain[T any](value T, failure Failure, receipt Receipt) MutationAttempt[T] {
	return MutationAttempt[T]{
		kind: mutationAttemptUncertain, value: value, failure: failure, receipt: receipt, hasFailure: true,
	}
}

// NewMutationRetry records a retryable mutation failure and optional provider-requested delay.
func NewMutationRetry[T any](failure Failure, retryAfter time.Duration) MutationAttempt[T] {
	return MutationAttempt[T]{kind: mutationAttemptRetry, failure: failure, retryAfter: retryAfter}
}

// Kind returns AttemptKindBranch, AttemptKindUncertain, or AttemptKindRetry, or the zero AttemptKind for a
// zero attempt.
func (attempt MutationAttempt[T]) Kind() AttemptKind {
	switch attempt.kind {
	case mutationAttemptBranch:
		return AttemptKindBranch
	case mutationAttemptUncertain:
		return AttemptKindUncertain
	case mutationAttemptRetry:
		return AttemptKindRetry
	default:
		return ""
	}
}

// Branch returns the branch a branch attempt selected, or "" for any other kind. RunMutation checks that
// the operation declares it, and selects UncertainBranchID for an uncertain attempt.
func (attempt MutationAttempt[T]) Branch() BranchID {
	if attempt.kind != mutationAttemptBranch {
		return ""
	}
	return attempt.branch
}

// Value returns the typed result of a branch or uncertain attempt, or the zero T for any other kind.
func (attempt MutationAttempt[T]) Value() T {
	if attempt.kind != mutationAttemptBranch && attempt.kind != mutationAttemptUncertain {
		var zero T
		return zero
	}
	return attempt.value
}

// Receipt returns the receipt the operation supplied with a branch or uncertain attempt, before
// RunMutation fills the call's identity and observation time, or the zero Receipt for any other kind.
func (attempt MutationAttempt[T]) Receipt() Receipt {
	if attempt.kind != mutationAttemptBranch && attempt.kind != mutationAttemptUncertain {
		return Receipt{}
	}
	return attempt.receipt
}

// Failure returns a copy of the attempt's failure: the optional failure of a branch attempt and the
// failure of an uncertain or retry attempt. It returns nil when the attempt has none. Changing the copy
// does not change the attempt.
func (attempt MutationAttempt[T]) Failure() *Failure {
	if !attempt.hasMutationFailure() {
		return nil
	}
	failure := attempt.failure
	return &failure
}

// RetryAfter returns the provider-requested delay of a retry attempt, or zero for the Step retry policy's
// backoff and for any other kind.
func (attempt MutationAttempt[T]) RetryAfter() time.Duration {
	if attempt.kind != mutationAttemptRetry {
		return 0
	}
	return attempt.retryAfter
}

func (attempt MutationAttempt[T]) hasMutationFailure() bool {
	switch attempt.kind {
	case mutationAttemptUncertain, mutationAttemptRetry:
		return true
	case mutationAttemptBranch:
		return attempt.hasFailure
	default:
		return false
	}
}
