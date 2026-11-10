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
	// ExpiresAt is the stored access credential expiry, when known.
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

// RefreshDriver decides when credentials need renewal and performs the provider request.
type RefreshDriver[C any] interface {
	// RefreshRequired reports whether credentials need a refresh before use, such as a missing access token,
	// unknown expiry, or expiry within the driver's skew. Known-expired credentials are always refreshed.
	RefreshRequired(RefreshState[C]) bool
	// Refresh performs exactly one provider request after durable admission. It must honor context cancellation.
	Refresh(context.Context, RefreshState[C]) (RefreshResult[C], error)
}

// CredentialDecoder validates complete private JSON at the connector boundary.
type CredentialDecoder[C any] func(json.RawMessage) (C, error)

// CredentialEncoder returns complete replacement JSON, retaining renewal material omitted by a provider response.
type CredentialEncoder[C any] func(C) (json.RawMessage, error)

// SharedCredentialUnmarshaler decodes the credential of a shared entry. A host that keeps a shared connection keeps
// the secrets that only refresh or mint a credential, such as a refresh token, a client secret or a signing key, and
// the credential broker serves the credential without them. A connector's generated credential type implements this
// interface, on its pointer, when its manifest declares such secrets.
//
// CredentialResolver decodes a shared entry's credential with UnmarshalSharedCredential when *C implements it, and
// otherwise with the CredentialDecoder, which requires every field. Ordinary entries always use the CredentialDecoder.
type SharedCredentialUnmarshaler interface {
	// UnmarshalSharedCredential decodes and validates contents, a credential without the host-kept secrets, into the
	// receiver. It accepts those secrets when they are present, and its errors never contain credential values.
	UnmarshalSharedCredential(contents []byte) error
}

// refreshTimeout bounds one provider refresh request.
const refreshTimeout = 30 * time.Second

// CredentialResolver resolves credentials per call and refreshes them during actual use when they are known to be
// expired or the driver requires a refresh.
// It coordinates through conditional object writes, not timers or a broker. A local Dex Web connections file adds
// its cross-process refresh lock, because its conditional writes coordinate only one process.
//
// An entry that the loaded document marks with sharedConnectionId resolves from the credential broker instead:
// it caches what the broker serves, reads again when the credential is stale, never reads or writes the
// stored connection and never calls the refresh driver. It decodes the credential with SharedCredentialUnmarshaler
// when *C implements it.
type CredentialResolver[C any] struct {
	store  *ConnectionStore
	key    ConnectionKey
	decode CredentialDecoder[C]
	encode CredentialEncoder[C]
	shared *sharedConnection
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
	resolver := &CredentialResolver[C]{store: store, key: key, decode: decode, encode: encode}
	if store.shared != nil {
		resolver.shared = store.shared.entries[key]
	}
	return resolver, nil
}

// Resolve reads exact current material without refreshing; known-expired credentials require ResolveWithRefresh.
func (provider *CredentialResolver[C]) Resolve(ctx context.Context) (C, error) {
	var zero C
	if provider.shared != nil {
		return provider.resolveShared(ctx)
	}
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

// ResolveWithRefresh admits at most one provider dispatch for a credential generation across application replicas.
// Observers wait or recover immutable results; abandoned admissions never permit provider replay.
//
// With a local Dex Web connections file, a refresh holds the exclusive flock of connections.json.lock beside it
// from reading the credential again through writing the result back, and so does an observer's recovery. A
// process that waited for the lock adopts the credential another process wrote back instead of refreshing again.
// The wait ends with ctx, and after two minutes at most, with an error that wraps ErrRefreshFailed.
func (provider *CredentialResolver[C]) ResolveWithRefresh(ctx context.Context, driver RefreshDriver[C]) (C, error) {
	var zero C
	if ctx == nil || driver == nil {
		return zero, errors.New("credential context and refresh driver are required")
	}
	if provider.shared != nil {
		return provider.resolveShared(ctx)
	}
	if provider.encode == nil {
		return zero, errors.New("project credential refresh requires an encoder")
	}
	refreshLocker, _ := provider.store.objects.(credentialRefreshLocker)
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
			err = provider.recoverExchange(ctx, record, refreshLocker)
			if err == nil || errors.Is(err, ErrConflict) {
				continue
			}
			return zero, err
		case CredentialRefreshing:
			err = provider.recoverExchange(ctx, record, refreshLocker)
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
			credentials, isResolved, err := provider.resolveReady(ctx, record, driver, refreshLocker)
			if err != nil {
				return zero, err
			}
			if isResolved {
				return credentials, nil
			}
		default:
			return zero, errors.New("project credential status is invalid")
		}
	}
}

// ResolveAfterRejection refreshes only when authoritative expiry has elapsed.
// A generic HTTP 401, permission error, or unknown expiry is insufficient evidence for an extra provider mutation.
func (provider *CredentialResolver[C]) ResolveAfterRejection(ctx context.Context, driver RefreshDriver[C]) (C, error) {
	var zero C
	if provider.shared != nil {
		credential, err := provider.shared.credentialAfterRejection(ctx)
		if err != nil {
			return zero, err
		}
		return provider.decodeShared(credential)
	}
	connection, err := provider.store.ReadConnection(ctx, provider.key)
	if err != nil {
		return zero, err
	}
	if connection.ExpiresAt == nil || connection.ExpiresAt.After(provider.store.now()) {
		return zero, errors.New("authentication rejection does not prove credential expiry")
	}
	return provider.ResolveWithRefresh(ctx, driver)
}

func (provider *CredentialResolver[C]) resolveShared(ctx context.Context) (C, error) {
	var zero C
	credential, err := provider.shared.credential(ctx)
	if err != nil {
		return zero, err
	}
	return provider.decodeShared(credential)
}

// decodeShared prefers the credential type's UnmarshalSharedCredential, because the broker omits host-kept secrets
// that the connector's decoder requires.
func (provider *CredentialResolver[C]) decodeShared(credential brokerCredential) (C, error) {
	var credentials, zero C
	if unmarshaler, isSharedAware := any(&credentials).(SharedCredentialUnmarshaler); isSharedAware {
		if err := unmarshaler.UnmarshalSharedCredential(credential.credential); err != nil {
			return zero, errors.New("shared project credential decoding failed")
		}
		return credentials, nil
	}
	credentials, err := provider.decode(credential.credential)
	if err != nil {
		return zero, errors.New("project credential decoding failed: this connector version does not support shared credentials")
	}
	return credentials, nil
}

// resolveReady returns usable credentials of a READY record; isResolved false means read the record again.
func (provider *CredentialResolver[C]) resolveReady(ctx context.Context, record connectionRecord, driver RefreshDriver[C], refreshLocker credentialRefreshLocker) (credentials C, isResolved bool, err error) {
	var zero C
	material, state, isRefreshRequired, err := provider.evaluateReady(ctx, record, driver)
	if err != nil {
		return zero, false, err
	}
	if !isRefreshRequired {
		return state.Credentials, true, nil
	}
	if refreshLocker == nil {
		return provider.refreshReady(ctx, record, material, state, driver)
	}
	return provider.refreshWhileLocked(ctx, driver, refreshLocker)
}

// refreshWhileLocked rereads the record under the lock, adopting another process's write-back instead of refreshing again.
func (provider *CredentialResolver[C]) refreshWhileLocked(ctx context.Context, driver RefreshDriver[C], refreshLocker credentialRefreshLocker) (credentials C, isResolved bool, err error) {
	var zero C
	lock, err := refreshLocker.lockCredentialRefresh(ctx)
	if err != nil {
		return zero, false, err
	}
	defer lock.release()
	record, _, err := provider.store.readRecord(ctx, provider.key)
	if err != nil {
		return zero, false, err
	}
	if record.Status != CredentialReady {
		return zero, false, nil
	}
	material, state, isRefreshRequired, err := provider.evaluateReady(ctx, record, driver)
	if err != nil {
		return zero, false, err
	}
	if !isRefreshRequired {
		return state.Credentials, true, nil
	}
	// A rotated refresh token that cannot be written back would strand every other process.
	if err = refreshLocker.verifyCredentialWriteBack(); err != nil {
		return zero, false, fmt.Errorf("%w: the credential cannot be written back: %w", ErrRefreshFailed, err)
	}
	return provider.refreshReady(ctx, record, material, state, driver)
}

// evaluateReady decodes the credentials of a READY record and decides whether they need a refresh now.
func (provider *CredentialResolver[C]) evaluateReady(ctx context.Context, record connectionRecord, driver RefreshDriver[C]) (material CredentialMaterial, state RefreshState[C], isRefreshRequired bool, err error) {
	material, err = provider.store.readMaterial(ctx, record)
	if err != nil {
		return CredentialMaterial{}, RefreshState[C]{}, false, err
	}
	credentials, err := provider.decode(material.Credentials)
	if err != nil {
		return CredentialMaterial{}, RefreshState[C]{}, false, errors.New("project credential decoding failed")
	}
	now := provider.store.now()
	state = RefreshState[C]{Credentials: credentials, ExpiresAt: material.ExpiresAt, Now: now}
	isKnownExpired := material.ExpiresAt != nil && !material.ExpiresAt.After(now)
	return material, state, isKnownExpired || driver.RefreshRequired(state), nil
}

// refreshReady admits and performs one refresh; isResolved false means the record changed or another caller won.
func (provider *CredentialResolver[C]) refreshReady(ctx context.Context, record connectionRecord, material CredentialMaterial, state RefreshState[C], driver RefreshDriver[C]) (credentials C, isResolved bool, err error) {
	var zero C
	attemptID, err := newAttemptID()
	if err != nil {
		return zero, false, err
	}
	admission, err := provider.store.beginExchange(ctx, provider.key, record.Revision, attemptID, state.Now.Add(refreshTimeout+10*time.Second), CredentialRefreshing)
	if errors.Is(err, ErrConflict) {
		return zero, false, nil
	}
	if err != nil {
		return zero, false, err
	}
	if !admission.ProviderDispatchAllowed {
		return zero, false, nil
	}
	credentials, err = provider.refreshAdmitted(ctx, admission, material, state, driver)
	if err != nil {
		return zero, false, err
	}
	return credentials, true, nil
}

// recoverExchange holds the refresh lock, when there is one, because publishing a recovered credential writes the file.
func (provider *CredentialResolver[C]) recoverExchange(ctx context.Context, record connectionRecord, refreshLocker credentialRefreshLocker) error {
	if refreshLocker != nil {
		lock, err := refreshLocker.lockCredentialRefresh(ctx)
		if err != nil {
			return err
		}
		defer lock.release()
	}
	_, err := provider.store.RecoverCredentialExchange(ctx, exchangeAdmission(record, false))
	return err
}

func (provider *CredentialResolver[C]) refreshAdmitted(ctx context.Context, admission ExchangeAdmission, prior CredentialMaterial, state RefreshState[C], driver RefreshDriver[C]) (C, error) {
	var zero C
	requestContext, cancel := context.WithTimeout(ctx, refreshTimeout)
	result, err := driver.Refresh(requestContext, state)
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
