// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package projectconfig

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
)

const connectionSchema = "connectors.dex.dev/project-connection/v1alpha2"

// CredentialStatus identifies the authoritative connection admission state.
type CredentialStatus string

const (
	// CredentialReady permits credential use, subject to expiry.
	CredentialReady CredentialStatus = "READY"
	// CredentialAuthorizing fences a new OAuth exchange before any provider request.
	CredentialAuthorizing CredentialStatus = "AUTHORIZING"
	// CredentialRefreshing prevents concurrent provider rotation while its admitted caller is active.
	CredentialRefreshing CredentialStatus = "REFRESHING"
	// CredentialReauthorizationRequired prevents reuse after an ambiguous or terminal provider exchange.
	CredentialReauthorizationRequired CredentialStatus = "REAUTHORIZATION_REQUIRED"
	// CredentialRevoked prevents provider use until a replacement is accepted.
	CredentialRevoked CredentialStatus = "REVOKED"
)

// ConnectionKey separates connectors sharing the same provider and named connection.
type ConnectionKey struct {
	// ConnectorID is the manifest's immutable connector identifier.
	ConnectorID string `json:"connectorId"`
	// ConnectionName is the application-declared named connection.
	ConnectionName string `json:"connectionName"`
}

// Connection contains safe authoritative metadata, never credential material or temporary OAuth secrets.
type Connection struct {
	// Key identifies the connection within Scope.
	Key ConnectionKey `json:"connection"`
	// Scope identifies its project and Live or Preview owner.
	Scope Scope `json:"scope"`
	// Revision changes after every committed mutation, including refresh admission.
	Revision uint64 `json:"revision"`
	// Fence changes for a new credential mutation, preventing stale refresh results from replacing later credentials.
	Fence uint64 `json:"fence"`
	// Status controls credential admission.
	Status CredentialStatus `json:"status"`
	// ExpiresAt is absent when the provider did not report expiry; absence never triggers speculative refresh.
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	// AuthMethod is the manifest authorization method used to produce the credentials.
	AuthMethod string `json:"authMethod"`
}

// CredentialMaterial contains private provider-boundary bytes. Never persist it in a Flow or send it to a browser.
type CredentialMaterial struct {
	// Credentials is a complete encoded credential object, including renewal material when available.
	Credentials json.RawMessage
	// ExpiresAt is the access credential's exact expiry, or nil when unknown.
	ExpiresAt *time.Time
	// AuthMethod is the verified selected authorization method.
	AuthMethod string
}

// String hides all private material during formatting.
func (CredentialMaterial) String() string { return "projectconfig.CredentialMaterial{[REDACTED]}" }

// GoString hides all private material during Go-syntax formatting.
func (material CredentialMaterial) GoString() string { return material.String() }

// MarshalJSON rejects accidental transport of credential material.
func (CredentialMaterial) MarshalJSON() ([]byte, error) {
	return nil, errors.New("private credentials cannot be serialized")
}

// ConnectionStoreConfig fixes the project boundary and its durable object store.
type ConnectionStoreConfig struct {
	// Objects owns authenticated, versioned conditional storage.
	Objects ObjectStore
	// Scope is trusted deployment configuration, never a request-selected storage target.
	Scope Scope
}

// ConnectionStore shares mutation fencing between Dex Web and every application replica.
type ConnectionStore struct {
	objects ObjectStore
	scope   Scope
	prefix  string
	now     func() time.Time
}

type objectReference struct {
	Key     string `json:"key"`
	Version string `json:"version"`
	Digest  string `json:"digest"`
}
type credentialMutation struct {
	Kind         string    `json:"kind"`
	InvocationID string    `json:"invocationId"`
	ID           string    `json:"id"`
	Fence        uint64    `json:"fence"`
	BaseRevision uint64    `json:"baseRevision"`
	Deadline     time.Time `json:"deadline"`
	ResultKey    string    `json:"resultKey"`
}
type connectionRecord struct {
	SchemaVersion string `json:"schemaVersion"`
	Connection
	Credential *objectReference    `json:"credential,omitempty"`
	Mutation   *credentialMutation `json:"mutation,omitempty"`
}
type credentialEnvelope struct {
	SchemaVersion string          `json:"schemaVersion"`
	Scope         Scope           `json:"scope"`
	Key           ConnectionKey   `json:"connection"`
	Fence         uint64          `json:"fence"`
	AttemptID     string          `json:"attemptId,omitempty"`
	Credentials   json.RawMessage `json:"credentials"`
	ExpiresAt     *time.Time      `json:"expiresAt,omitempty"`
	AuthMethod    string          `json:"authMethod"`
}

// NewConnectionStore validates the fixed scope without creating any connections or calling a provider.
func NewConnectionStore(config *ConnectionStoreConfig) (*ConnectionStore, error) {
	if config == nil || config.Objects == nil {
		return nil, errors.New("project connection object store is required")
	}
	prefix, err := config.Scope.Prefix()
	if err != nil {
		return nil, err
	}
	return &ConnectionStore{objects: config.Objects, scope: config.Scope, prefix: prefix, now: time.Now}, nil
}

// ReadConnection reads authoritative safe metadata without refreshing or creating credentials.
func (store *ConnectionStore) ReadConnection(ctx context.Context, key ConnectionKey) (Connection, error) {
	record, _, err := store.readRecord(ctx, key)
	return record.Connection, err
}

// ReadCredentialMaterial reads exact private material for trusted Dex Web setup commands or keep-field updates.
// This method never refreshes; callers must enforce expiry and must not expose the returned bytes to a browser.
func (store *ConnectionStore) ReadCredentialMaterial(ctx context.Context, key ConnectionKey) (CredentialMaterial, Connection, error) {
	record, _, err := store.readRecord(ctx, key)
	if err != nil {
		return CredentialMaterial{}, Connection{}, err
	}
	if record.Status != CredentialReady {
		return CredentialMaterial{}, record.Connection, errors.New("project credential is unavailable")
	}
	material, err := store.readMaterial(ctx, record)
	return material, record.Connection, err
}

// ReplaceCredential atomically fences prior authorization or refresh work using expectedRevision; zero creates a new connection.
// Immutable candidate writes are reconciled before the head changes; failures never overwrite another writer.
func (store *ConnectionStore) ReplaceCredential(ctx context.Context, key ConnectionKey, expectedRevision uint64, material CredentialMaterial) (Connection, error) {
	path, err := store.connectionPath(key)
	if err != nil {
		return Connection{}, err
	}
	if err = validateMaterial(material); err != nil {
		return Connection{}, err
	}
	record, object, err := store.readRecord(ctx, key)
	if errors.Is(err, ErrObjectNotFound) && expectedRevision == 0 {
		record = connectionRecord{SchemaVersion: connectionSchema, Connection: Connection{Key: key, Scope: store.scope}}
	} else if err != nil {
		return Connection{}, err
	}
	if record.Revision != expectedRevision {
		return record.Connection, ErrConflict
	}
	envelope := credentialEnvelope{SchemaVersion: connectionSchema, Scope: store.scope, Key: key, Fence: record.Fence + 1, Credentials: material.Credentials, ExpiresAt: material.ExpiresAt, AuthMethod: material.AuthMethod}
	contents, err := json.Marshal(envelope)
	if err != nil {
		return Connection{}, errors.New("credential encoding failed")
	}
	ref, err := store.createImmutable(ctx, path+"/credentials/"+digestBytes(contents), contents)
	if err != nil {
		return record.Connection, err
	}
	record.Credential, record.Mutation = &ref, nil
	record.Revision++
	record.Fence++
	record.Status = CredentialReady
	record.ExpiresAt, record.AuthMethod = material.ExpiresAt, material.AuthMethod
	_, err = store.writeRecord(ctx, record, object.ETag)
	return record.Connection, err
}

// RevokeConnection fences pending refresh and removes the active credential reference; historical versions remain under storage retention.
func (store *ConnectionStore) RevokeConnection(ctx context.Context, key ConnectionKey, expectedRevision uint64) (Connection, error) {
	record, object, err := store.readRecord(ctx, key)
	if err != nil {
		return Connection{}, err
	}
	if record.Revision != expectedRevision {
		return record.Connection, ErrConflict
	}
	record.Revision++
	record.Fence++
	record.Status = CredentialRevoked
	record.Credential, record.Mutation, record.ExpiresAt = nil, nil, nil
	_, err = store.writeRecord(ctx, record, object.ETag)
	return record.Connection, err
}

func (store *ConnectionStore) readRecord(ctx context.Context, key ConnectionKey) (connectionRecord, Object, error) {
	path, err := store.connectionPath(key)
	if err != nil {
		return connectionRecord{}, Object{}, err
	}
	object, err := store.objects.ReadObject(ctx, path+"/head", "")
	if err != nil {
		return connectionRecord{}, Object{}, err
	}
	var record connectionRecord
	if strictJSON(object.Contents, &record) != nil || record.SchemaVersion != connectionSchema || record.Scope != store.scope || record.Key != key || record.Revision == 0 || record.Fence == 0 || object.Version == "" || object.ETag == "" {
		return connectionRecord{}, Object{}, errors.New("project connection identity is invalid")
	}
	switch record.Status {
	case CredentialReady, CredentialRefreshing:
		if record.Credential == nil || !strings.HasPrefix(record.Credential.Key, path+"/") || record.Credential.Version == "" || len(record.Credential.Digest) != 64 {
			return connectionRecord{}, Object{}, errors.New("project credential reference is invalid")
		}
	case CredentialAuthorizing, CredentialReauthorizationRequired:
		if record.Credential != nil && (!strings.HasPrefix(record.Credential.Key, path+"/") || record.Credential.Version == "" || len(record.Credential.Digest) != 64) {
			return connectionRecord{}, Object{}, errors.New("project credential reference is invalid")
		}
	case CredentialRevoked:
		if record.Credential != nil || record.Mutation != nil {
			return connectionRecord{}, Object{}, errors.New("revoked connection retains active material")
		}
	default:
		return connectionRecord{}, Object{}, errors.New("project credential status is invalid")
	}
	if record.Status == CredentialRefreshing || record.Status == CredentialAuthorizing {
		if record.Mutation == nil || !scopeIdentity.MatchString(record.Mutation.ID) || record.Mutation.InvocationID == "" || record.Mutation.Kind != string(record.Status) || record.Mutation.Fence != record.Fence || record.Mutation.BaseRevision+1 != record.Revision || record.Mutation.Deadline.IsZero() || record.Mutation.ResultKey != path+"/exchange-results/"+record.Mutation.ID {
			return connectionRecord{}, Object{}, errors.New("credential refresh admission is invalid")
		}
	} else if record.Mutation != nil {
		return connectionRecord{}, Object{}, errors.New("inactive credential refresh admission")
	}
	return record, object, nil
}

func (store *ConnectionStore) writeRecord(ctx context.Context, record connectionRecord, etag string) (Object, error) {
	path, err := store.connectionPath(record.Key)
	if err != nil {
		return Object{}, err
	}
	contents, err := json.Marshal(record)
	if err != nil {
		return Object{}, errors.New("project connection encoding failed")
	}
	var result Object
	if etag == "" {
		result, err = store.objects.CreateObject(ctx, path+"/head", contents)
	} else {
		result, err = store.objects.CompareAndSwapObject(ctx, path+"/head", etag, contents)
	}
	if errors.Is(err, ErrOutcomeUnknown) {
		observed, readErr := store.objects.ReadObject(ctx, path+"/head", "")
		if readErr == nil && bytes.Equal(observed.Contents, contents) {
			return observed, nil
		}
	}
	return result, err
}

func (store *ConnectionStore) readMaterial(ctx context.Context, record connectionRecord) (CredentialMaterial, error) {
	if record.Credential == nil {
		return CredentialMaterial{}, errors.New("project credential is absent")
	}
	object, err := store.objects.ReadObject(ctx, record.Credential.Key, record.Credential.Version)
	if err != nil {
		return CredentialMaterial{}, err
	}
	var envelope credentialEnvelope
	if digestBytes(object.Contents) != record.Credential.Digest || strictJSON(object.Contents, &envelope) != nil || envelope.SchemaVersion != connectionSchema || envelope.Scope != store.scope || envelope.Key != record.Key || envelope.AuthMethod != record.AuthMethod || !sameExpiry(envelope.ExpiresAt, record.ExpiresAt) {
		return CredentialMaterial{}, errors.New("project credential identity or integrity differs")
	}
	material := CredentialMaterial{Credentials: envelope.Credentials, ExpiresAt: envelope.ExpiresAt, AuthMethod: envelope.AuthMethod}
	if err = validateMaterial(material); err != nil {
		return CredentialMaterial{}, err
	}
	return material, nil
}

func (store *ConnectionStore) createImmutable(ctx context.Context, key string, contents []byte) (objectReference, error) {
	object, err := store.objects.CreateObject(ctx, key, contents)
	if errors.Is(err, ErrConflict) || errors.Is(err, ErrOutcomeUnknown) {
		observed, readErr := store.objects.ReadObject(ctx, key, "")
		if readErr == nil && bytes.Equal(observed.Contents, contents) {
			object, err = observed, nil
		}
	}
	if err != nil {
		return objectReference{}, err
	}
	if object.Version == "" || object.Version == "null" {
		return objectReference{}, ErrOutcomeUnknown
	}
	return objectReference{Key: key, Version: object.Version, Digest: digestBytes(contents)}, nil
}

func (store *ConnectionStore) connectionPath(key ConnectionKey) (string, error) {
	if !regexpConnectorID(key.ConnectorID) || strings.TrimSpace(key.ConnectionName) != key.ConnectionName || len(key.ConnectionName) == 0 || len(key.ConnectionName) > 256 || strings.ContainsAny(key.ConnectionName, "\x00\r\n") {
		return "", errors.New("invalid project connection key")
	}
	return store.prefix + "/connections/" + key.ConnectorID + "/" + base64.RawURLEncoding.EncodeToString([]byte(key.ConnectionName)), nil
}

func regexpConnectorID(value string) bool {
	if len(value) < 2 || len(value) > 63 || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	return strings.Trim(value, "abcdefghijklmnopqrstuvwxyz0123456789-") == ""
}
func validateMaterial(material CredentialMaterial) error {
	var object map[string]json.RawMessage
	if len(material.Credentials) == 0 || len(material.Credentials) > 64<<10 || strictJSON(material.Credentials, &object) != nil || object == nil || material.AuthMethod == "" || len(material.AuthMethod) > 128 {
		return errors.New("invalid private credential material")
	}
	return nil
}

// DecodeCredentials strictly decodes one stored credential object into a connector's credential fields.
// Unknown members and trailing values are rejected. Generated connector code is its caller.
func DecodeCredentials(contents json.RawMessage, destination any) error {
	if destination == nil {
		return errors.New("credential destination is required")
	}
	if err := strictJSON(contents, destination); err != nil {
		return errors.New("stored credential does not match the connector's credential fields")
	}
	return nil
}

func strictJSON(contents []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return errors.New("invalid project object JSON")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("invalid project object JSON suffix")
	}
	return nil
}
func digestBytes(contents []byte) string {
	value := sha256.Sum256(contents)
	return hex.EncodeToString(value[:])
}
func newAttemptID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", errors.New("credential admission identity unavailable")
	}
	return hex.EncodeToString(value[:]), nil
}

func sameExpiry(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == right
	}
	return left.Equal(*right)
}

// MarshalText rejects accidental textual transport of private material.
func (CredentialMaterial) MarshalText() ([]byte, error) {
	return nil, errors.New("private credentials cannot be serialized")
}

// MarshalYAML rejects accidental YAML transport of private material.
func (CredentialMaterial) MarshalYAML() (any, error) {
	return nil, errors.New("private credentials cannot be serialized")
}
