// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package connector

import (
	"errors"
	"fmt"
)

// Credential is intentionally not serializable and never renders its values.
type Credential struct {
	values map[string]string
}

func NewCredential(values map[string]string) Credential {
	copyValues := make(map[string]string, len(values))
	for key, value := range values {
		copyValues[key] = value
	}
	return Credential{values: copyValues}
}

func (credential Credential) Value(name string) (string, bool) {
	value, ok := credential.values[name]
	return value, ok
}

func (Credential) String() string { return "[REDACTED]" }

func (Credential) GoString() string { return "connector.Credential{[REDACTED]}" }

func (Credential) MarshalJSON() ([]byte, error) {
	return nil, errors.New("connector credentials cannot be serialized")
}

func (Credential) MarshalText() ([]byte, error) {
	return nil, errors.New("connector credentials cannot be serialized")
}

func (Credential) MarshalYAML() (any, error) {
	return nil, errors.New("connector credentials cannot be serialized")
}

type CredentialProvider interface {
	Resolve(Call) (Credential, error)
}

type StaticCredentialProvider map[ConnectionRef]Credential

func (provider StaticCredentialProvider) Resolve(call Call) (Credential, error) {
	credential, ok := provider[call.Connection]
	if !ok {
		return Credential{}, NewError(ErrorAuthentication, call.Operation.ConnectorID, call.Operation.OperationID, "connection is not configured", nil)
	}
	return credential, nil
}

func RequiredCredentialValue(credential Credential, name string) (string, error) {
	value, ok := credential.Value(name)
	if !ok || value == "" {
		return "", fmt.Errorf("required credential field %q is missing", name)
	}
	return value, nil
}
