// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package projectconfig

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo/internal/conformancefixture"
)

func TestConformanceProjectProtocol(t *testing.T) {
	ctx := context.Background()
	observedAt := time.Date(2026, 10, 29, 12, 30, 45, 123456789, time.FixedZone("UTC+8", 8*60*60))
	conformancefixture.Check(t, "connections.json", dexWebConnectionsFile{
		SchemaVersion: dexWebConnectionsSchema,
		Connections: []dexWebConnection{
			{ConnectorID: "sample", AuthMethodID: "apiKey", AuthMethodIDs: []string{"apiKey"}, ModulePath: "github.com/superdurable/dex-connectors-library/connectors/sample", Provider: "sample", ConnectionName: "api", Configuration: json.RawMessage(`{"region":"west"}`), Credentials: json.RawMessage(`{"apiKey":"REDACTED_API_KEY"}`)},
			{ConnectorID: "sample", AuthMethodID: "oauth2", AuthMethodIDs: []string{"oauth2", "apiKey"}, ModulePath: "github.com/superdurable/dex-connectors-library/connectors/sample", Provider: "sample", ConnectionName: "oauth", Configuration: json.RawMessage(`{"workspace":"example"}`), Credentials: json.RawMessage(`{"accessToken":"REDACTED_ACCESS_TOKEN","refreshToken":"REDACTED_REFRESH_TOKEN"}`), CredentialExpiresAt: &observedAt},
			{ConnectorID: "sample", AuthMethodID: "multipleSecrets", AuthMethodIDs: []string{"multipleSecrets"}, ModulePath: "github.com/superdurable/dex-connectors-library/connectors/sample", Provider: "sample", ConnectionName: "multi", Configuration: json.RawMessage(`{}`), Credentials: json.RawMessage(`{"clientId":"REDACTED_CLIENT_ID","clientSecret":"REDACTED_CLIENT_SECRET","signingSecret":"REDACTED_SIGNING_SECRET"}`)},
			{ConnectorID: "sample", AuthMethodID: "apiKey", AuthMethodIDs: []string{}, ModulePath: "github.com/superdurable/dex-connectors-library/connectors/sample", Provider: "sample", ConnectionName: "élément <&> 😀", Configuration: json.RawMessage(`{"label":"ligne\u2028nouvelle"}`), Credentials: json.RawMessage(`{"apiKey":"REDACTED_UNICODE_KEY"}`)},
			{ConnectorID: "sample", AuthMethodID: "apiKey", ModulePath: "github.com/superdurable/dex-connectors-library/connectors/sample", Provider: "sample", ConnectionName: "null-methods", Configuration: json.RawMessage(`{}`), Credentials: json.RawMessage(`{"apiKey":"REDACTED_NULL_METHODS_KEY"}`)},
		},
		TriggerBindings: []TriggerConfiguration{},
	})

	scope := Scope{ProjectID: "sample-project", Kind: scopeKindPreview}
	prefix, err := scope.Prefix()
	if err != nil {
		t.Fatal(err)
	}
	objects, err := newVersionedStore("")
	if err != nil {
		t.Fatal(err)
	}
	connectionStore, err := NewConnectionStore(&ConnectionStoreConfig{Objects: objects, Scope: scope})
	if err != nil {
		t.Fatal(err)
	}
	configurationStore, err := NewConfigurationStore(&ConfigurationStoreConfig{Objects: objects, Scope: scope})
	if err != nil {
		t.Fatal(err)
	}
	connectionKey := ConnectionKey{ConnectorID: "sample", ConnectionName: "main-é-流程 😀"}
	connectionPath, err := connectionStore.connectionPath(connectionKey)
	if err != nil {
		t.Fatal(err)
	}
	material := CredentialMaterial{Credentials: json.RawMessage(`{"accessToken":"REDACTED_ACCESS_TOKEN"}`), ExpiresAt: &observedAt, AuthMethod: "oauth2"}
	if _, err := connectionStore.ReplaceCredential(ctx, connectionKey, 0, material); err != nil {
		t.Fatal(err)
	}
	record, recordObject, err := connectionStore.readRecord(ctx, connectionKey)
	if err != nil {
		t.Fatal(err)
	}
	credentialObject, err := objects.ReadObject(ctx, record.Credential.Key, record.Credential.Version)
	if err != nil {
		t.Fatal(err)
	}
	mutation := credentialMutation{Kind: string(CredentialRefreshing), InvocationID: "invocation-7", ID: "attempt-7", Fence: record.Fence + 1, BaseRevision: record.Revision, Deadline: observedAt, ResultKey: exchangeResultPath(connectionPath, "attempt-7")}
	record.Revision++
	record.Fence++
	record.Status = CredentialRefreshing
	record.Mutation = &mutation
	recordObject, err = connectionStore.writeRecord(ctx, record, recordObject.ETag)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := connectionStore.CommitCredentialExchange(ctx, ExchangeAdmission{Key: connectionKey, AttemptID: mutation.ID, Fence: mutation.Fence, Deadline: mutation.Deadline}, material); err != nil {
		t.Fatal(err)
	}
	exchangeResultObject, err := objects.ReadObject(ctx, mutation.ResultKey, "")
	if err != nil {
		t.Fatal(err)
	}
	completedRecordObject, err := objects.ReadObject(ctx, connectionHeadPath(connectionPath), "")
	if err != nil {
		t.Fatal(err)
	}
	inbox, err := NewTriggerInbox(objects, scope, TriggerInboxKey{ConnectorID: "sample", ConnectionName: connectionKey.ConnectionName, TriggerName: "itemCreated", BindingName: "main / west"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := inbox.Pending(ctx); err != nil {
		t.Fatal(err)
	}
	emptyInboxObject, err := objects.ReadObject(ctx, inbox.key, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := inbox.Add(ctx, PendingTriggerEvent{ID: "event-é-1", Event: json.RawMessage(`{"label":"<ready>&"}`)}); err != nil {
		t.Fatal(err)
	}
	inboxObject, err := objects.ReadObject(ctx, inbox.key, "")
	if err != nil {
		t.Fatal(err)
	}
	secretRef := ApplicationSecretRef{Key: applicationSecretPath(prefix, credentialBrokerTokenName, "0123456789abcdef0123456789abcdef"), Version: "version-1", Digest: "sha256:" + strings.Repeat("c", 64)}
	applicationRef := ApplicationSecretRef{Key: applicationSecretPath(prefix, "APP_SECRET", "fedcba9876543210fedcba9876543210"), Version: "version-1", Digest: "sha256:" + strings.Repeat("d", 64)}
	ordinaryLabel := "élément <&>"
	configuration := Configuration{
		Connections:             []ConnectionConfiguration{{ConnectorID: "sample", ConnectionName: connectionKey.ConnectionName, ModulePath: "github.com/superdurable/dex-connectors-library/connectors/sample", Provider: "sample", AuthMethodID: "oauth2", SharedConnectionID: "shared-7", Configuration: json.RawMessage(`{"region":"west"}`)}},
		TriggerBindings:         []TriggerConfiguration{{ConnectorID: "sample", ConnectionName: connectionKey.ConnectionName, TriggerName: "itemCreated", BindingName: "main", Configuration: json.RawMessage(`{"folder":"inbox"}`)}},
		OperationConfigurations: []OperationConfiguration{{ConnectorID: "sample", ConnectionName: connectionKey.ConnectionName, OperationID: "createItem", FlowType: "sampleFlow", StepType: "createItem", Configuration: json.RawMessage(`{"limit":3}`)}},
		Environment:             map[string]EnvironmentValue{"ORDINARY_LABEL": {Value: &ordinaryLabel}, "APP_SECRET": {SecretRef: &applicationRef}},
		CredentialBroker:        &CredentialBrokerConfiguration{Endpoint: "https://broker.example.test/internal/connection-credentials/read", TokenSecretRef: secretRef},
	}
	_, configurationObject, err := configurationStore.WriteConfiguration(ctx, 0, configuration)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := configurationStore.FreezeConfiguration(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	conformancefixture.Check(t, "project-storage.json", map[string]any{
		"scope": scope, "snapshot": snapshot, "connectionKey": connectionKey,
		"connectionHeadKey":                       connectionHeadPath(connectionPath),
		"credentialObjectKey":                     credentialObject.Key,
		"exchangeResultKey":                       exchangeResultPath(connectionPath, "attempt-7"),
		"triggerInboxHeadKey":                     inbox.key,
		"applicationSecret":                       secretRef,
		"objectCAS":                               map[string]string{"key": recordObject.Key, "version": recordObject.Version, "etag": recordObject.ETag},
		"objectReference":                         *record.Credential,
		"mutation":                                mutation,
		"connectionRecord":                        json.RawMessage(recordObject.Contents),
		"connectionRecordCanonicalJSON":           string(recordObject.Contents),
		"credentialEnvelope":                      json.RawMessage(credentialObject.Contents),
		"credentialEnvelopeCanonicalJSON":         string(credentialObject.Contents),
		"credentialExchangeEnvelope":              json.RawMessage(exchangeResultObject.Contents),
		"credentialExchangeEnvelopeCanonicalJSON": string(exchangeResultObject.Contents),
		"completedConnectionRecord":               json.RawMessage(completedRecordObject.Contents),
		"emptyTriggerInboxDocument":               json.RawMessage(emptyInboxObject.Contents),
		"triggerInboxDocument":                    json.RawMessage(inboxObject.Contents),
		"configurationSnapshotDocument":           json.RawMessage(configurationObject.Contents),
		"configurationSnapshotCanonicalJSON":      string(configurationObject.Contents),
	})
	conformancefixture.Check(t, "project-environment.json", map[string]any{
		"localDexWeb": map[string]string{LocalConnectionsFileEnvironment: "./connections.json"},
		"hosted": map[string]string{
			projectIDEnvironment: scope.ProjectID, projectScopeEnvironment: scope.Kind,
			projectConfigKeyEnvironment: snapshot.Key, projectConfigVersionEnvironment: snapshot.Version,
			projectConfigDigestEnvironment: snapshot.Digest, projectStorageBucketEnvironment: "example-bucket",
			projectStoragePrefixEnvironment:    "fixtures/",
			projectStorageKMSKeyARNEnvironment: "arn:aws:kms:us-west-2:123456789012:key/example-key",
		},
		"localObjectStoreOverrides": map[string]string{
			projectStorageEndpointEnvironment: "http://127.0.0.1:9000", projectAllowLocalStorageEnvironment: "true",
		},
		"credentialBrokerSecretName": credentialBrokerTokenName,
	})
	conformancefixture.Check(t, "credential-broker.json", map[string]any{
		"firstRequest":            credentialBrokerRequest{SharedConnectionID: "shared-7"},
		"refreshRequest":          credentialBrokerRequest{SharedConnectionID: "shared-7", RejectedRevision: 3},
		"ready":                   credentialBrokerResponse{Status: string(credentialBrokerReady), Credential: json.RawMessage(`{"accessToken":"REDACTED_ACCESS_TOKEN","refreshToken":"REDACTED_REFRESH_TOKEN"}`), Revision: 4, IssuedAt: &observedAt, ExpiresAt: &observedAt},
		"reauthorizationRequired": credentialBrokerResponse{Status: string(credentialBrokerReauthorizationRequired)},
		"connectionNotSelected":   credentialBrokerResponse{Status: string(credentialBrokerConnectionNotSelected)},
	})
}
