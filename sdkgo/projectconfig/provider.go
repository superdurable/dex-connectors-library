// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package projectconfig

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrRefreshFailed reports a credential refresh that failed without proving the credential invalid.
// The prior credential stays authorized, and a later call refreshes again.
var ErrRefreshFailed = errors.New("project credential refresh failed; the current authorization is kept")

// RefreshState carries private current credentials only at the provider boundary.
type RefreshState[C any] struct {
	// Credentials contains complete current credential material.
	Credentials C
	// ExpiresAt is the authoritative known-expired access credential expiry.
	ExpiresAt *time.Time
	// Now captures the caller's refresh decision time.
	Now time.Time
}

// RefreshResult contains complete replacement material for immutable persistence before publication.
type RefreshResult[C any] struct {
	// Credentials retains renewal material when the provider omitted a replacement.
	Credentials C
	// ExpiresAt must be in the future.
	ExpiresAt time.Time
}

// RefreshFunc performs exactly one provider request after durable admission. Implementations must honor context cancellation.
type RefreshFunc[C any] func(context.Context, RefreshState[C]) (RefreshResult[C], error)

// CredentialDecoder validates complete private JSON at the connector boundary.
type CredentialDecoder[C any] func(json.RawMessage) (C, error)

// CredentialEncoder returns complete replacement JSON, retaining renewal material omitted by a provider response.
type CredentialEncoder[C any] func(C) (json.RawMessage, error)

// refreshTimeout bounds one provider refresh request.
const refreshTimeout = 30 * time.Second

// CredentialResolver resolves credentials per call and refreshes only known-expired credentials during actual use.
// It coordinates through conditional object writes, not process-local locks, timers, or a broker.
type CredentialResolver[C any] struct {
	store  *ConnectionStore
	key    ConnectionKey
	decode CredentialDecoder[C]
	encode CredentialEncoder[C]
}

// NewCredentialResolver binds one project connection without reading credentials or contacting a provider.
// A nil encode resolves credentials that are never refreshed; ResolveWithRefresh then fails.
func NewCredentialResolver[C any](store *ConnectionStore, key ConnectionKey, decode CredentialDecoder[C], encode CredentialEncoder[C]) (*CredentialResolver[C], error) {
	if store == nil || decode == nil {
		return nil, errors.New("project credential store and decoder are required")
	}
	if _, err := store.connectionPath(key); err != nil {
		return nil, err
	}
	return &CredentialResolver[C]{store: store, key: key, decode: decode, encode: encode}, nil
}

// Resolve reads exact current material without refreshing; known-expired credentials require ResolveWithRefresh.
func (provider *CredentialResolver[C]) Resolve(ctx context.Context) (C, error) {
	var zero C
	material, connection, err := provider.store.ReadCredentialMaterial(ctx, provider.key)
	if err != nil {
		return zero, err
	}
	if connection.ExpiresAt != nil && !connection.ExpiresAt.After(provider.store.now()) {
		return zero, errors.New("expired project credential requires refresh")
	}
	credentials, err := provider.decode(material.Credentials)
	if err != nil {
		return zero, errors.New("project credential decoding failed")
	}
	return credentials, nil
}

// ResolveWithRefresh admits at most one provider dispatch for an expired credential generation across application replicas.
// Observers wait or recover immutable results; abandoned admissions require reauthorization and never permit provider replay.
func (provider *CredentialResolver[C]) ResolveWithRefresh(ctx context.Context, driver RefreshFunc[C]) (C, error) {
	var zero C
	if ctx == nil || driver == nil {
		return zero, errors.New("credential context and refresh driver are required")
	}
	if provider.encode == nil {
		return zero, errors.New("project credential refresh requires an encoder")
	}
	for {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		record, _, err := provider.store.readRecord(ctx, provider.key)
		if err != nil {
			return zero, err
		}
		switch record.Status {
		case CredentialRevoked, CredentialReauthorizationRequired:
			return zero, ErrReauthorizationRequired
		case CredentialAuthorizing:
			if provider.store.now().Before(record.Mutation.Deadline) {
				return zero, ErrExchangePending
			}
			_, err = provider.store.RecoverCredentialExchange(ctx, exchangeAdmission(record, false))
			if err == nil || errors.Is(err, ErrConflict) {
				continue
			}
			return zero, err
		case CredentialRefreshing:
			_, err = provider.store.RecoverCredentialExchange(ctx, exchangeAdmission(record, false))
			if err == nil || errors.Is(err, ErrConflict) {
				continue
			}
			if errors.Is(err, ErrReauthorizationRequired) {
				return zero, ErrReauthorizationRequired
			}
			if !errors.Is(err, ErrExchangePending) {
				return zero, err
			}
			if err = waitForExchange(ctx); err != nil {
				return zero, err
			}
		case CredentialReady:
			material, err := provider.store.readMaterial(ctx, record)
			if err != nil {
				return zero, err
			}
			credentials, err := provider.decode(material.Credentials)
			if err != nil {
				return zero, errors.New("project credential decoding failed")
			}
			now := provider.store.now()
			if material.ExpiresAt == nil || material.ExpiresAt.After(now) {
				return credentials, nil
			}
			attemptID, err := newAttemptID()
			if err != nil {
				return zero, err
			}
			admission, err := provider.store.beginExchange(ctx, provider.key, record.Revision, attemptID, now.Add(refreshTimeout+10*time.Second), CredentialRefreshing)
			if errors.Is(err, ErrConflict) {
				continue
			}
			if err != nil {
				return zero, err
			}
			if !admission.ProviderDispatchAllowed {
				continue
			}
			return provider.refreshAdmitted(ctx, admission, material, credentials, driver)
		default:
			return zero, errors.New("project credential status is invalid")
		}
	}
}

// ResolveAfterRejection refreshes only when authoritative expiry has elapsed.
// A generic HTTP 401, permission error, or unknown expiry is insufficient evidence for an extra provider mutation.
func (provider *CredentialResolver[C]) ResolveAfterRejection(ctx context.Context, driver RefreshFunc[C]) (C, error) {
	var zero C
	connection, err := provider.store.ReadConnection(ctx, provider.key)
	if err != nil {
		return zero, err
	}
	if connection.ExpiresAt == nil || connection.ExpiresAt.After(provider.store.now()) {
		return zero, errors.New("authentication rejection does not prove credential expiry")
	}
	return provider.ResolveWithRefresh(ctx, driver)
}

func (provider *CredentialResolver[C]) refreshAdmitted(ctx context.Context, admission ExchangeAdmission, prior CredentialMaterial, credentials C, driver RefreshFunc[C]) (C, error) {
	var zero C
	requestContext, cancel := context.WithTimeout(ctx, refreshTimeout)
	result, err := driver(requestContext, RefreshState[C]{Credentials: credentials, ExpiresAt: prior.ExpiresAt, Now: provider.store.now()})
	cancel()
	if errors.Is(err, ErrReauthorizationRequired) {
		return zero, provider.failRefresh(ctx, admission)
	}
	if err != nil {
		return zero, provider.abortRefresh(ctx, admission, err)
	}
	if !result.ExpiresAt.After(provider.store.now()) {
		return zero, provider.abortRefresh(ctx, admission, errors.New("refreshed credential has no future expiry"))
	}
	encoded, err := provider.encode(result.Credentials)
	if err != nil {
		return zero, provider.abortRefresh(ctx, admission, errors.New("refreshed credential encoding failed"))
	}
	material := CredentialMaterial{Credentials: encoded, ExpiresAt: &result.ExpiresAt, AuthMethod: prior.AuthMethod}
	if err = validateMaterial(material); err != nil {
		return zero, provider.abortRefresh(ctx, admission, err)
	}
	if _, err = provider.store.CommitCredentialExchange(ctx, admission, material); err != nil {
		return zero, err
	}
	return result.Credentials, nil
}

// abortRefresh keeps the prior credential usable after a refresh that a later call may retry.
// The write uses a short detached deadline so a canceled Step still releases its admission.
func (provider *CredentialResolver[C]) abortRefresh(ctx context.Context, admission ExchangeAdmission, cause error) error {
	restoreContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := provider.store.abortRefresh(restoreContext, admission); err != nil {
		return errors.Join(fmt.Errorf("%w: %w", ErrRefreshFailed, cause), err)
	}
	return fmt.Errorf("%w: %w", ErrRefreshFailed, cause)
}

// failRefresh fences the connection after a provider proved the credential can no longer be renewed.
func (provider *CredentialResolver[C]) failRefresh(ctx context.Context, admission ExchangeAdmission) error {
	// Canceled callers leave durable admission for later reconciliation without starting a detached worker.
	if err := provider.store.FailCredentialExchange(ctx, admission); err != nil {
		return err
	}
	return ErrReauthorizationRequired
}

func waitForExchange(ctx context.Context) error {
	timer := time.NewTimer(100 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
