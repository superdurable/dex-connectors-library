// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

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
	"gopkg.in/yaml.v3"
)

func TestPythonCodegenGoldenBitbucketMockUsesProviderIdentity(t *testing.T) {
	manifestPath := filepath.Join("..", "..", "connectors", "atlassian", "bitbucket", "connector.yaml")
	manifest, err := load(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	files, err := generatedPythonFiles(manifestPath, manifest)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"models.py", "definitions.py", "steps.py", "connection.py", "mock.py"} {
		path := filepath.Join(filepath.Dir(manifestPath), "python", "src", "dex_connectors", "atlassian_bitbucket", "_generated", name)
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(files[path], want) {
			t.Errorf("%s differs from generated golden output", name)
		}
	}
	mockPath := filepath.Join(filepath.Dir(manifestPath), "python", "src", "dex_connectors", "atlassian_bitbucket", "_generated", "mock.py")
	mockSource := string(files[mockPath])
	if !strings.Contains(mockSource, `connection_name: str = "bitbucket"`) {
		t.Error("mock default connection name must use metadata.name")
	}
	if !strings.Contains(mockSource, `dex_connectors.atlassian_bitbucket.bitbucketmock`) {
		t.Error("mock helpers must name the real public mock module")
	}
}

func TestPythonCodegenMultiAuthGolden(t *testing.T) {
	manifestPath := filepath.Join("..", "..", "schema", "testdata", "multi-auth.yaml")
	manifest, err := load(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Spec.Languages = &schema.Languages{Python: &schema.LanguagePackage{Operations: []string{"getThing"}}}
	manifest.Spec.Configuration.Fields = []schema.Field{
		{Name: "region", GoName: "Region", Type: "string", Description: "Optional region.", Required: false},
		{Name: "accountId", GoName: "AccountID", Type: "string", Description: "Required account ID.", Required: true},
	}
	manifest.Spec.Auth.Methods[0].Configuration = &schema.Configuration{Fields: []schema.Field{{Name: "workspaceId", GoName: "WorkspaceID", Type: "string", Description: "Workspace for the selected method.", Required: true}}}
	manifest.Spec.Auth.Methods[1].Fields = append(manifest.Spec.Auth.Methods[1].Fields, schema.Field{Name: "session_note", GoName: "SessionNote", Type: "string", Description: "Optional session note.", Required: false})
	generated, err := renderPythonCodegen(pythonCodegenInput{Manifest: manifest, PackageName: "multi_auth_fixture", MockPackageName: "multiauthfixturemock"})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"config.py", "connection.py"} {
		goldenPath := filepath.Join("testdata", "multi-auth-golden", name)
		if os.Getenv("UPDATE_MULTI_AUTH_GOLDEN") == "1" {
			if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(goldenPath, []byte(generated[name]), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		want, err := os.ReadFile(goldenPath)
		if err != nil {
			t.Fatal(err)
		}
		if generated[name] != string(want) {
			t.Errorf("%s differs from multi-auth golden output", name)
		}
	}
	pythonSources, err := json.Marshal(map[string]string{"config.py": generated["config.py"], "connection.py": generated["connection.py"]})
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("python3", "-c", multiAuthPythonRuntimeTest)
	command.Stdin = bytes.NewReader(pythonSources)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated multi-auth credential behavior: %v\n%s", err, output)
	}
}

const multiAuthPythonRuntimeTest = `
import ast, asyncio, json, sys
from dataclasses import dataclass
from typing import Any
sources = json.load(sys.stdin)
config_tree = ast.parse(sources['config.py'])
config_class = next(node for node in config_tree.body if isinstance(node, ast.ClassDef) and node.name == 'Config')
config_namespace = {'__name__': '__main__', 'dataclass': dataclass}
exec(compile(ast.Module(body=[config_class], type_ignores=[]), '<generated-config>', 'exec'), config_namespace)
assert config_namespace['Config'](account_id='account').region == ''
source = sources['connection.py']
tree = ast.parse(source)
credential_source = next(node for node in tree.body if isinstance(node, ast.ClassDef) and node.name == '_ProjectCredentialSource')
config_decoder = next(node for node in tree.body if isinstance(node, ast.FunctionDef) and node.name == '_decode_config')
class SecretString:
    def __init__(self, value): self.value = value
class Credentials:
    def __init__(self, **values): self.__dict__.update(values)
class Config:
    def __init__(self, **values): self.__dict__.update(values)
class Project:
    def __init__(self, values): self.values = values
    async def resolve_credential(self, key, call_id): return self.values
namespace = {'Any': Any, 'Call': object, 'LoadedProject': object, 'ConnectionKey': object, 'Credentials': Credentials, 'Config': Config, 'SecretString': SecretString}
exec(compile(ast.Module(body=[credential_source, config_decoder], type_ignores=[]), '<generated>', 'exec'), namespace)
Source = namespace['_ProjectCredentialSource']
assert namespace['_decode_config']({'accountId':'account'}).workspace_id == ''
assert namespace['_decode_config']({'accountId':'account', 'workspaceId':'workspace'}).workspace_id == 'workspace'
class Call:
    id = 'call'
def resolve(values):
    return asyncio.run(Source(Project(values), object()).resolve(Call()))
default = resolve({'auth_method':None, 'oauth_client_id':'id', 'oauth_client_secret':'secret', 'access_token':'access', 'refresh_token':'refresh', 'service_account_key':17})
assert default.auth_method_id == 'google-oauth' and not hasattr(default, 'service_account_key')
alternate = resolve({'auth_method':'workspace-service-account', 'service_account_key':'key', 'delegated_user':'user', 'session_note':'note', 'oauth_client_id':17})
assert alternate.auth_method_id == 'workspace-service-account' and alternate.service_account_key.value == 'key'
assert alternate.session_note == 'note'
assert not hasattr(alternate, 'oauth_client_id')
optional_null = resolve({'auth_method':'workspace-service-account', 'service_account_key':'key', 'delegated_user':'user', 'session_note':None})
assert not hasattr(optional_null, 'session_note')
for values, message in (
    ({'auth_method':'workspace-service-account', 'service_account_key':None, 'delegated_user':'user'}, 'credential service_account_key is required'),
    ({'auth_method':'unknown'}, 'credential auth_method is invalid'),
    ({'auth_method':0}, 'credential auth_method is invalid'),
):
    try: resolve(values)
    except ValueError as error: assert message in str(error), str(error)
    else: raise AssertionError('invalid selected method was accepted')
`

func TestPythonCodegenKeepsMockScenariosDistinctFromBranchHelpers(t *testing.T) {
	connectorDirectory := filepath.Join("..", "..", "connectors", "google", "calendar")
	manifest, err := load(filepath.Join(connectorDirectory, "connector.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	operationNames := make([]string, 0, len(manifest.Spec.Operations))
	modelNames := make([]string, 0, len(manifest.Spec.Operations)*2)
	for _, operation := range manifest.Spec.Operations {
		operationNames = append(operationNames, operation.Name)
		modelNames = append(modelNames, operation.InputType, operation.OutputType)
	}
	if manifest.Spec.Languages == nil {
		manifest.Spec.Languages = &schema.Languages{}
	}
	manifest.Spec.Languages.Python = &schema.LanguagePackage{Operations: operationNames, Triggers: []string{}}
	graph, err := gomodel.Load(filepath.Join(connectorDirectory, "go"), modelNames)
	if err != nil {
		t.Fatal(err)
	}
	generated, err := renderPythonCodegen(pythonCodegenInput{
		Manifest: manifest, Models: graph.Models, PackageName: "google_calendar", MockPackageName: "calendarmock",
	})
	if err != nil {
		t.Fatal(err)
	}
	mockSource := generated["mock.py"]
	for _, functionName := range []string{"update_event_not_found", "update_event_not_found_mock"} {
		if strings.Count(mockSource, "def "+functionName+"(") != 1 {
			t.Errorf("expected one %s helper", functionName)
		}
	}
}

func TestPythonCodegenAcceptsOpenEnumsAndMissingMockKeys(t *testing.T) {
	connectorDirectory := filepath.Join("..", "..", "connectors", "atlassian", "bitbucket")
	manifest, err := load(filepath.Join(connectorDirectory, "connector.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	graph, err := gomodel.Load(filepath.Join(connectorDirectory, "go"), []string{
		"ListRepositoriesInput", "RepositoryPage", "ListPullRequestsInput", "PullRequestPage",
	})
	if err != nil {
		t.Fatal(err)
	}
	for index := range graph.Models {
		if graph.Models[index].Name == "RepositoryPage" {
			graph.Models[index].Fields = append(graph.Models[index].Fields, gomodel.Field{
				Name: "state", Type: gomodel.Type{Kind: gomodel.Enum, EnumBase: gomodel.String, EnumValues: []string{"open", "closed"}},
			}, gomodel.Field{
				Name: "from", Type: gomodel.Type{Kind: gomodel.Enum, EnumBase: gomodel.String, EnumValues: []string{"source"}},
			})
		}
	}
	generated, err := renderPythonCodegen(pythonCodegenInput{
		Manifest: manifest, Models: graph.Models, PackageName: "atlassian_bitbucket", MockPackageName: "bitbucketmock",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(generated["models.py"], "state: str = _dataclass_field(kw_only=True)") {
		t.Error("required open enum must retain its Go field order as a keyword-only string")
	}
	if strings.Contains(generated["models.py"], "Literal[") {
		t.Error("provider enums are open sets")
	}
	if !strings.Contains(generated["mock.py"], "state=''") {
		t.Error("mock output must supply the Go zero value for an omitted required enum")
	}
	if !strings.Contains(generated["mock.py"], "from_=''") {
		t.Error("mock output must escape a missing required field whose wire name is a Python keyword")
	}
	command := exec.Command("python3", "-c", "import ast, sys; ast.parse(sys.stdin.read())")
	command.Stdin = strings.NewReader(generated["mock.py"])
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated mock syntax: %v: %s", err, output)
	}
}

func TestPythonCodegenPreservesOptionalPointerPresence(t *testing.T) {
	connectorDirectory := filepath.Join("..", "..", "connectors", "atlassian", "bitbucket")
	manifest, err := load(filepath.Join(connectorDirectory, "connector.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	graph, err := gomodel.Load(filepath.Join(connectorDirectory, "go"), []string{
		"ListRepositoriesInput", "RepositoryPage", "ListPullRequestsInput", "PullRequestPage",
	})
	if err != nil {
		t.Fatal(err)
	}
	for index := range graph.Models {
		if graph.Models[index].Name != "ListRepositoriesInput" {
			continue
		}
		graph.Models[index].Fields = append(graph.Models[index].Fields,
			gomodel.Field{Name: "isCompleted", Type: gomodel.Type{Kind: gomodel.Bool}, Optional: true, HasPresence: true},
			gomodel.Field{Name: "waitTimeSeconds", Type: gomodel.Type{Kind: gomodel.Int}, Optional: true, HasPresence: true},
			gomodel.Field{Name: "items", Type: gomodel.Type{Kind: gomodel.List, Elem: &gomodel.Type{Kind: gomodel.String}, Nullable: true}, Optional: true, HasPresence: true},
		)
	}
	generated, err := renderPythonCodegen(pythonCodegenInput{
		Manifest: manifest, Models: graph.Models, PackageName: "atlassian_bitbucket", MockPackageName: "bitbucketmock",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"is_completed: bool | None = _dataclass_field(default=None, metadata={'dex_json_name': 'isCompleted', 'dex_json_omit_default': True})",
		"wait_time_seconds: int | None = _dataclass_field(default=None, metadata={'dex_json_name': 'waitTimeSeconds', 'dex_json_omit_default': True})",
		"items: tuple[str, ...] | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})",
	} {
		if !strings.Contains(generated["models.py"], expected) {
			t.Errorf("missing pointer presence model field %q", expected)
		}
	}
}

func TestPythonGenerateCheckDetectsModelDrift(t *testing.T) {
	manifestSource := filepath.Join("..", "..", "connectors", "atlassian", "bitbucket", "connector.yaml")
	manifestContents, err := os.ReadFile(manifestSource)
	if err != nil {
		t.Fatal(err)
	}
	var manifestDocument map[string]any
	if err := yaml.Unmarshal(manifestContents, &manifestDocument); err != nil {
		t.Fatal(err)
	}
	manifestSpecification, ok := manifestDocument["spec"].(map[string]any)
	if !ok {
		t.Fatal("Bitbucket manifest has no spec")
	}
	languageDeclarations, ok := manifestSpecification["languages"].(map[string]any)
	if !ok {
		t.Fatal("Bitbucket manifest has no language declarations")
	}
	pythonDeclaration, ok := languageDeclarations["python"]
	if !ok {
		t.Fatal("Bitbucket manifest has no Python declaration")
	}
	// Keep the Python drift fixture independent of the real connector's other language packages.
	manifestSpecification["languages"] = map[string]any{"python": pythonDeclaration}
	manifestContents, err = yaml.Marshal(manifestDocument)
	if err != nil {
		t.Fatal(err)
	}
	projectContents, err := os.ReadFile(filepath.Join(filepath.Dir(manifestSource), "python", "pyproject.toml"))
	if err != nil {
		t.Fatal(err)
	}
	repository := t.TempDir()
	connectorDirectory := filepath.Join(repository, "connectors", "atlassian", "bitbucket")
	manifestPath := filepath.Join(connectorDirectory, "connector.yaml")
	packageDirectory := filepath.Join(connectorDirectory, "python", "src", "dex_connectors", "atlassian_bitbucket")
	fixtureFiles := map[string][]byte{
		filepath.Join(repository, "catalog.yaml"): []byte("directories: []\n"),
		manifestPath: manifestContents,
		filepath.Join(connectorDirectory, "go", "go.mod"):             []byte("module example.com/bitbucket\n\ngo 1.24\n"),
		filepath.Join(connectorDirectory, "go", "models.go"):          []byte("package bitbucket\ntype ListRepositoriesInput struct{}\ntype RepositoryPage struct{}\ntype ListPullRequestsInput struct{}\ntype PullRequestPage struct{}\n"),
		filepath.Join(packageDirectory, "__init__.py"):                []byte(""),
		filepath.Join(connectorDirectory, "python", "pyproject.toml"): projectContents,
	}
	for path, contents := range fixtureFiles {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, contents, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := generate([]string{"--lang", "python", manifestPath}); err != nil {
		t.Fatal(err)
	}
	if err := generate([]string{"--check", "--lang", "python", manifestPath}); err != nil {
		t.Fatal(err)
	}
	modelPath := filepath.Join(packageDirectory, "_generated", "models.py")
	if err := os.WriteFile(modelPath, []byte("# drift\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := generate([]string{"--lang", "python", "--check", manifestPath}); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("expected Python model drift, got %v", err)
	}
}

func TestPythonCodegenDecodesOptionalAndTypedConfiguration(t *testing.T) {
	for _, testCase := range []struct {
		name string
		path string
	}{
		{"postgresql", filepath.Join("..", "..", "connectors", "postgresql", "connector.yaml")},
		{"oauth2", filepath.Join("..", "..", "connectors", "superdurable", "oauth2-http", "connector.yaml")},
		{"sqs", filepath.Join("..", "..", "connectors", "amazon", "sqs", "connector.yaml")},
		{"mixpanel", filepath.Join("..", "..", "connectors", "mixpanel", "connector.yaml")},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			manifest, err := load(testCase.path)
			if err != nil {
				t.Fatal(err)
			}
			operationNames := make([]string, 0, len(manifest.Spec.Operations))
			for _, operation := range manifest.Spec.Operations {
				operationNames = append(operationNames, operation.Name)
			}
			if manifest.Spec.Languages == nil {
				manifest.Spec.Languages = &schema.Languages{}
			}
			manifest.Spec.Languages.Python = &schema.LanguagePackage{Operations: operationNames}
			generated, err := renderPythonCodegen(pythonCodegenInput{
				Manifest: manifest, Models: []gomodel.Model{}, PackageName: "configuration_fixture", MockPackageName: "fixturemock",
			})
			if err != nil {
				t.Fatal(err)
			}
			payload, err := json.Marshal(map[string]string{
				"case": testCase.name, "config": generated["config.py"], "connection": generated["connection.py"],
			})
			if err != nil {
				t.Fatal(err)
			}
			const script = `import ast, dataclasses, json, sys, typing
sources = json.load(sys.stdin)
config = next(node for node in ast.parse(sources["config"]).body if isinstance(node, ast.ClassDef) and node.name == "Config")
decode = next(node for node in ast.parse(sources["connection"]).body if isinstance(node, ast.FunctionDef) and node.name == "_decode_config")
namespace = {"dataclass": dataclasses.dataclass, "Any": typing.Any}
exec(compile(ast.Module(body=[config, decode], type_ignores=[]), "generated.py", "exec"), namespace)
decode_config = namespace["_decode_config"]
if sources["case"] == "postgresql":
    required = {"host": "db.example", "database": "app", "user": "dex_app"}
    assert decode_config(required).ssl_mode == "require"
    assert decode_config({**required, "sslMode": "verify-full"}).ssl_mode == "verify-full"
    try:
        decode_config({**required, "sslMode": "unknown"})
    except ValueError:
        pass
    else:
        raise AssertionError("invalid PostgreSQL enum was accepted")
elif sources["case"] == "oauth2":
    required = {"authorizationEndpoint": "https://auth.example", "tokenEndpoint": "https://auth.example/token", "apiBaseUrl": "https://api.example", "scopes": ["openid", "profile"]}
    value = decode_config(required)
    assert value.scopes == ("openid", "profile")
    assert value.token_endpoint_auth_method == "client_secret_post"
    try:
        decode_config({**required, "scopes": "openid"})
    except ValueError:
        pass
    else:
        raise AssertionError("stringList accepted a string")
elif sources["case"] == "sqs":
    value = decode_config({})
    assert value.region == "us-east-1"
    assert value.endpoint == ""
    assert value.default_queue_url == ""
    assert decode_config({"endpoint": "https://sqs.example"}).endpoint == "https://sqs.example"
else:
    assert decode_config({}).project_id == 0
`
			command := exec.Command("python3", "-c", script)
			command.Stdin = bytes.NewReader(payload)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("generated configuration failed: %v: %s", err, output)
			}
		})
	}
}

func TestPythonCodegenCreatesRealNestedMockFacades(t *testing.T) {
	for _, connectorDirectory := range []string{
		filepath.Join("amazon", "sqs"),
		filepath.Join("google", "calendar"),
		filepath.Join("microsoft", "outlook-calendar"),
		filepath.Join("superdurable", "oauth2-http"),
	} {
		t.Run(connectorDirectory, func(t *testing.T) {
			manifest, err := load(filepath.Join("..", "..", "connectors", connectorDirectory, "connector.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			if manifest.Spec.Languages == nil {
				manifest.Spec.Languages = &schema.Languages{}
			}
			operationNames := make([]string, 0, len(manifest.Spec.Operations))
			modelNames := make(map[string]bool)
			for _, operation := range manifest.Spec.Operations {
				operationNames = append(operationNames, operation.Name)
				modelNames[operation.InputType] = true
				modelNames[operation.OutputType] = true
			}
			manifest.Spec.Languages.Python = &schema.LanguagePackage{Operations: operationNames}
			repository := t.TempDir()
			if err := os.WriteFile(filepath.Join(repository, "catalog.yaml"), []byte("directories: []\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			fixtureDirectory := filepath.Join(repository, "connectors", connectorDirectory)
			goDirectory := filepath.Join(fixtureDirectory, "go")
			if err := os.MkdirAll(goDirectory, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(goDirectory, "go.mod"), []byte("module example.com/mockfixture\n\ngo 1.24\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			models := "package mockfixture\n"
			for name := range modelNames {
				models += "type " + name + " struct{}\n"
			}
			if err := os.WriteFile(filepath.Join(goDirectory, "models.go"), []byte(models), 0o644); err != nil {
				t.Fatal(err)
			}
			packageName := pythonImportPackageName(filepath.ToSlash(filepath.Join("connectors", connectorDirectory)))
			packageDirectory := filepath.Join(fixtureDirectory, "python", "src", "dex_connectors", packageName)
			if err := os.MkdirAll(packageDirectory, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(packageDirectory, "__init__.py"), []byte(""), 0o644); err != nil {
				t.Fatal(err)
			}
			files, err := generatedPythonFiles(filepath.Join(fixtureDirectory, "connector.yaml"), manifest)
			if err != nil {
				t.Fatal(err)
			}
			mockPackageName := manifest.Spec.Codegen.Go.Package + "mock"
			facadePath := filepath.Join(packageDirectory, mockPackageName, "__init__.py")
			if !bytes.Contains(files[facadePath], []byte("from .._generated.mock import *")) {
				t.Errorf("missing generated public mock module %s", facadePath)
			}
			generatedMockPath := filepath.Join(packageDirectory, "_generated", "mock.py")
			modulePath := "dex_connectors." + packageName + "." + mockPackageName
			if !bytes.Contains(files[generatedMockPath], []byte(modulePath)) {
				t.Errorf("mock helpers do not name the real %s module", modulePath)
			}
		})
	}
}

func TestPythonCodegenUsesHistoricalInlineModelNames(t *testing.T) {
	for _, testCase := range []struct {
		connector   string
		packageName string
		publicName  string
		goName      string
	}{
		{"cloudflare", "cloudflare", "PurgeRequest", "CachePurgeResult"},
		{"vercel", "vercel", "Pagination", "DeploymentsPagePagination"},
	} {
		t.Run(testCase.connector, func(t *testing.T) {
			manifestPath := filepath.Join("..", "..", "connectors", testCase.connector, "connector.yaml")
			manifest, err := load(manifestPath)
			if err != nil {
				t.Fatal(err)
			}
			files, err := generatedPythonFiles(manifestPath, manifest)
			if err != nil {
				t.Fatal(err)
			}
			mockPath := filepath.Join(filepath.Dir(manifestPath), "python", "src", "dex_connectors", testCase.packageName, "_generated", "mock.py")
			source := string(files[mockPath])
			if !strings.Contains(source, testCase.publicName+"(") {
				t.Errorf("mock output does not construct %s", testCase.publicName)
			}
			if strings.Contains(source, "from ..models import "+testCase.goName) {
				t.Errorf("mock exposes Go inline name %s", testCase.goName)
			}
		})
	}
}

func TestPythonPublishedModelNamesPreventExternalNameCollision(t *testing.T) {
	modelPath := filepath.Join(t.TempDir(), "models.py")
	if err := os.WriteFile(modelPath, []byte("class ExternalFirstItem:\n    pass\n\nclass v1Item:\n    pass\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	publishedNames, err := publishedPythonModelNames(modelPath)
	if err != nil {
		t.Fatal(err)
	}
	goDirectory := filepath.Join("..", "..", "internal", "codegen", "gomodel", "testdata", "models")
	_, err = gomodel.Load(goDirectory, []string{"ExternalFirstItem", "ExternalSameName"}, publishedNames...)
	if err == nil || !strings.Contains(err.Error(), `Go model name "v1Item" conflicts`) {
		t.Fatalf("published Python model name was renamed: %v", err)
	}
}
