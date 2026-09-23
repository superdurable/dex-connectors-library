// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package connector

import (
	"fmt"
	"time"
)

// FailureKind describes a provider or local fact. It does not decide retry policy.
type FailureKind string

const (
	FailureValidation        FailureKind = "VALIDATION"
	FailureAuthentication    FailureKind = "AUTHENTICATION"
	FailureAuthorization     FailureKind = "AUTHORIZATION"
	FailureNotFound          FailureKind = "NOT_FOUND"
	FailureConflict          FailureKind = "CONFLICT"
	FailureRateLimit         FailureKind = "RATE_LIMIT"
	FailureAvailability      FailureKind = "AVAILABILITY"
	FailureProviderRejection FailureKind = "PROVIDER_REJECTION"
	FailureTransport         FailureKind = "TRANSPORT"
	FailureResponseTooLarge  FailureKind = "RESPONSE_TOO_LARGE"
	FailureProtocol          FailureKind = "PROTOCOL"
	FailureLocalDefect       FailureKind = "LOCAL_DEFECT"
)

// Failure is safe to persist. It must never contain credentials or provider bodies.
type Failure struct {
	Kind      FailureKind `json:"kind"`
	Provider  string      `json:"provider"`
	Operation string      `json:"operation"`
	Message   string      `json:"message"`
}

func (failure Failure) validate() error {
	switch failure.Kind {
	case FailureValidation, FailureAuthentication, FailureAuthorization, FailureNotFound,
		FailureConflict, FailureRateLimit, FailureAvailability, FailureProviderRejection,
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
	Failure Failure
}

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

func NewMutationRetry[T any](failure Failure, retryAfter time.Duration) MutationAttempt[T] {
	return MutationAttempt[T]{kind: mutationAttemptRetry, failure: failure, retryAfter: retryAfter}
}
