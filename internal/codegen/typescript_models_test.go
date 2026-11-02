// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package codegen

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/superdurable/dex-connectors-library/internal/codegen/gomodel"
	"github.com/superdurable/dex-connectors-library/schema"
)

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
	manifest.Spec.Auth.Methods[0].Configuration = &schema.Configuration{Fields: []schema.Field{{Name: "workspaceId", GoName: "WorkspaceID", Type: "string", Description: "Workspace for the selected method.", Required: true}}}
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

const multiAuthTypeScriptRuntimeTest = `
import assert from 'node:assert/strict';
import { ConfigSchema, decodeCredentials } from './models.js';
assert.deepEqual(ConfigSchema.parse({}), {});
assert.equal(ConfigSchema.parse({ workspaceId: 'workspace' }).workspaceId, 'workspace');
const oauth = decodeCredentials({ oauth_client_id: 'id', oauth_client_secret: 'secret', access_token: 'access', refresh_token: 'refresh', service_account_key: 17 });
assert.equal(oauth.authMethodId, 'google-oauth');
assert.equal(oauth.serviceAccountKey, undefined);
const service = decodeCredentials({ service_account_key: 'key', delegated_user: 'user', oauth_client_id: 17 }, 'workspace-service-account');
assert.equal(service.authMethodId, 'workspace-service-account');
assert.equal(service.serviceAccountKey?.reveal(), 'key');
assert.equal(service.oauthClientId, undefined);
assert.throws(() => decodeCredentials({ auth_method: 'workspace-service-account', delegated_user: 'user' }));
assert.throws(() => decodeCredentials({ auth_method: 'unknown' }));
assert.throws(() => decodeCredentials({ auth_method: 0 }));
`

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
