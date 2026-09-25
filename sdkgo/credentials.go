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

func NewSecretString(value string) SecretString { return SecretString{value: value} }

// Reveal returns the secret only at the provider request boundary.
func (secret SecretString) Reveal() string { return secret.value }

func (SecretString) String() string { return "[REDACTED]" }

func (SecretString) GoString() string { return "sdkgo.SecretString{[REDACTED]}" }

func (SecretString) MarshalJSON() ([]byte, error) {
	return nil, errors.New("connector secrets cannot be serialized")
}

func (SecretString) MarshalText() ([]byte, error) {
	return nil, errors.New("connector secrets cannot be serialized")
}

func (SecretString) MarshalYAML() (any, error) {
	return nil, errors.New("connector secrets cannot be serialized")
}

type CredentialProvider[C any] interface {
	Resolve(Call) (C, error)
}

type StaticCredentialProvider[C any] map[ConnectionRef]C

func (provider StaticCredentialProvider[C]) Resolve(call Call) (C, error) {
	credential, ok := provider[call.Connection]
	if !ok {
		var zero C
		return zero, errors.New("connection is not configured")
	}
	return credential, nil
}
