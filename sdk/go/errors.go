// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package connector

import (
	"errors"
	"fmt"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
)

type ErrorKind string

const (
	ErrorValidation            ErrorKind = "VALIDATION"
	ErrorAuthentication        ErrorKind = "AUTHENTICATION"
	ErrorAuthorization         ErrorKind = "AUTHORIZATION"
	ErrorNotFound              ErrorKind = "NOT_FOUND"
	ErrorConflict              ErrorKind = "CONFLICT"
	ErrorRateLimit             ErrorKind = "RATE_LIMIT"
	ErrorRetryableAvailability ErrorKind = "RETRYABLE_AVAILABILITY"
	ErrorTerminalRejection     ErrorKind = "TERMINAL_REJECTION"
	ErrorUnknownMutation       ErrorKind = "UNKNOWN_MUTATION"
	ErrorLocalDefect           ErrorKind = "LOCAL_DEFECT"
)

// Error exposes a safe message while retaining the original cause for errors.Is/As.
type Error struct {
	Kind       ErrorKind
	Provider   string
	Operation  string
	Message    string
	RetryAfter time.Duration
	receipt    *Receipt
	cause      error
}

// Failure is the serializable, secret-free form of a confirmed or unknown mutation failure.
type Failure struct {
	Kind       ErrorKind     `json:"kind"`
	Provider   string        `json:"provider"`
	Operation  string        `json:"operation"`
	Message    string        `json:"message"`
	RetryAfter time.Duration `json:"retryAfter,omitempty"`
}

func NewError(kind ErrorKind, provider, operation, safeMessage string, cause error) *Error {
	return &Error{Kind: kind, Provider: provider, Operation: operation, Message: safeMessage, cause: cause}
}

func (err *Error) WithRetryAfter(delay time.Duration) *Error {
	err.RetryAfter = delay
	return err
}

func (err *Error) WithReceipt(receipt Receipt) *Error {
	err.receipt = &receipt
	return err
}

func (err *Error) Receipt() (*Receipt, bool) {
	return err.receipt, err.receipt != nil
}

func (err *Error) Error() string {
	return fmt.Sprintf("connector %s %s failed (%s): %s", err.Provider, err.Operation, err.Kind, err.Message)
}

func (err *Error) Unwrap() error { return err.cause }

func IsKind(err error, kind ErrorKind) bool {
	var connectorErr *Error
	return errors.As(err, &connectorErr) && connectorErr.Kind == kind
}

func IsRetryable(err error) bool {
	return IsKind(err, ErrorRateLimit) || IsKind(err, ErrorRetryableAvailability)
}

func IsUnknownMutation(err error) bool { return IsKind(err, ErrorUnknownMutation) }

// DexRetry converts only connector failures that are safe for Dex to retry.
func DexRetry(err error, fallback time.Duration) (*dex.RetryAfterError, bool) {
	var connectorErr *Error
	if !errors.As(err, &connectorErr) || !IsRetryable(connectorErr) {
		return nil, false
	}
	delay := connectorErr.RetryAfter
	if delay <= 0 {
		delay = fallback
	}
	if delay <= 0 {
		return nil, false
	}
	return dex.RetryAfter(delay, err), true
}

func normalizeMutationError[T any](call Call, err error) (MutationResult[T], error) {
	var connectorErr *Error
	if !errors.As(err, &connectorErr) {
		return MutationResult[T]{}, err
	}
	if IsRetryable(connectorErr) || connectorErr.Kind == ErrorValidation || connectorErr.Kind == ErrorLocalDefect {
		return MutationResult[T]{}, err
	}
	outcome := MutationFailed
	if connectorErr.Kind == ErrorUnknownMutation {
		outcome = MutationUnknown
	}
	receipt := Receipt{}
	if existing, ok := connectorErr.Receipt(); ok {
		receipt = *existing
	}
	receipt, receiptErr := completeReceipt(receipt, call)
	if receiptErr != nil {
		return MutationResult[T]{}, receiptErr
	}
	return MutationResult[T]{
		Outcome: outcome,
		Receipt: receipt,
		Failure: &Failure{
			Kind: connectorErr.Kind, Provider: connectorErr.Provider,
			Operation: connectorErr.Operation, Message: connectorErr.Message,
			RetryAfter: connectorErr.RetryAfter,
		},
	}, nil
}
