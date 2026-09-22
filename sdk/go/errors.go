// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package connector

import (
	"errors"
	"fmt"
	"time"
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
