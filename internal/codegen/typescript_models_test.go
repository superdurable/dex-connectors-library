// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package codegen

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/superdurable/dex-connectors-library/internal/codegen/gomodel"
	"github.com/superdurable/dex-connectors-library/schema"
)

func TestTypeScriptRefreshDeclarationUsesDecodedManifest(t *testing.T) {
	manifestFile, err := os.Open(filepath.Join("..", "..", "connectors", "google", "gmail", "connector.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := manifestFile.Close(); err != nil {
			t.Fatal(err)
		}
	})
	manifest, err := schema.Decode(manifestFile)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Spec.Auth.Methods[0].OAuth2.Refresh.AdditionalParameters = map[string]string{
		"description": "retained", "protocol": "oauth2",
	}
	manifest.Spec.Auth.Methods[0].OAuth2.TokenEndpoint = schema.ValueSource{
		FromCredential: "oauth_client_id", Values: map[string]string{"description": "https://example.com/token"},
	}
	declaration, err := generateTypeScriptRefreshDeclaration(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"defaultMethod": "google-oauth"`, `"requiredScopes"`,
		`"workspace-domain-delegation"`, `"oauth_client_id": "oauthClientId"`,
		`"description": "retained"`, `"protocol": "oauth2"`, `"description": "https://example.com/token"`} {
		if !strings.Contains(string(declaration), expected) {
			t.Errorf("generated refresh declaration omits %s", expected)
		}
	}
	connection := string(generateTypeScriptConnection(manifest, nil))
	if !strings.Contains(connection, "newRefreshingCredentialProvider(project.connections, { connectorId: ConnectorID, connectionName }, decode, encode, decodeShared)") {
		t.Fatal("generated project credentials omit shared credential decoding")
	}
}

func TestTypeScriptMultiAuthModelsGolden(t *testing.T) {
	manifestFile, err := os.Open(filepath.Join("..", "..", "schema", "testdata", "multi-auth.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	defer manifestFile.Close()
	manifest, err := schema.Decode(manifestFile)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Spec.Configuration.Fields = []schema.Field{
		{Name: "region", GoName: "Region", Type: "string", Description: "Optional region.", Required: false},
		{Name: "accountId", GoName: "AccountID", Type: "string", Description: "Required account ID.", Required: true},
	}
	manifest.Spec.Auth.Methods[0].Configuration = &schema.Configuration{Fields: []schema.Field{{Name: "workspaceId", GoName: "WorkspaceID", Type: "string", Description: "Workspace for the selected method.", Required: true}}}
	manifest.Spec.Auth.Methods[1].Fields = append(manifest.Spec.Auth.Methods[1].Fields, schema.Field{Name: "session_note", GoName: "SessionNote", Type: "string", Description: "Optional session note.", Required: false})
	manifest.Spec.Auth.Methods[1].Fields = append(manifest.Spec.Auth.Methods[1].Fields, schema.Field{Name: "environment", GoName: "Environment", Type: "enum", Enum: []string{"production", "sandbox"}, Description: "Login environment.", Required: false}, schema.Field{Name: "login_url", GoName: "LoginURL", Type: "url", Description: "Login URL.", Required: false})
	manifest.Spec.Auth.Methods = append(manifest.Spec.Auth.Methods, schema.AuthMethod{ID: "typed", Fields: []schema.Field{
		{Name: "request_count", GoName: "RequestCount", Type: "integer", Required: true},
		{Name: "wait_duration", GoName: "WaitDuration", Type: "duration", Required: true},
		{Name: "labels", GoName: "Labels", Type: "stringList", Required: false},
		{Name: "headers", GoName: "Headers", Type: "stringMap", Required: false},
		{Name: "optional_count", GoName: "OptionalCount", Type: "integer", Required: false},
	}})
	grantFile, err := os.Open(filepath.Join("..", "..", "schema", "testdata", "token-grants.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	defer grantFile.Close()
	grantManifest, err := schema.Decode(grantFile)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Spec.Auth.Methods = append(manifest.Spec.Auth.Methods, grantManifest.Spec.Auth.Methods[2])
	got := append(bytes.TrimRight(generateTypeScriptModels(manifest, nil), "\n"), '\n')
	goldenPath := filepath.Join("testdata", "multi-auth-models.ts")
	if os.Getenv("UPDATE_MULTI_AUTH_GOLDEN") == "1" {
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatal("multi-auth TypeScript models differ from golden output")
	}
	if os.Getenv("TS_CODEGEN_COMPILE") != "1" {
		return
	}
	stripeDirectory, err := filepath.Abs(filepath.Join("..", "..", "connectors", "stripe", "typescript"))
	if err != nil {
		t.Fatal(err)
	}
	fixtureDirectory := t.TempDir()
	if err := os.Symlink(filepath.Join(stripeDirectory, "node_modules"), filepath.Join(fixtureDirectory, "node_modules")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixtureDirectory, "package.json"), []byte(`{"type":"module"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixtureDirectory, "models.ts"), got, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixtureDirectory, "test.ts"), []byte(multiAuthTypeScriptRuntimeTest), 0o644); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(filepath.Join(stripeDirectory, "node_modules", ".bin", "tsx"), "test.ts")
	command.Dir = fixtureDirectory
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated multi-auth TypeScript credential behavior: %v\n%s", err, output)
	}
}

func TestTypeScriptSingleAuthModelsGolden(t *testing.T) {
	manifestFile, err := os.Open(filepath.Join("..", "..", "schema", "testdata", "single-auth-credentials.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	defer manifestFile.Close()
	manifest, err := schema.Decode(manifestFile)
	if err != nil {
		t.Fatal(err)
	}
	got := append(bytes.TrimRight(generateTypeScriptModels(manifest, nil), "\n"), '\n')
	goldenPath := filepath.Join("testdata", "single-auth-models.ts")
	if os.Getenv("UPDATE_SINGLE_AUTH_GOLDEN") == "1" {
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("single-auth TypeScript models differ from golden output")
	}
	if os.Getenv("TS_CODEGEN_COMPILE") != "1" {
		return
	}
	stripeDirectory, err := filepath.Abs(filepath.Join("..", "..", "connectors", "stripe", "typescript"))
	if err != nil {
		t.Fatal(err)
	}
	fixtureDirectory := t.TempDir()
	if err := os.Symlink(filepath.Join(stripeDirectory, "node_modules"), filepath.Join(fixtureDirectory, "node_modules")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixtureDirectory, "package.json"), []byte(`{"type":"module"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixtureDirectory, "models.ts"), got, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixtureDirectory, "test.ts"), []byte(singleAuthTypeScriptRuntimeTest), 0o644); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(filepath.Join(stripeDirectory, "node_modules", ".bin", "tsx"), "test.ts")
	command.Dir = fixtureDirectory
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated single-auth TypeScript credential behavior: %v\n%s", err, output)
	}
}

const singleAuthTypeScriptRuntimeTest = `
import assert from 'node:assert/strict';
import { decodeCredentials, decodeSharedCredentials } from './models.js';
const complete = decodeCredentials({ oauth_client_id: 'id', oauth_client_secret: 'secret', access_token: 'access', refresh_token: 'refresh', environment: 'sandbox', login_url: 'https://example.com/login' });
assert.equal(complete.oauthClientId, 'id');
assert.equal(complete.oauthClientSecret.reveal(), 'secret');
assert.equal(complete.accessToken.reveal(), 'access');
assert.equal(complete.refreshToken.reveal(), 'refresh');
assert.equal(complete.environment, 'sandbox');
assert.equal(complete.loginUrl, 'https://example.com/login');
const shared = decodeSharedCredentials({ access_token: 'access' });
assert.equal(shared.oauthClientId, '');
assert.equal(shared.oauthClientSecret.reveal(), '');
assert.equal(shared.refreshToken.reveal(), '');
assert.equal(decodeSharedCredentials({ oauth_client_id: 'id', oauth_client_secret: 'secret', access_token: 'access' }).oauthClientSecret.reveal(), 'secret');
const absentOptional = decodeCredentials({ access_token: 'access' });
assert.equal(absentOptional.oauthClientId, '');
assert.equal(absentOptional.oauthClientSecret.reveal(), '');
assert.equal(absentOptional.refreshToken.reveal(), '');
assert.equal(absentOptional.environment, '');
assert.equal(absentOptional.loginUrl, '');
assert.equal(decodeCredentials({ access_token: 'access', refresh_token: null }).refreshToken.reveal(), '');
assert.throws(() => decodeCredentials({ access_token: 'access', unknown: 'value' }), /stored credential does not match/);
assert.throws(() => decodeCredentials({ access_token: 3 }), /credential access_token has an invalid type/);
assert.throws(() => decodeSharedCredentials({ access_token: 'access', unknown: 'value' }), /stored credential does not match/);
assert.throws(() => decodeSharedCredentials({ access_token: 'access', oauth_client_secret: 3 }), /credential oauth_client_secret has an invalid type/);
assert.throws(() => decodeCredentials({ auth_method: '' }), /stored credential does not match/);
assert.throws(() => decodeCredentials({ access_token: 'access', environment: 'staging' }), /credential environment is invalid/);
assert.throws(() => decodeCredentials({ access_token: 'access', login_url: 'relative' }), /credential login_url must be an absolute URL/);
assert.throws(() => decodeCredentials({}), /credential access_token is required/);
assert.throws(() => decodeSharedCredentials({}), /credential access_token is required/);
`

const multiAuthTypeScriptRuntimeTest = `
import assert from 'node:assert/strict';
import { ConfigSchema, decodeCredentials, decodeSharedCredentials } from './models.js';
assert.deepEqual(ConfigSchema.parse({ accountId: 'account' }), { accountId: 'account' });
assert.equal(ConfigSchema.parse({ accountId: 'account', region: 'west', workspaceId: 'workspace' }).region, 'west');
assert.throws(() => ConfigSchema.parse({}));
const oauth = decodeCredentials({ auth_method: null, oauth_client_id: 'id', oauth_client_secret: 'secret', access_token: 'access', refresh_token: 'refresh' });
assert.equal(oauth.authMethodId, 'google-oauth');
assert.equal(oauth.serviceAccountKey, undefined);
assert.equal(decodeSharedCredentials({ oauth_client_id: 'id', access_token: 'access' }).accessToken?.reveal(), 'access');
assert.equal(decodeSharedCredentials({ auth_method: 'app-only', tenant_id: 'tenant', client_id: 'id', access_token: 'access' }).accessToken?.reveal(), 'access');
const service = decodeCredentials({ auth_method: 'workspace-service-account', service_account_key: 'key', delegated_user: 'user', session_note: 'note', environment: 'sandbox', login_url: 'https://example.com/login', oauth_client_id: 'other' });
assert.equal(service.authMethodId, 'workspace-service-account');
assert.equal(service.serviceAccountKey?.reveal(), 'key');
assert.equal(service.sessionNote, 'note');
assert.equal(service.oauthClientId, undefined);
for (const invalidURL of [' https://example.com', 'https://example.com\n', 'https://exa mple.com', 'https://%zz', 'https://example.com/%zz', 'https://u:p@[secretish]/']) {
  assert.throws(() => decodeCredentials({ auth_method: 'workspace-service-account', service_account_key: 'key', delegated_user: 'user', login_url: invalidURL }), /credential login_url must be an absolute URL/);
}
assert.equal(decodeCredentials({ auth_method: 'workspace-service-account', service_account_key: 'key', delegated_user: 'user', login_url: 'https://example.com:99999' }).loginUrl, 'https://example.com:99999');
const typed = decodeCredentials({ auth_method: 'typed', request_count: 1, wait_duration: 2, labels: [null, 'x'], headers: { empty: null, filled: 'x' }, optional_count: 0 });
assert.deepEqual(typed.labels, ['', 'x']);
assert.deepEqual(typed.headers, { empty: '', filled: 'x' });
assert.equal(typed.optionalCount, undefined);
assert.throws(() => decodeCredentials({ auth_method: 'typed', request_count: 1, wait_duration: 2, labels: [1] }), /credential labels has an invalid type/);
assert.throws(() => decodeCredentials({ auth_method: 'typed', request_count: 1, wait_duration: 2, headers: { bad: 1 } }), /credential headers has an invalid type/);
for (const field of ['request_count', 'wait_duration']) {
  for (const invalidValue of [0, -1]) {
    assert.throws(() => decodeCredentials({ auth_method: 'typed', request_count: 1, wait_duration: 2, [field]: invalidValue }), new RegExp('credential ' + field + ' must be positive'));
  }
}
assert.throws(() => decodeCredentials({ auth_method: 'typed', request_count: 1, wait_duration: 2, optional_count: -1 }), /credential optional_count cannot be negative/);
assert.equal(decodeCredentials({ auth_method: 'workspace-service-account', service_account_key: 'key', delegated_user: 'user', session_note: null }).sessionNote, undefined);
assert.throws(() => decodeCredentials({ auth_method: 'workspace-service-account', service_account_key: null, delegated_user: 'user' }), /credential service_account_key is required/);
assert.throws(() => decodeCredentials({ auth_method: 'workspace-service-account', delegated_user: 'user' }));
assert.throws(() => decodeCredentials({ auth_method: 'unknown' }));
assert.throws(() => decodeCredentials({ auth_method: 0 }));
assert.throws(() => decodeCredentials({ auth_method: 'google-oauth', oauth_client_id: 'id', oauth_client_secret: 'secret', access_token: 'access', refresh_token: 'refresh', service_account_key: 17 }), /credential service_account_key has an invalid type/);
assert.throws(() => decodeCredentials({ auth_method: 'google-oauth', oauth_client_id: 'id', oauth_client_secret: 'secret', access_token: 'access', refresh_token: 'refresh', unknown: 'value' }), /stored credential does not match/);
assert.throws(() => decodeCredentials({ auth_method: 'workspace-service-account', service_account_key: 'key', delegated_user: 'user', environment: 'staging' }), /credential environment is invalid/);
assert.throws(() => decodeCredentials({ auth_method: 'workspace-service-account', service_account_key: 'key', delegated_user: 'user', login_url: 'relative' }), /credential login_url must be an absolute URL/);
assert.throws(() => decodeSharedCredentials({ oauth_client_id: 'id' }), /credential access_token is required/);
assert.throws(() => decodeSharedCredentials({ access_token: 'access' }), /credential oauth_client_id is required/);
assert.throws(() => decodeSharedCredentials({ auth_method: 'app-only', tenant_id: 'tenant', client_id: 'id' }), /credential access_token is required/);
`

func TestSharedRequiredCredentialFieldsHasEmptyJSONArray(t *testing.T) {
	fields := SharedRequiredCredentialFields(schema.AuthMethod{Fields: []schema.Field{{Name: "optional", Type: "string"}}})
	encoded, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != "[]" {
		t.Fatalf("empty shared-required fields encode as %s, want []", encoded)
	}
}

func TestTypeScriptModelsKeepRequiredFieldsAndLimitLazyToCycles(t *testing.T) {
	models := []gomodel.Model{
		{Name: "Plain", Fields: []gomodel.Field{{Name: "id", Type: gomodel.Type{Kind: gomodel.String}}, {Name: "status", Type: gomodel.Type{Kind: gomodel.Enum, EnumBase: gomodel.String, EnumValues: []string{"open", "closed"}}}}},
		{Name: "A", Fields: []gomodel.Field{{Name: "b", Type: gomodel.Type{Kind: gomodel.Ref, Ref: "B", Nullable: true}}}},
		{Name: "B", Fields: []gomodel.Field{{Name: "a", Type: gomodel.Type{Kind: gomodel.Ref, Ref: "A", Nullable: true}}}},
	}
	generated := string(generateTypeScriptModels(schema.Manifest{}, models))
	if !strings.Contains(generated, "export const PlainSchema = z.strictObject({") || strings.Contains(generated, ".prefault(") {
		t.Fatalf("plain schema lost its required ZodObject shape:\n%s", generated)
	}
	if !strings.Contains(generated, "\"status\": z.string(),") || !strings.Contains(generated, `"open" | "closed" | (string & {})`) {
		t.Fatalf("named string enum is not open:\n%s", generated)
	}
	if strings.Count(generated, "= z.lazy(") != 2 {
		t.Fatalf("only the mutually recursive models should be lazy:\n%s", generated)
	}
	if got := emptyTypeScriptValue(gomodel.Type{Kind: gomodel.Enum, EnumBase: gomodel.String, EnumValues: []string{"open"}}); got != "''" {
		t.Fatalf("enum zero value fabricated a known status: %s", got)
	}
}

func TestTypeScriptManifestDescriptionsAreSafeJSDoc(t *testing.T) {
	manifest := schema.Manifest{Spec: schema.Spec{
		Configuration: schema.Configuration{Fields: []schema.Field{{Name: "timeout", Type: "duration", Description: "Go duration */ unsafe"}, {Name: "label", Type: "string", Description: "Label */ unsafe"}}},
		Auth:          schema.Auth{Fields: []schema.Field{{Name: "token", GoName: "Token", Type: "string", Description: "Token */ unsafe", Required: true}}},
	}}
	generated := string(generateTypeScriptModels(manifest, nil))
	if strings.Contains(generated, "*/ unsafe") || !strings.Contains(generated, "Go duration * / unsafe Runtime value in milliseconds.") || !strings.Contains(generated, "Label * / unsafe") || !strings.Contains(generated, "Token * / unsafe") {
		t.Fatalf("unsafe or misleading generated JSDoc:\n%s", generated)
	}
	steps := string(generateTypeScriptSteps([]schema.Operation{{Name: "read", GoName: "Read", InputType: "Input", OutputType: "Output", Branches: []schema.OperationBranch{{ID: "done", Description: "Done */ unsafe"}}}}))
	if strings.Contains(steps, "*/ unsafe") || !strings.Contains(steps, "Done * / unsafe") {
		t.Fatalf("branch JSDoc was not escaped:\n%s", steps)
	}
}

func TestTypeScriptJSDocBlankLinesHaveNoTrailingWhitespace(t *testing.T) {
	comment := typeScriptJSDoc("First line\n\nSecond line")
	if comment != "First line\n   *\n   * Second line" {
		t.Fatalf("blank JSDoc line has unexpected formatting: %q", comment)
	}
}
