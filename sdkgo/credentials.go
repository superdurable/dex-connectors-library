// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sdkgo

import (
	"context"
	"errors"
	"time"
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

// CredentialRefreshState describes the current credential material and expiry observed by a provider.
// ExpiresAt is nil when the host has no expiry metadata. Now is captured once for a refresh decision.
type CredentialRefreshState[C any] struct {
	// Credentials is the current connector-specific credential value.
	Credentials C
	// ExpiresAt is the current access credential expiry, when known.
	ExpiresAt *time.Time
	// Now is the provider's refresh-decision time.
	Now time.Time
}

// CredentialRefreshResult contains replacement credential material and its required expiry.
// Providers persist the complete result atomically before returning it to a connector call.
type CredentialRefreshResult[C any] struct {
	// Credentials is the complete replacement credential value.
	Credentials C
	// ExpiresAt is the replacement access credential expiry.
	ExpiresAt time.Time
}

// CredentialRefreshDriver refreshes connector-specific credential material without owning persistence.
// RefreshRequired must be deterministic for one state. Refresh may call the provider and must preserve
// a prior refresh token when the provider omits a rotated value.
type CredentialRefreshDriver[C any] interface {
	// RefreshRequired reports whether state needs a provider refresh before use.
	RefreshRequired(CredentialRefreshState[C]) bool
	// Refresh obtains complete replacement credentials and a future expiry.
	Refresh(context.Context, CredentialRefreshState[C]) (CredentialRefreshResult[C], error)
}

// RefreshingCredentialProvider lets a host coordinate refresh and persistence around a driver.
// Implementations must serialize concurrent refreshes for one logical connection and reload current
// state after acquiring that serialization boundary.
type RefreshingCredentialProvider[C any] interface {
	CredentialProvider[C]
	// ResolveWithRefresh returns usable credentials, refreshing and persisting them when required.
	ResolveWithRefresh(context.Context, Call, CredentialRefreshDriver[C]) (C, error)
}

// ResolveCredential resolves one call through refresh support when the provider implements it.
// A non-refreshing provider retains the CredentialProvider behavior. A nil provider or driver fails.
func ResolveCredential[C any](
	ctx context.Context,
	provider CredentialProvider[C],
	call Call,
	driver CredentialRefreshDriver[C],
) (C, error) {
	var zero C
	if provider == nil {
		return zero, errors.New("credential provider is required")
	}
	if driver == nil {
		return zero, errors.New("credential refresh driver is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	refreshingProvider, ok := provider.(RefreshingCredentialProvider[C])
	if !ok {
		return provider.Resolve(call)
	}
	return refreshingProvider.ResolveWithRefresh(ctx, call, driver)
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
