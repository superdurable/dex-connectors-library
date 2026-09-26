// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sdkgo

import (
	"errors"
)

// SecretString is intentionally not serializable and always renders redacted.
type SecretString struct {
	value string
}

// NewSecretString wraps a secret for provider-boundary access without making it serializable.
func NewSecretString(value string) SecretString { return SecretString{value: value} }

// Reveal returns the secret only at the provider request boundary.
func (secret SecretString) Reveal() string { return secret.value }

// String returns a redacted representation that never exposes the secret.
func (SecretString) String() string { return "[REDACTED]" }

// GoString returns a redacted representation that never exposes the secret.
func (SecretString) GoString() string { return "sdkgo.SecretString{[REDACTED]}" }

// MarshalJSON redacts the secret while preserving JSON shape.
func (SecretString) MarshalJSON() ([]byte, error) {
	return nil, errors.New("connector secrets cannot be serialized")
}

// MarshalText returns a redacted textual representation.
func (SecretString) MarshalText() ([]byte, error) {
	return nil, errors.New("connector secrets cannot be serialized")
}

// MarshalYAML returns a redacted YAML representation.
func (SecretString) MarshalYAML() (any, error) {
	return nil, errors.New("connector secrets cannot be serialized")
}

// CredentialProvider resolves credentials at call time so they never enter durable Flow state.
type CredentialProvider[C any] interface {
	// Resolve returns credentials for the call's connection or an error when unavailable.
	Resolve(Call) (C, error)
}

// StaticCredentialProvider resolves credentials from an in-memory connection map.
type StaticCredentialProvider[C any] map[ConnectionRef]C

// Resolve returns credentials for one connector call without persisting them.
func (provider StaticCredentialProvider[C]) Resolve(call Call) (C, error) {
	credential, ok := provider[call.Connection]
	if !ok {
		var zero C
		return zero, errors.New("connection is not configured")
	}
	return credential, nil
}
