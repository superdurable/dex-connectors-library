// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package graphtest

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// CredentialHost is an in-memory credential host for tests. It refreshes through the connector's driver
// whenever the driver requires a refresh, forces one refresh after a rejected access token, keeps every
// replacement, and stops resolving once a refresh requires reauthorization. It serializes all calls.
type CredentialHost[C any] struct {
	mutex                     sync.Mutex
	credentials               C
	expiresAt                 *time.Time
	isReauthorizationRequired bool
}

// NewCredentialHost holds credentials whose access token expires at expiresAt; nil records no expiry.
func NewCredentialHost[C any](credentials C, expiresAt *time.Time) *CredentialHost[C] {
	return &CredentialHost[C]{credentials: credentials, expiresAt: expiresAt}
}

// Resolve returns the held credentials without refreshing them.
func (host *CredentialHost[C]) Resolve(sdkgo.Call) (C, error) {
	host.mutex.Lock()
	defer host.mutex.Unlock()
	if host.isReauthorizationRequired {
		var zero C
		return zero, sdkgo.ErrReauthorizationRequired
	}
	return host.credentials, nil
}

// ResolveWithRefresh refreshes the held credentials when driver requires it and keeps the replacement.
func (host *CredentialHost[C]) ResolveWithRefresh(ctx context.Context, _ sdkgo.Call, driver sdkgo.CredentialRefreshDriver[C]) (C, error) {
	return host.resolve(ctx, driver, false)
}

// ResolveAfterRejection refreshes the held credentials once after a provider rejected their access token.
func (host *CredentialHost[C]) ResolveAfterRejection(ctx context.Context, _ sdkgo.Call, driver sdkgo.CredentialRefreshDriver[C]) (C, error) {
	return host.resolve(ctx, driver, true)
}

// Stored returns the held credentials and whether a refresh required reauthorization.
func (host *CredentialHost[C]) Stored() (C, bool) {
	host.mutex.Lock()
	defer host.mutex.Unlock()
	return host.credentials, host.isReauthorizationRequired
}

func (host *CredentialHost[C]) resolve(ctx context.Context, driver sdkgo.CredentialRefreshDriver[C], isRefreshForced bool) (C, error) {
	host.mutex.Lock()
	defer host.mutex.Unlock()
	var zero C
	if host.isReauthorizationRequired {
		return zero, sdkgo.ErrReauthorizationRequired
	}
	state := sdkgo.CredentialRefreshState[C]{Credentials: host.credentials, ExpiresAt: host.expiresAt, Now: time.Now().UTC()}
	if !isRefreshForced && !driver.RefreshRequired(state) {
		return host.credentials, nil
	}
	result, err := driver.Refresh(ctx, state)
	if err != nil {
		host.isReauthorizationRequired = sdkgo.IsReauthorizationRequired(err)
		return zero, fmt.Errorf("refresh test credentials: %w", err)
	}
	if !result.ExpiresAt.After(state.Now) {
		return zero, errors.New("refreshed test credentials have no future expiry")
	}
	expiresAt := result.ExpiresAt
	host.credentials, host.expiresAt = result.Credentials, &expiresAt
	return result.Credentials, nil
}
