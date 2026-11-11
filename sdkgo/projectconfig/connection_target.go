// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package projectconfig

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// ConnectionTargetFingerprintSchema identifies the canonical document that ConnectionTargetFingerprint hashes. It is
// the "schema" member of that document, so a later format produces different fingerprints instead of colliding.
const ConnectionTargetFingerprintSchema = "connectors.dex.dev/connection-target/v1"

// connectionTargetFingerprintPrefix precedes the lowercase hexadecimal SHA-256 of the canonical document.
const connectionTargetFingerprintPrefix = "sha256:"

var (
	// ErrConnectionTargetChanged reports that a credential resolution refused to return a credential because the
	// connection no longer points at the target the caller expected through ContextWithExpectedConnectionTarget:
	// the record's account, settings or authorization method changed, or the file no longer lists the record exactly
	// once. The refusal happens before any provider request, including a token refresh, so the attempt definitely
	// sent nothing.
	ErrConnectionTargetChanged = errors.New("project connection target changed; no credential was used")
	// ErrConnectionTargetUnverifiable reports that a credential resolution refused to return a credential because
	// the caller expected a connection target and the connection's source cannot read the target together with the
	// credential. Only a project loaded from a Dex Web connections file can. Nothing was sent to the provider.
	ErrConnectionTargetUnverifiable = errors.New("project connection target cannot be verified; no credential was used")
)

// connectionTargetDocument is the canonical document of ConnectionTargetFingerprint. Members are sorted by name.
type connectionTargetDocument struct {
	AuthMethodID       string   `json:"authMethodId"`
	AuthMethodIDs      []string `json:"authMethodIds"`
	AuthorizationID    string   `json:"authorizationId"`
	Configuration      any      `json:"configuration"`
	ConnectionName     string   `json:"connectionName"`
	ConnectorID        string   `json:"connectorId"`
	Provider           string   `json:"provider"`
	Schema             string   `json:"schema"`
	SharedConnectionID string   `json:"sharedConnectionId"`
}

// ConnectionTargetFingerprint identifies what the connection points at, from its non-secret fields only, so a
// caller can record the target a call was checked against and later detect that the connection was rebound. Two
// entries have the same fingerprint exactly when they share ConnectorID, ConnectionName, Provider, AuthMethodID,
// AuthMethodIDs (in order), AuthorizationID, SharedConnectionID and the JSON value of Configuration. ModulePath,
// credentials and their expiry never enter it, so a connector upgrade or a token refresh keeps the fingerprint.
//
// The format is stable, so callers may persist fingerprints. The result is "sha256:" followed by the lowercase
// hexadecimal SHA-256 of a canonical JSON object with exactly these members: authMethodId, authMethodIds,
// authorizationId, configuration, connectionName, connectorId, provider, schema and sharedConnectionId. Its
// members and every nested object's members are sorted by name, with no insignificant white space. Strings are
// encoded as Go's encoding/json encodes them with HTML escaping disabled, and numbers keep their JSON text. schema
// is ConnectionTargetFingerprintSchema, an absent AuthorizationID is the empty string, nil AuthMethodIDs is the
// empty array, and an empty Configuration is null. A future format changes schema and therefore every fingerprint.
//
// It returns an error when Configuration is not exactly one JSON value.
func (connection ConnectionConfiguration) ConnectionTargetFingerprint() (string, error) {
	var settings any
	if trimmed := bytes.TrimSpace(connection.Configuration); len(trimmed) > 0 {
		decoder := json.NewDecoder(bytes.NewReader(trimmed))
		decoder.UseNumber()
		if err := decoder.Decode(&settings); err != nil {
			return "", errors.New("connection configuration is not a JSON value")
		}
		if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
			return "", errors.New("connection configuration has data after its JSON value")
		}
	}
	authMethodIDs := connection.AuthMethodIDs
	if authMethodIDs == nil {
		authMethodIDs = []string{}
	}
	var canonical bytes.Buffer
	encoder := json.NewEncoder(&canonical)
	encoder.SetEscapeHTML(false)
	// encoding/json writes struct members in declaration order and map members sorted, which is the canonical order.
	err := encoder.Encode(connectionTargetDocument{
		AuthMethodID: connection.AuthMethodID, AuthMethodIDs: authMethodIDs, AuthorizationID: connection.AuthorizationID,
		Configuration: settings, ConnectionName: connection.ConnectionName, ConnectorID: connection.ConnectorID,
		Provider: connection.Provider, Schema: ConnectionTargetFingerprintSchema, SharedConnectionID: connection.SharedConnectionID,
	})
	if err != nil {
		return "", fmt.Errorf("encode connection target: %w", err)
	}
	sum := sha256.Sum256(bytes.TrimSuffix(canonical.Bytes(), []byte("\n")))
	return connectionTargetFingerprintPrefix + hex.EncodeToString(sum[:]), nil
}

// expectedConnectionTargetsKey is the context key of the expected connection targets.
type expectedConnectionTargetsKey struct{}

// ContextWithExpectedConnectionTarget returns a context under which every credential resolution of key refuses to
// return a credential unless the connection still has fingerprint, a ConnectionTargetFingerprint value.
// Resolutions of other connections are unaffected; a later call for the same key replaces its expectation.
//
// The context reaches CredentialResolver through a connector's normal call path, such as a Step context or
// sdkgo.ResolveCredential, so generated connector code needs no change. Under it, Resolve, ResolveWithRefresh,
// ResolveAfterRejection and ConnectionStore.ReadCredentialMaterial read the credential and the connection's
// target fields from one read of the connection record. ResolveWithRefresh compares again after it acquires the
// refresh lock, before any refresh request. When the fingerprints differ, or the file no longer lists the record
// exactly once, they return an error wrapping ErrConnectionTargetChanged without sending any provider request,
// without changing the record and without returning the credential. A refresh already sent for a matching record
// still commits its result, so a rotated refresh token is never lost.
//
// Only a project loaded from a Dex Web connections file can verify the target. Every other source, including
// hosted and S3 storage and shared entries served by the credential broker, refuses a resolution that carries an
// expectation with ErrConnectionTargetUnverifiable, because its records carry no authorization identity and its
// settings are fixed when the project is loaded. An empty or malformed fingerprint never matches.
func ContextWithExpectedConnectionTarget(ctx context.Context, key ConnectionKey, fingerprint string) context.Context {
	previous, _ := ctx.Value(expectedConnectionTargetsKey{}).(map[ConnectionKey]string)
	expected := make(map[ConnectionKey]string, len(previous)+1)
	for previousKey, previousFingerprint := range previous {
		expected[previousKey] = previousFingerprint
	}
	expected[key] = fingerprint
	return context.WithValue(ctx, expectedConnectionTargetsKey{}, expected)
}

// expectedConnectionTarget returns the fingerprint that ctx requires for key, if any.
func expectedConnectionTarget(ctx context.Context, key ConnectionKey) (fingerprint string, isExpected bool) {
	if ctx == nil {
		return "", false
	}
	expected, _ := ctx.Value(expectedConnectionTargetsKey{}).(map[ConnectionKey]string)
	fingerprint, isExpected = expected[key]
	return fingerprint, isExpected
}

// connectionTargetSnapshotReader is implemented by a store whose connection records carry their target fields.
type connectionTargetSnapshotReader interface {
	// readConnectionHeadWithTarget returns the connection head and the target fingerprint from one read of the
	// record, or an error wrapping errConnectionRemovedFromFile or errDuplicateConnectionInFile.
	readConnectionHeadWithTarget(ctx context.Context, headKey string, key ConnectionKey) (head Object, fingerprint string, err error)
}

// guardedRecordError reports a removed or duplicated record as a changed target to a caller that expects one,
// because no single expected target remains.
func guardedRecordError(err error, isExpected bool) error {
	if isExpected && (errors.Is(err, errConnectionRemovedFromFile) || errors.Is(err, errDuplicateConnectionInFile)) {
		return fmt.Errorf("%w: %w", ErrConnectionTargetChanged, err)
	}
	return err
}
