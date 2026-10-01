// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package projectconfig

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// ErrExchangePending means a prior invocation owns provider dispatch; observers may recover but must never replay it.
var ErrExchangePending = errors.New("credential exchange is pending")

// ErrReauthorizationRequired prevents reuse after a lost, ambiguous, or failed exchange.
var ErrReauthorizationRequired = errors.New("project connection requires reauthorization")

// ExchangeAdmission identifies one fenced exchange without exposing credentials or permitting durable replay of provider dispatch.
type ExchangeAdmission struct {
	// Key identifies the admitted connection.
	Key ConnectionKey
	// AttemptID is assigned once by the OAuth operation or on-use refresh caller.
	AttemptID string
	// Fence binds every result to the credential generation accepted before dispatch.
	Fence uint64
	// Deadline bounds observers' wait for an immutable result; expiry never permits another provider dispatch.
	Deadline time.Time
	// ProviderDispatchAllowed is true only for the invocation that won a conditional admission write.
	// Never persist or reconstruct this permission; retry observers receive false.
	ProviderDispatchAllowed bool `json:"-" yaml:"-"`
}

// BeginCredentialExchange conditionally admits one OAuth exchange before calling a provider.
// A new connection uses expectedRevision zero. An already admitted attempt returns dispatch permission false.
// Callers must persist attemptID before this call and enforce actor, state, and OAuth expiry independently.
func (store *ConnectionStore) BeginCredentialExchange(ctx context.Context, key ConnectionKey, expectedRevision uint64, attemptID string, deadline time.Time) (ExchangeAdmission, error) {
	return store.beginExchange(ctx, key, expectedRevision, attemptID, deadline, CredentialAuthorizing)
}

// CommitCredentialExchange persists immutable result material before conditionally publishing its exact version.
// Unknown writes are reconciled; later replacement or revocation always prevents this exchange from publishing.
func (store *ConnectionStore) CommitCredentialExchange(ctx context.Context, admission ExchangeAdmission, material CredentialMaterial) (Connection, error) {
	record, object, err := store.readAdmitted(ctx, admission)
	if errors.Is(err, ErrConflict) {
		if isComplete, completedErr := store.isCompletedExchange(ctx, record, admission.AttemptID, admission.Fence); completedErr != nil {
			return Connection{}, completedErr
		} else if isComplete {
			return record.Connection, nil
		}
	}
	if err != nil {
		return Connection{}, err
	}
	if err = validateMaterial(material); err != nil {
		return record.Connection, err
	}
	envelope := credentialEnvelope{SchemaVersion: connectionSchema, Scope: store.scope, Key: record.Key, Fence: record.Fence, AttemptID: admission.AttemptID, Credentials: material.Credentials, ExpiresAt: material.ExpiresAt, ModuleVersion: material.ModuleVersion, AuthMethod: material.AuthMethod}
	contents, err := json.Marshal(envelope)
	if err != nil {
		return record.Connection, errors.New("credential result encoding failed")
	}
	reference, err := store.createImmutable(ctx, record.Mutation.ResultKey, contents)
	if err != nil {
		return record.Connection, err
	}
	return store.publishExchange(ctx, record, object.ETag, reference, envelope)
}

// RecoverCredentialExchange reconciles a persisted result without ever calling the provider.
// Before Deadline it returns ErrExchangePending. Afterward, absence of a result fences the connection for reauthorization.
func (store *ConnectionStore) RecoverCredentialExchange(ctx context.Context, admission ExchangeAdmission) (Connection, error) {
	record, object, err := store.readAdmitted(ctx, admission)
	if errors.Is(err, ErrConflict) {
		if isComplete, completedErr := store.isCompletedExchange(ctx, record, admission.AttemptID, admission.Fence); completedErr != nil {
			return Connection{}, completedErr
		} else if isComplete {
			return record.Connection, nil
		}
	}
	if err != nil {
		return Connection{}, err
	}
	result, err := store.objects.ReadObject(ctx, record.Mutation.ResultKey, "")
	if errors.Is(err, ErrObjectNotFound) {
		if store.now().Before(record.Mutation.Deadline) {
			return record.Connection, ErrExchangePending
		}
		connection, failureErr := store.failExchange(ctx, record, object.ETag)
		if failureErr != nil {
			return connection, failureErr
		}
		return connection, ErrReauthorizationRequired
	}
	if err != nil {
		return record.Connection, err
	}
	var envelope credentialEnvelope
	if strictJSON(result.Contents, &envelope) != nil || result.Version == "" || result.Version == "null" || envelope.SchemaVersion != connectionSchema || envelope.Scope != store.scope || envelope.Key != record.Key || envelope.Fence != record.Fence || envelope.AttemptID != admission.AttemptID {
		return record.Connection, errors.New("credential exchange result identity is invalid")
	}
	if err = validateMaterial(CredentialMaterial{Credentials: envelope.Credentials, ExpiresAt: envelope.ExpiresAt, ModuleVersion: envelope.ModuleVersion, AuthMethod: envelope.AuthMethod}); err != nil {
		return record.Connection, err
	}
	reference := objectReference{Key: result.Key, Version: result.Version, Digest: digestBytes(result.Contents)}
	return store.publishExchange(ctx, record, object.ETag, reference, envelope)
}

// FailCredentialExchange fences an unsuccessful or ambiguous provider exchange without retrying the provider.
// Call RecoverCredentialExchange first when an immutable result write may already have succeeded.
func (store *ConnectionStore) FailCredentialExchange(ctx context.Context, admission ExchangeAdmission) error {
	record, object, err := store.readAdmitted(ctx, admission)
	if err != nil {
		return err
	}
	_, err = store.failExchange(ctx, record, object.ETag)
	return err
}

func (store *ConnectionStore) beginExchange(ctx context.Context, key ConnectionKey, expectedRevision uint64, attemptID string, deadline time.Time, status CredentialStatus) (ExchangeAdmission, error) {
	if !scopeIdentity.MatchString(attemptID) {
		return ExchangeAdmission{}, errors.New("invalid credential exchange identity or deadline")
	}
	path, err := store.connectionPath(key)
	if err != nil {
		return ExchangeAdmission{}, err
	}
	record, object, err := store.readRecord(ctx, key)
	if errors.Is(err, ErrObjectNotFound) && expectedRevision == 0 && status == CredentialAuthorizing {
		record = connectionRecord{SchemaVersion: connectionSchema, Connection: Connection{Key: key, Scope: store.scope}}
	} else if err != nil {
		return ExchangeAdmission{}, err
	}
	if isComplete, completedErr := store.isCompletedExchange(ctx, record, attemptID, record.Fence); completedErr != nil {
		return ExchangeAdmission{}, completedErr
	} else if isComplete {
		return ExchangeAdmission{Key: key, AttemptID: attemptID, Fence: record.Fence, Deadline: deadline}, nil
	}
	if record.Mutation != nil && record.Mutation.ID == attemptID && record.Mutation.Kind == string(status) {
		return exchangeAdmission(record, false), nil
	}
	if !deadline.After(store.now()) || deadline.After(store.now().Add(15*time.Minute)) {
		return ExchangeAdmission{}, errors.New("invalid credential exchange deadline")
	}
	if record.Revision != expectedRevision {
		return ExchangeAdmission{}, ErrConflict
	}
	if status == CredentialRefreshing && record.Status != CredentialReady {
		return ExchangeAdmission{}, ErrConflict
	}
	invocationID, err := newAttemptID()
	if err != nil {
		return ExchangeAdmission{}, err
	}
	record.Mutation = &credentialMutation{Kind: string(status), InvocationID: invocationID, ID: attemptID, Fence: record.Fence + 1, BaseRevision: record.Revision, Deadline: deadline, ResultKey: path + "/exchange-results/" + attemptID}
	record.Revision++
	record.Fence++
	record.Status = status
	if _, err = store.writeRecord(ctx, record, object.ETag); err != nil {
		if errors.Is(err, ErrConflict) {
			observed, _, readErr := store.readRecord(ctx, key)
			if readErr != nil {
				return ExchangeAdmission{}, errors.Join(err, readErr)
			}
			if observed.Mutation != nil && observed.Mutation.ID == attemptID && observed.Mutation.Kind == string(status) && observed.Mutation.BaseRevision == expectedRevision && observed.Mutation.Deadline.Equal(deadline) {
				return exchangeAdmission(observed, false), nil
			}
		}
		return ExchangeAdmission{}, err
	}
	return exchangeAdmission(record, true), nil
}

func (store *ConnectionStore) readAdmitted(ctx context.Context, admission ExchangeAdmission) (connectionRecord, Object, error) {
	record, object, err := store.readRecord(ctx, admission.Key)
	if err != nil {
		return record, object, err
	}
	if record.Mutation == nil || record.Mutation.ID != admission.AttemptID || record.Fence != admission.Fence || !record.Mutation.Deadline.Equal(admission.Deadline) {
		return record, object, ErrConflict
	}
	return record, object, nil
}

func (store *ConnectionStore) publishExchange(ctx context.Context, record connectionRecord, etag string, reference objectReference, envelope credentialEnvelope) (Connection, error) {
	record.Revision++
	record.Status = CredentialReady
	record.Credential, record.Mutation = &reference, nil
	record.ExpiresAt, record.ModuleVersion, record.AuthMethod = envelope.ExpiresAt, envelope.ModuleVersion, envelope.AuthMethod
	_, err := store.writeRecord(ctx, record, etag)
	return record.Connection, err
}

func (store *ConnectionStore) failExchange(ctx context.Context, record connectionRecord, etag string) (Connection, error) {
	record.Revision++
	record.Fence++
	record.Status = CredentialReauthorizationRequired
	record.Mutation = nil
	_, err := store.writeRecord(ctx, record, etag)
	return record.Connection, err
}

func exchangeAdmission(record connectionRecord, canDispatch bool) ExchangeAdmission {
	return ExchangeAdmission{Key: record.Key, AttemptID: record.Mutation.ID, Fence: record.Fence, Deadline: record.Mutation.Deadline, ProviderDispatchAllowed: canDispatch}
}

func (store *ConnectionStore) isCompletedExchange(ctx context.Context, record connectionRecord, attemptID string, fence uint64) (bool, error) {
	if record.Status != CredentialReady || record.Fence != fence || record.Credential == nil {
		return false, nil
	}
	path, err := store.connectionPath(record.Key)
	if err != nil {
		return false, err
	}
	if record.Credential.Key != path+"/exchange-results/"+attemptID {
		return false, nil
	}
	object, err := store.objects.ReadObject(ctx, record.Credential.Key, record.Credential.Version)
	if err != nil {
		return false, err
	}
	var envelope credentialEnvelope
	if digestBytes(object.Contents) != record.Credential.Digest || strictJSON(object.Contents, &envelope) != nil || envelope.SchemaVersion != connectionSchema || envelope.Scope != store.scope || envelope.Key != record.Key || envelope.Fence != fence || envelope.AttemptID != attemptID {
		return false, errors.New("completed credential exchange identity differs")
	}
	return true, nil
}
