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

func (failure Failure) validate() error {
	switch failure.Kind {
	case FailureValidation, FailureAuthentication, FailureAuthorization, FailureNotFound,
		FailureConflict, FailureRateLimit, FailureQuotaExhausted, FailureAvailability, FailureProviderRejection,
		FailureTransport, FailureResponseTooLarge, FailureProtocol, FailureLocalDefect:
	default:
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

type queryAttemptKind uint8

const (
	queryAttemptInvalid queryAttemptKind = iota
	queryAttemptBranch
	queryAttemptRetry
)

// QueryAttempt is constructed only through NewQuery* constructors.
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

type mutationAttemptKind uint8

const (
	mutationAttemptInvalid mutationAttemptKind = iota
	mutationAttemptBranch
	mutationAttemptUncertain
	mutationAttemptRetry
)

// MutationAttempt is constructed only through NewMutation* constructors.
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

// NewMutationUncertain records that a dispatched mutation's outcome cannot be confirmed.
func NewMutationUncertain[T any](value T, failure Failure, receipt Receipt) MutationAttempt[T] {
	return MutationAttempt[T]{
		kind: mutationAttemptUncertain, value: value, failure: failure, receipt: receipt, hasFailure: true,
	}
}

// NewMutationRetry records a retryable mutation failure and optional provider-requested delay.
func NewMutationRetry[T any](failure Failure, retryAfter time.Duration) MutationAttempt[T] {
	return MutationAttempt[T]{kind: mutationAttemptRetry, failure: failure, retryAfter: retryAfter}
}
