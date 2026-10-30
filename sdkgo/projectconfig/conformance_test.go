// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package projectconfig

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo/internal/conformancefixture"
)

func TestConformanceProjectProtocol(t *testing.T) {
	observedAt := time.Date(2026, 10, 29, 12, 30, 45, 123000000, time.UTC)
	conformancefixture.Check(t, "connections.json", dexWebConnectionsFile{
		SchemaVersion: dexWebConnectionsSchema,
		Connections: []dexWebConnection{
			{ConnectorID: "sample", AuthMethodID: "apiKey", AuthMethodIDs: []string{"apiKey"}, ModulePath: "github.com/superdurable/dex-connectors-library/connectors/sample", Provider: "sample", ConnectionName: "api", Configuration: json.RawMessage(`{"region":"west"}`), Credentials: json.RawMessage(`{"apiKey":"REDACTED_API_KEY"}`)},
			{ConnectorID: "sample", AuthMethodID: "oauth2", AuthMethodIDs: []string{"oauth2", "apiKey"}, ModulePath: "github.com/superdurable/dex-connectors-library/connectors/sample", Provider: "sample", ConnectionName: "oauth", Configuration: json.RawMessage(`{"workspace":"example"}`), Credentials: json.RawMessage(`{"accessToken":"REDACTED_ACCESS_TOKEN","refreshToken":"REDACTED_REFRESH_TOKEN"}`), CredentialExpiresAt: &observedAt},
			{ConnectorID: "sample", AuthMethodID: "multipleSecrets", AuthMethodIDs: []string{"multipleSecrets"}, ModulePath: "github.com/superdurable/dex-connectors-library/connectors/sample", Provider: "sample", ConnectionName: "multi", Configuration: json.RawMessage(`{}`), Credentials: json.RawMessage(`{"clientId":"REDACTED_CLIENT_ID","clientSecret":"REDACTED_CLIENT_SECRET","signingSecret":"REDACTED_SIGNING_SECRET"}`)},
		},
		TriggerBindings: []TriggerConfiguration{},
	})
	scope := Scope{ProjectID: "sample-project", Kind: scopeKindPreview}
	prefix, err := scope.Prefix()
	if err != nil {
		t.Fatal(err)
	}
	connectionKey := ConnectionKey{ConnectorID: "sample", ConnectionName: "main / west"}
	store := &ConnectionStore{prefix: prefix}
	connectionPath, err := store.connectionPath(connectionKey)
	if err != nil {
		t.Fatal(err)
	}
	inbox, err := NewTriggerInbox(&versionedStore{}, scope, TriggerInboxKey{ConnectorID: "sample", ConnectionName: connectionKey.ConnectionName, TriggerName: "itemCreated", BindingName: "main / west"})
	if err != nil {
		t.Fatal(err)
	}
	secretKey := prefix + "/app-secrets/WEBHOOK_SECRET/0123456789abcdef0123456789abcdef"
	snapshot := SnapshotRef{Key: prefix + "/configuration/head", Version: "version-2", Digest: "sha256:" + strings.Repeat("a", 64), MediaType: "application/json"}
	object := Object{Key: snapshot.Key, Version: snapshot.Version, ETag: `"etag-2"`}
	conformancefixture.Check(t, "project-storage.json", map[string]any{
		"scope":               scope,
		"snapshot":            snapshot,
		"connectionKey":       connectionKey,
		"connectionHeadKey":   connectionPath + "/head",
		"credentialObjectKey": connectionPath + "/credentials/" + strings.Repeat("b", 64),
		"exchangeResultKey":   connectionPath + "/exchange-results/attempt-7",
		"triggerInboxHeadKey": inbox.key,
		"applicationSecret":   ApplicationSecretRef{Key: secretKey, Version: "version-1", Digest: "sha256:" + strings.Repeat("c", 64)},
		"objectCAS":           map[string]string{"key": object.Key, "version": object.Version, "etag": object.ETag},
		"objectReference":     objectReference{Key: connectionPath + "/credentials/" + strings.Repeat("b", 64), Version: "version-3", Digest: strings.Repeat("b", 64)},
		"mutation":            credentialMutation{Kind: string(CredentialRefreshing), InvocationID: "invocation-7", ID: "attempt-7", Fence: 2, BaseRevision: 1, Deadline: observedAt, ResultKey: connectionPath + "/exchange-results/attempt-7"},
	})
	conformancefixture.Check(t, "project-environment.json", map[string]any{
		"localDexWeb": map[string]string{LocalConnectionsFileEnvironment: "./connections.json"},
		"hosted": map[string]string{
			"DEX_PROJECT_ID": scope.ProjectID, "DEX_PROJECT_SCOPE": scope.Kind,
			"DEX_PROJECT_CONFIG_KEY": snapshot.Key, "DEX_PROJECT_CONFIG_VERSION": snapshot.Version,
			"DEX_PROJECT_CONFIG_DIGEST": snapshot.Digest, "DEX_PROJECT_STORAGE_BUCKET": "example-bucket",
			"DEX_PROJECT_STORAGE_PREFIX":      "fixtures/",
			"DEX_PROJECT_STORAGE_KMS_KEY_ARN": "arn:aws:kms:us-west-2:123456789012:key/example-key",
		},
		"localObjectStoreOverrides": map[string]string{
			"DEX_PROJECT_STORAGE_ENDPOINT": "http://127.0.0.1:9000", "DEX_PROJECT_ALLOW_LOCAL_STORAGE": "true",
		},
	})
	conformancefixture.Check(t, "credential-broker.json", map[string]any{
		"firstRequest":            credentialBrokerRequest{SharedConnectionID: "shared-7"},
		"refreshRequest":          credentialBrokerRequest{SharedConnectionID: "shared-7", RejectedRevision: 3},
		"ready":                   credentialBrokerResponse{Status: string(credentialBrokerReady), Credential: json.RawMessage(`{"accessToken":"REDACTED_ACCESS_TOKEN","refreshToken":"REDACTED_REFRESH_TOKEN"}`), Revision: 4, IssuedAt: &observedAt, ExpiresAt: &observedAt},
		"reauthorizationRequired": credentialBrokerResponse{Status: string(credentialBrokerReauthorizationRequired)},
		"connectionNotSelected":   credentialBrokerResponse{Status: string(credentialBrokerConnectionNotSelected)},
	})
}
