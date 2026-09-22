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
	queryAttemptSucceeded
	queryAttemptFailed
	queryAttemptRetry
)

// QueryAttempt is constructed only through NewQuery* constructors.
type QueryAttempt[T any] struct {
	kind       queryAttemptKind
	value      T
	receipt    Receipt
	failure    Failure
	retryAfter time.Duration
}

func NewQuerySuccess[T any](value T, receipt Receipt) QueryAttempt[T] {
	return QueryAttempt[T]{kind: queryAttemptSucceeded, value: value, receipt: receipt}
}

func NewQueryFailure[T any](value T, failure Failure, receipt Receipt) QueryAttempt[T] {
	return QueryAttempt[T]{kind: queryAttemptFailed, value: value, failure: failure, receipt: receipt}
}

func NewQueryRetry[T any](failure Failure, retryAfter time.Duration) QueryAttempt[T] {
	return QueryAttempt[T]{kind: queryAttemptRetry, failure: failure, retryAfter: retryAfter}
}

type mutationAttemptKind uint8

const (
	mutationAttemptInvalid mutationAttemptKind = iota
	mutationAttemptSucceeded
	mutationAttemptFailed
	mutationAttemptUnknown
	mutationAttemptRetry
)

// MutationAttempt is constructed only through NewMutation* constructors.
type MutationAttempt[T any] struct {
	kind       mutationAttemptKind
	value      T
	receipt    Receipt
	failure    Failure
	retryAfter time.Duration
}

func NewMutationSuccess[T any](value T, receipt Receipt) MutationAttempt[T] {
	return MutationAttempt[T]{kind: mutationAttemptSucceeded, value: value, receipt: receipt}
}

func NewMutationFailure[T any](value T, failure Failure, receipt Receipt) MutationAttempt[T] {
	return MutationAttempt[T]{kind: mutationAttemptFailed, value: value, failure: failure, receipt: receipt}
}

func NewMutationUnknown[T any](value T, failure Failure, receipt Receipt) MutationAttempt[T] {
	return MutationAttempt[T]{kind: mutationAttemptUnknown, value: value, failure: failure, receipt: receipt}
}

func NewMutationRetry[T any](failure Failure, retryAfter time.Duration) MutationAttempt[T] {
	return MutationAttempt[T]{kind: mutationAttemptRetry, failure: failure, retryAfter: retryAfter}
}
