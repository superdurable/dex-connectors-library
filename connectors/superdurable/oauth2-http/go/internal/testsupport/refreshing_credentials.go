// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package testsupport

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// RefreshingCredentials is an in-memory credential provider for one connection. It follows the project
// credential resolver's rules: known-expired credentials and those the driver asks to refresh are renewed
// once at a time and persisted before they are returned, and a rejection renews only credentials whose
// recorded expiry has elapsed.
type RefreshingCredentials[C any] struct {
	mu          sync.Mutex
	credentials C
	expiresAt   *time.Time
	now         func() time.Time
	refreshes   int
}

// NewRefreshingCredentials stores credentials with their recorded expiry, nil for none. A nil now uses
// the wall clock.
func NewRefreshingCredentials[C any](credentials C, expiresAt *time.Time, now func() time.Time) *RefreshingCredentials[C] {
	if now == nil {
		now = time.Now
	}
	return &RefreshingCredentials[C]{credentials: credentials, expiresAt: expiresAt, now: now}
}

// Current returns the stored credentials and their recorded expiry without refreshing.
func (provider *RefreshingCredentials[C]) Current() (C, *time.Time) {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return provider.credentials, provider.expiresAt
}

// RefreshCount reports how many refreshes the driver completed.
func (provider *RefreshingCredentials[C]) RefreshCount() int {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return provider.refreshes
}

// Resolve returns the stored credentials, or an error once their recorded expiry has elapsed.
func (provider *RefreshingCredentials[C]) Resolve(sdkgo.Call) (C, error) {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if provider.expiresAt != nil && !provider.expiresAt.After(provider.now()) {
		var zero C
		return zero, errors.New("expired credential requires refresh")
	}
	return provider.credentials, nil
}

// ResolveWithRefresh refreshes expired credentials and those the driver asks to renew, then returns them.
func (provider *RefreshingCredentials[C]) ResolveWithRefresh(ctx context.Context, _ sdkgo.Call, driver sdkgo.CredentialRefreshDriver[C]) (C, error) {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return provider.resolveLocked(ctx, driver)
}

// ResolveAfterRejection refreshes only credentials whose recorded expiry has elapsed, as the project
// resolver does: a 401 alone never proves that a token expired.
func (provider *RefreshingCredentials[C]) ResolveAfterRejection(ctx context.Context, _ sdkgo.Call, driver sdkgo.CredentialRefreshDriver[C]) (C, error) {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if provider.expiresAt == nil || provider.expiresAt.After(provider.now()) {
		var zero C
		return zero, errors.New("authentication rejection does not prove credential expiry")
	}
	return provider.resolveLocked(ctx, driver)
}

func (provider *RefreshingCredentials[C]) resolveLocked(ctx context.Context, driver sdkgo.CredentialRefreshDriver[C]) (C, error) {
	now := provider.now()
	state := sdkgo.CredentialRefreshState[C]{Credentials: provider.credentials, ExpiresAt: provider.expiresAt, Now: now}
	isKnownExpired := provider.expiresAt != nil && !provider.expiresAt.After(now)
	if !isKnownExpired && !driver.RefreshRequired(state) {
		return provider.credentials, nil
	}
	result, err := driver.Refresh(ctx, state)
	if err != nil {
		var zero C
		return zero, err
	}
	provider.credentials, provider.expiresAt = result.Credentials, &result.ExpiresAt
	provider.refreshes++
	return provider.credentials, nil
}

var (
	_ sdkgo.RefreshingCredentialProvider[struct{}]         = (*RefreshingCredentials[struct{}])(nil)
	_ sdkgo.RejectedCredentialRefreshingProvider[struct{}] = (*RefreshingCredentials[struct{}])(nil)
)
