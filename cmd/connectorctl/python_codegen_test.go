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

func TestPythonRefreshDeclarationUsesDecodedManifest(t *testing.T) {
	manifestPath := filepath.Join("..", "..", "connectors", "google", "gmail", "connector.yaml")
	manifest, err := load(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Spec.Auth.Methods[0].OAuth2.Refresh.AdditionalParameters = map[string]string{
		"description": "retained", "protocol": "oauth2",
	}
	manifest.Spec.Auth.Methods[0].OAuth2.TokenEndpoint = schema.ValueSource{
		FromCredential: "oauth_client_id", Values: map[string]string{"description": "https://example.com/token"},
	}
	files, err := generatedPythonFiles(manifestPath, manifest)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("..", "..", "connectors", "google", "gmail", "python", "src", "dex_connectors", "google_gmail", "_generated", "refresh.py")
	declaration := string(files[path])
	for _, expected := range []string{"'defaultMethod': 'google-oauth'", "'requiredScopes'", "'workspace-domain-delegation'", "'oauth_client_id': 'oauth_client_id'", "'description': 'retained'", "'protocol': 'oauth2'", "'values': {'description': 'https://example.com/token'}"} {
		if !strings.Contains(declaration, expected) {
			t.Errorf("generated refresh declaration omits %s", expected)
		}
	}
}

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
	manifest.Spec.Auth.Methods[0].Fields = append(manifest.Spec.Auth.Methods[0].Fields, schema.Field{Name: "webhook_signing_key", GoName: "WebhookSigningKey", Type: "secretString", Description: "Optional webhook signing key.", Required: false})
	manifest.Spec.Auth.Methods[1].Fields = append(manifest.Spec.Auth.Methods[1].Fields, schema.Field{Name: "environment", GoName: "Environment", Type: "enum", Enum: []string{"production", "sandbox"}, Description: "Login environment.", Required: false}, schema.Field{Name: "login_url", GoName: "LoginURL", Type: "url", Description: "Login URL.", Required: false})
	manifest.Spec.Auth.Methods = append(manifest.Spec.Auth.Methods, schema.AuthMethod{ID: "typed", Fields: []schema.Field{
		{Name: "request_count", GoName: "RequestCount", Type: "integer", Required: true},
		{Name: "wait_duration", GoName: "WaitDuration", Type: "duration", Required: true},
		{Name: "labels", GoName: "Labels", Type: "stringList", Required: false},
		{Name: "headers", GoName: "Headers", Type: "stringMap", Required: false},
		{Name: "optional_count", GoName: "OptionalCount", Type: "integer", Required: false},
	}})
	manifest.Spec.Auth.Methods = append(manifest.Spec.Auth.Methods, schema.AuthMethod{ID: "optional-only", Fields: []schema.Field{{Name: "optional_note", GoName: "OptionalNote", Type: "string", Required: false}}})
	grantManifest, err := load(filepath.Join("..", "..", "schema", "testdata", "token-grants.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	manifest.Spec.Auth.Methods = append(manifest.Spec.Auth.Methods, grantManifest.Spec.Auth.Methods[2])
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

func TestPythonCalendlyOptionalSecretRefreshRoundTrip(t *testing.T) {
	manifest, err := load(filepath.Join("..", "..", "connectors", "calendly", "connector.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	manifest.Spec.Languages = &schema.Languages{Python: &schema.LanguagePackage{Operations: []string{"listScheduledEvents"}}}
	generated, err := renderPythonCodegen(pythonCodegenInput{Manifest: manifest, PackageName: "calendly_fixture", MockPackageName: "calendlymock"})
	if err != nil {
		t.Fatal(err)
	}
	sources, err := json.Marshal(map[string]string{"config.py": generated["config.py"], "connection.py": generated["connection.py"]})
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("python3", "-c", calendlyOptionalSecretRuntimeTest)
	command.Stdin = bytes.NewReader(sources)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("Calendly optional secret refresh round-trip: %v\n%s", err, output)
	}
}

const calendlyOptionalSecretRuntimeTest = `
import ast, json, sys
from dataclasses import dataclass, replace
from typing import Any
sources = json.load(sys.stdin)
class SecretString:
    def __init__(self, value): self.value = value
    def reveal(self): return self.value
namespace = {'__name__': '__main__', 'dataclass': dataclass, 'SecretString': SecretString, 'Any': Any}
config = ast.parse(sources['config.py'])
credentials = next(node for node in config.body if isinstance(node, ast.ClassDef) and node.name == 'Credentials')
exec(compile(ast.Module(body=[credentials], type_ignores=[]), '<credentials>', 'exec'), namespace)
connection = ast.parse(sources['connection.py'])
codec = [node for node in connection.body if isinstance(node, ast.FunctionDef) and node.name in ('decode_credentials', 'encode_credentials')]
exec(compile(ast.Module(body=codec, type_ignores=[]), '<codec>', 'exec'), namespace)
decode, encode = namespace['decode_credentials'], namespace['encode_credentials']
oauth = {'auth_method': 'calendly-oauth', 'oauth_client_id': 'client', 'oauth_client_secret': 'secret', 'access_token': 'access', 'refresh_token': 'refresh'}
pat = {'auth_method': 'personal-access-token', 'access_token': 'token'}
for material in (oauth, pat):
    resolved = decode(material)
    assert resolved.webhook_signing_key.reveal() == ''
    assert encode(resolved) == material
    assert decode(encode(resolved)).webhook_signing_key.reveal() == ''
    assert encode(replace(resolved, webhook_signing_key=SecretString('key')))['webhook_signing_key'] == 'key'
`

const multiAuthPythonRuntimeTest = `
import ast, asyncio, json, sys
from dataclasses import dataclass
from typing import Any
from urllib.parse import urlsplit
sources = json.load(sys.stdin)
config_tree = ast.parse(sources['config.py'])
config_class = next(node for node in config_tree.body if isinstance(node, ast.ClassDef) and node.name == 'Config')
config_namespace = {'__name__': '__main__', 'dataclass': dataclass}
exec(compile(ast.Module(body=[config_class], type_ignores=[]), '<generated-config>', 'exec'), config_namespace)
assert config_namespace['Config'](account_id='account').region == ''
source = sources['connection.py']
tree = ast.parse(source)
credential_decoder = next(node for node in tree.body if isinstance(node, ast.FunctionDef) and node.name == 'decode_credentials')
shared_decoder = next(node for node in tree.body if isinstance(node, ast.FunctionDef) and node.name == 'decode_shared_credentials')
credential_encoder = next(node for node in tree.body if isinstance(node, ast.FunctionDef) and node.name == 'encode_credentials')
url_validator = next(node for node in tree.body if isinstance(node, ast.FunctionDef) and node.name == '_is_absolute_credential_url')
config_decoder = next(node for node in tree.body if isinstance(node, ast.FunctionDef) and node.name == '_decode_config')
class SecretString:
    def __init__(self, value): self.value = value
    def reveal(self): return self.value
class Credentials:
    def __init__(self, **values): self.__dict__.update(values)
class Config:
    def __init__(self, **values): self.__dict__.update(values)
from re import search
namespace = {'Any': Any, 'Call': object, 'LoadedProject': object, 'ConnectionKey': object, 'Credentials': Credentials, 'Config': Config, 'SecretString': SecretString, 'urlsplit': urlsplit, 'search': search}
exec(compile(ast.Module(body=[url_validator, credential_decoder, shared_decoder, credential_encoder, config_decoder], type_ignores=[]), '<generated>', 'exec'), namespace)
assert namespace['_decode_config']({'accountId':'account'}).workspace_id == ''
assert namespace['_decode_config']({'accountId':'account', 'workspaceId':'workspace'}).workspace_id == 'workspace'
def resolve(values, shared=False):
    return namespace['decode_shared_credentials'](values) if shared else namespace['decode_credentials'](values)
default = resolve({'auth_method':None, 'oauth_client_id':'id', 'oauth_client_secret':'secret', 'access_token':'access', 'refresh_token':'refresh'})
assert default.auth_method_id == 'google-oauth' and not hasattr(default, 'service_account_key')
assert namespace['encode_credentials'](default)['auth_method'] == 'google-oauth'
assert default.webhook_signing_key.reveal() == ''
assert 'webhook_signing_key' not in namespace['encode_credentials'](default)
assert resolve(namespace['encode_credentials'](default)).webhook_signing_key.reveal() == ''
shared = resolve({'oauth_client_id':'id', 'access_token':'access'}, shared=True)
assert shared.access_token.value == 'access' and not hasattr(shared, 'oauth_client_secret')
app_only = resolve({'auth_method':'app-only', 'tenant_id':'tenant', 'client_id':'id', 'access_token':'access'}, shared=True)
assert app_only.access_token.value == 'access' and not hasattr(app_only, 'client_secret')
alternate = resolve({'auth_method':'workspace-service-account', 'service_account_key':'key', 'delegated_user':'user', 'session_note':'note', 'environment':'sandbox', 'login_url':'https://example.com/login', 'oauth_client_id':'other'})
assert alternate.auth_method_id == 'workspace-service-account' and alternate.service_account_key.value == 'key'
assert alternate.session_note == 'note'
assert not hasattr(alternate, 'oauth_client_id')
for invalid_url in (' https://example.com', 'https://example.com\n', 'https://exa mple.com', 'https://%zz', 'https://example.com/%zz', 'https://u:p@[secretish]/'):
    try: resolve({'auth_method':'workspace-service-account', 'service_account_key':'key', 'delegated_user':'user', 'login_url':invalid_url})
    except ValueError as error: assert str(error) == 'Multi Auth Fixture credential login_url must be an absolute URL', str(error)
    else: raise AssertionError('invalid URL accepted')
assert resolve({'auth_method':'workspace-service-account', 'service_account_key':'key', 'delegated_user':'user', 'login_url':'https://example.com:99999'}).login_url.endswith(':99999')
typed = resolve({'auth_method':'typed', 'request_count':1, 'wait_duration':2, 'labels':[None, 'x'], 'headers':{'empty':None, 'filled':'x'}, 'optional_count':0})
assert typed.labels == ['', 'x'] and typed.headers == {'empty':'', 'filled':'x'} and not hasattr(typed, 'optional_count')
assert resolve({'auth_method':'optional-only'}, shared=True).auth_method_id == 'optional-only'
for invalid_collection in ({'labels':[1]}, {'headers':{'bad':1}}):
    try: resolve({'auth_method':'typed', 'request_count':1, 'wait_duration':2, **invalid_collection})
    except ValueError as error: assert 'has an invalid type' in str(error), str(error)
    else: raise AssertionError('invalid collection element accepted')
for field in ('request_count', 'wait_duration'):
    for invalid_value in (0, -1):
        values = {'auth_method':'typed', 'request_count':1, 'wait_duration':2, field:invalid_value}
        try: resolve(values)
        except ValueError as error: assert str(error) == 'Multi Auth Fixture credential ' + field + ' must be positive', str(error)
        else: raise AssertionError('nonpositive required number accepted')
try: resolve({'auth_method':'typed', 'request_count':1, 'wait_duration':2, 'optional_count':-1})
except ValueError as error: assert str(error) == 'Multi Auth Fixture credential optional_count cannot be negative', str(error)
else: raise AssertionError('negative optional number accepted')
optional_null = resolve({'auth_method':'workspace-service-account', 'service_account_key':'key', 'delegated_user':'user', 'session_note':None})
assert optional_null.session_note == ''
for values, message in (
    ({'auth_method':'workspace-service-account', 'service_account_key':None, 'delegated_user':'user'}, 'credential service_account_key is required'),
    ({'auth_method':'unknown'}, 'credential auth_method is invalid'),
    ({'auth_method':0}, 'credential auth_method is invalid'),
    ({'auth_method':'google-oauth', 'oauth_client_id':'id', 'access_token':'access', 'oauth_client_secret':'secret', 'refresh_token':'refresh', 'service_account_key':17}, 'credential service_account_key has an invalid type'),
    ({'auth_method':'google-oauth', 'oauth_client_id':'id', 'access_token':'access', 'oauth_client_secret':'secret', 'refresh_token':'refresh', 'unknown':'value'}, 'stored credential does not match'),
    ({'auth_method':'workspace-service-account', 'service_account_key':'key', 'delegated_user':'user', 'environment':'staging'}, 'credential environment is invalid'),
    ({'auth_method':'workspace-service-account', 'service_account_key':'key', 'delegated_user':'user', 'login_url':'relative'}, 'credential login_url must be an absolute URL'),
):
    try: resolve(values)
    except ValueError as error: assert message in str(error), str(error)
    else: raise AssertionError('invalid selected method was accepted')
for values in ({'oauth_client_id':'id'}, {'access_token':'access'}):
    try: resolve(values, shared=True)
    except ValueError: pass
    else: raise AssertionError('incomplete shared credential was accepted')
try: resolve({'auth_method':'app-only', 'tenant_id':'tenant', 'client_id':'id'}, shared=True)
except ValueError as error: assert 'credential access_token is required' in str(error)
else: raise AssertionError('shared minted token was optional')
`

func TestPythonCodegenSingleAuthGolden(t *testing.T) {
	manifest, err := load(filepath.Join("..", "..", "connectors", "spotify", "connector.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	manifest.Spec.Auth.Fields[1].Required = true
	manifest.Spec.Languages = &schema.Languages{Python: &schema.LanguagePackage{Operations: []string{"listPlaylistTracks"}}}
	generated, err := renderPythonCodegen(pythonCodegenInput{Manifest: manifest, PackageName: "spotify_fixture", MockPackageName: "spotifymock"})
	if err != nil {
		t.Fatal(err)
	}
	goldenPath := filepath.Join("testdata", "single-auth-golden", "connection.py")
	if os.Getenv("UPDATE_SINGLE_AUTH_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, []byte(generated["connection.py"]), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	if generated["connection.py"] != string(want) {
		t.Fatal("single-auth Python connection differs from golden output")
	}
	pythonSources, err := json.Marshal(map[string]string{"config.py": generated["config.py"], "connection.py": generated["connection.py"]})
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("python3", "-c", singleAuthPythonRuntimeTest)
	command.Stdin = bytes.NewReader(pythonSources)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated single-auth credential behavior: %v\n%s", err, output)
	}
}

func TestPythonCodegenJWTBearerGolden(t *testing.T) {
	manifest, err := load(filepath.Join("..", "..", "schema", "testdata", "token-grants.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	manifest.Spec.Auth.Methods = []schema.AuthMethod{manifest.Spec.Auth.Methods[len(manifest.Spec.Auth.Methods)-1]}
	manifest.Spec.Auth.DefaultMethod = "jwt-bearer"
	manifest.Spec.Languages = &schema.Languages{Python: &schema.LanguagePackage{Operations: []string{"getThing"}}}
	generated, err := renderPythonCodegen(pythonCodegenInput{Manifest: manifest, PackageName: "jwt_bearer_fixture", MockPackageName: "jwtbearermock"})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"connection.py", "refresh.py"} {
		goldenPath := filepath.Join("testdata", "jwt-bearer-golden", name)
		if os.Getenv("UPDATE_JWT_BEARER_GOLDEN") == "1" {
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
			t.Errorf("%s differs from JWT bearer golden output", name)
		}
	}
	if !strings.Contains(generated["connection.py"], "decode_shared_credentials") ||
		!strings.Contains(generated["connection.py"], "RefreshingCredentialProvider") {
		t.Error("JWT bearer connection omits renewable shared credentials")
	}
}

const singleAuthPythonRuntimeTest = `
import ast, asyncio, json, sys
from dataclasses import dataclass
sources = json.load(sys.stdin)
class SecretString:
    def __init__(self, value): self.value = value
    def reveal(self): return self.value
config_tree = ast.parse(sources['config.py'])
credentials_class = next(node for node in config_tree.body if isinstance(node, ast.ClassDef) and node.name == 'Credentials')
namespace = {'__name__':'__main__', 'dataclass':dataclass, 'SecretString':SecretString, 'Call':object, 'LoadedProject':object, 'ConnectionKey':object}
exec(compile(ast.Module(body=[credentials_class], type_ignores=[]), '<generated-config>', 'exec'), namespace)
tree = ast.parse(sources['connection.py'])
credential_decoder = next(node for node in tree.body if isinstance(node, ast.FunctionDef) and node.name == 'decode_credentials')
shared_decoder = next(node for node in tree.body if isinstance(node, ast.FunctionDef) and node.name == 'decode_shared_credentials')
credential_encoder = next(node for node in tree.body if isinstance(node, ast.FunctionDef) and node.name == 'encode_credentials')
credential_source = next(node for node in tree.body if isinstance(node, ast.ClassDef) and node.name == '_ProjectCredentialSource')
exec(compile(ast.Module(body=[credential_decoder, shared_decoder, credential_encoder], type_ignores=[]), '<generated-connection>', 'exec'), namespace)
def resolve(values, shared=False):
    return namespace['decode_shared_credentials'](values) if shared else namespace['decode_credentials'](values)
complete = resolve({'oauth_client_id':'id', 'oauth_client_secret':'secret', 'access_token':'access', 'refresh_token':'refresh'})
assert complete.oauth_client_id == 'id' and complete.oauth_client_secret.reveal() == 'secret' and complete.refresh_token.reveal() == 'refresh'
assert namespace['encode_credentials'](complete) == {'oauth_client_id':'id', 'oauth_client_secret':'secret', 'access_token':'access', 'refresh_token':'refresh'}
shared = resolve({'access_token':'access'}, shared=True)
assert shared.oauth_client_id == '' and shared.oauth_client_secret.reveal() == '' and shared.refresh_token.reveal() == '' and shared.access_token.reveal() == 'access'
absent_optional = resolve({'oauth_client_secret':'secret', 'access_token':'access'})
assert absent_optional.oauth_client_id == '' and absent_optional.oauth_client_secret.reveal() == 'secret' and absent_optional.refresh_token.reveal() == ''
try: resolve({'access_token':'access'})
except ValueError as error: assert str(error) == 'Spotify credential oauth_client_secret is required'
else: raise AssertionError('private host-kept secret was optional')
for shared_flag in (False, True):
    try: resolve({'access_token':'access', 'unknown':'value'}, shared=shared_flag)
    except ValueError as error: assert str(error) == 'Spotify stored credential does not match the connector credential fields'
    else: raise AssertionError('unknown key accepted')
class RefreshDriver:
    def __class_getitem__(cls, item): return cls
    def __init__(self, *args, **kwargs): self.args = args; self.kwargs = kwargs
class RefreshProvider:
    def __init__(self, *args): self.args = args; self.calls = []
    async def resolve_with_refresh(self, call, driver):
        self.calls.append(('resolve', driver)); return complete
    async def resolve_after_rejection(self, call, driver):
        self.calls.append(('rejection', driver)); return complete
class Project:
    connections = object()
from types import SimpleNamespace
namespace.update({'RefreshingCredentialProvider':RefreshProvider, 'ManifestCredentialRefreshDriver':RefreshDriver,
                  'AUTHORIZATION':{'type':'oauth2'}, 'CREDENTIAL_FIELDS':{'access_token':'access_token'},
                  'httpx':SimpleNamespace(AsyncClient=object), 'CredentialRefreshDriver':RefreshDriver})
exec(compile(ast.Module(body=[credential_source], type_ignores=[]), '<generated-source>', 'exec'), namespace)
source = namespace['_ProjectCredentialSource'](Project(), object(), object(), {'tenant':'example'})
assert source.provider.args[2:] == (namespace['decode_credentials'], namespace['encode_credentials'], namespace['decode_shared_credentials'])
assert source.driver.args[1:3] == (namespace['AUTHORIZATION'], namespace['CREDENTIAL_FIELDS'])
assert source.driver.kwargs['settings'] == {'tenant':'example'}
assert asyncio.run(source.resolve(object())) is complete
assert asyncio.run(source.resolve_after_rejection(object())) is complete
assert [kind for kind, _ in source.provider.calls] == ['resolve', 'rejection']
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
