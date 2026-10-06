// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package testsupport keeps project credentials and storage in memory for tests whose subject is the
// connector or its example, not project storage.
package testsupport

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// RefreshingCredentialSource keeps one connection's credentials in memory and is safe for concurrent use.
// It refreshes them with the connector's refresh driver when the driver requires it or the provider
// rejected them after their recorded expiry, keeps them unchanged when a refresh fails, and fails every
// later call once a refresh reports that reauthorization is required.
type RefreshingCredentialSource[C any] struct {
	mutex                     sync.Mutex
	credentials               C
	expiresAt                 *time.Time
	now                       func() time.Time
	isReauthorizationRequired bool
}

// errRejectionWithoutExpiry mirrors projectconfig, which refreshes after a rejection only once expiry has passed.
var errRejectionWithoutExpiry = errors.New("authentication rejection does not prove credential expiry")

// NewRefreshingCredentialSource holds credentials whose access token expires at expiresAt; a nil expiresAt
// records no expiry.
func NewRefreshingCredentialSource[C any](credentials C, expiresAt *time.Time) *RefreshingCredentialSource[C] {
	return NewRefreshingCredentialSourceWithClock(credentials, expiresAt, time.Now)
}

// NewRefreshingCredentialSourceWithClock is NewRefreshingCredentialSource whose expiry decisions read now.
func NewRefreshingCredentialSourceWithClock[C any](credentials C, expiresAt *time.Time, now func() time.Time) *RefreshingCredentialSource[C] {
	return &RefreshingCredentialSource[C]{credentials: credentials, expiresAt: expiresAt, now: now}
}

// Resolve returns the current credentials without refreshing them.
func (source *RefreshingCredentialSource[C]) Resolve(sdkgo.Call) (C, error) {
	source.mutex.Lock()
	defer source.mutex.Unlock()
	if source.isReauthorizationRequired {
		var zero C
		return zero, sdkgo.ErrReauthorizationRequired
	}
	return source.credentials, nil
}

// ResolveWithRefresh refreshes the credentials first when driver reports that a refresh is required.
func (source *RefreshingCredentialSource[C]) ResolveWithRefresh(
	ctx context.Context,
	_ sdkgo.Call,
	driver sdkgo.CredentialRefreshDriver[C],
) (C, error) {
	source.mutex.Lock()
	defer source.mutex.Unlock()
	if source.isReauthorizationRequired {
		var zero C
		return zero, sdkgo.ErrReauthorizationRequired
	}
	if !driver.RefreshRequired(source.refreshState()) {
		return source.credentials, nil
	}
	return source.refresh(ctx, driver)
}

// ResolveAfterRejection refreshes the credentials once after the provider rejected them, but only when their
// recorded expiry has passed, as a project connection does; otherwise it returns an error.
func (source *RefreshingCredentialSource[C]) ResolveAfterRejection(
	ctx context.Context,
	_ sdkgo.Call,
	driver sdkgo.CredentialRefreshDriver[C],
) (C, error) {
	source.mutex.Lock()
	defer source.mutex.Unlock()
	if source.isReauthorizationRequired {
		var zero C
		return zero, sdkgo.ErrReauthorizationRequired
	}
	if source.expiresAt == nil || source.expiresAt.After(source.now()) {
		var zero C
		return zero, errRejectionWithoutExpiry
	}
	return source.refresh(ctx, driver)
}

// Current returns the stored credentials and their recorded expiry.
func (source *RefreshingCredentialSource[C]) Current() (C, *time.Time) {
	source.mutex.Lock()
	defer source.mutex.Unlock()
	return source.credentials, source.expiresAt
}

// IsReauthorizationRequired reports whether a refresh required the connection to be authorized again.
func (source *RefreshingCredentialSource[C]) IsReauthorizationRequired() bool {
	source.mutex.Lock()
	defer source.mutex.Unlock()
	return source.isReauthorizationRequired
}

// refresh runs one driver refresh; the caller holds source.mutex.
func (source *RefreshingCredentialSource[C]) refresh(ctx context.Context, driver sdkgo.CredentialRefreshDriver[C]) (C, error) {
	result, err := driver.Refresh(ctx, source.refreshState())
	if err != nil {
		source.isReauthorizationRequired = sdkgo.IsReauthorizationRequired(err)
		var zero C
		return zero, err
	}
	source.credentials, source.expiresAt = result.Credentials, &result.ExpiresAt
	return result.Credentials, nil
}

func (source *RefreshingCredentialSource[C]) refreshState() sdkgo.CredentialRefreshState[C] {
	return sdkgo.CredentialRefreshState[C]{Credentials: source.credentials, ExpiresAt: source.expiresAt, Now: source.now()}
}
