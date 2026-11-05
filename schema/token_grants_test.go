// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package schema_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/schema"
)

func TestDecodedProviderTokenResponseRules(t *testing.T) {
	for _, test := range []struct {
		path            string
		missingType     bool
		exactScopes     bool
		scopeSeparators string
		grantTokenTypes []string
	}{
		{path: "slack", scopeSeparators: schema.ScopeSeparatorsWhitespaceAndComma, grantTokenTypes: []string{"bot", "user"}},
		{path: "linkedin", missingType: true, exactScopes: true, scopeSeparators: schema.ScopeSeparatorsWhitespaceAndComma},
		{path: "atlassian/jira", missingType: true, scopeSeparators: schema.ScopeSeparatorsWhitespaceAndComma},
		{path: "atlassian/confluence", missingType: true, scopeSeparators: schema.ScopeSeparatorsWhitespaceAndComma},
		{path: "atlassian/jira-service-management", missingType: true, scopeSeparators: schema.ScopeSeparatorsWhitespaceAndComma},
	} {
		t.Run(test.path, func(t *testing.T) {
			file, err := os.Open(filepath.Join("..", "connectors", test.path, "connector.yaml"))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, file.Close()) })
			manifest, err := schema.Decode(file)
			require.NoError(t, err)
			method := manifest.Spec.Auth.AuthMethods()[0]
			refresh := method.OAuth2.Refresh
			require.Equal(t, test.missingType, refresh.AcceptsMissingTokenType)
			require.Equal(t, test.exactScopes, refresh.ExactScopes)
			require.Equal(t, test.scopeSeparators, refresh.ScopeSeparators)
			if len(test.grantTokenTypes) > 0 {
				require.Len(t, refresh.Grants, len(test.grantTokenTypes))
				for index, tokenType := range test.grantTokenTypes {
					require.Equal(t, []string{tokenType}, refresh.Grants[index].AcceptedTokenTypes)
					require.NotNil(t, refresh.Grants[index].ExactScopes)
					require.True(t, *refresh.Grants[index].ExactScopes)
				}
			}
		})
	}
}

func TestOAuthAlternativeScopesAndDelegatedSubjectDeclarations(t *testing.T) {
	decodeConnector := func(directory string) schema.Manifest {
		manifestFile, err := os.Open(filepath.Join("..", "connectors", directory, "connector.yaml"))
		require.NoError(t, err)
		defer manifestFile.Close()
		manifest, err := schema.Decode(manifestFile)
		require.NoError(t, err)
		return manifest
	}
	youtube := decodeConnector("google/youtube")
	readOnly := youtube.Spec.Auth.Methods[0].OAuth2.Refresh
	require.Equal(t, [][]string{{"https://www.googleapis.com/auth/youtube.readonly"}, {"https://www.googleapis.com/auth/youtube"}}, readOnly.AcceptedScopeSets)
	salesforce := decodeConnector("salesforce")
	require.Equal(t, [][]string{{"api"}, {"full"}}, salesforce.Spec.Auth.Methods[0].OAuth2.Refresh.AcceptedScopeSets)
	require.Equal(t, [][]string{{"api"}, {"full"}}, salesforce.Spec.Auth.Methods[2].ServiceAccount.JWTBearer.AcceptedScopeSets)
	for _, connector := range []string{"calendar", "docs", "drive", "forms"} {
		manifest := decodeConnector("google/" + connector)
		for _, method := range manifest.Spec.Auth.Methods {
			if method.ServiceAccount != nil {
				require.Equal(t, "goMailAddress", method.ServiceAccount.JWTBearer.SubjectValidation)
			}
		}
	}
}

func TestMissingExpiryRejectRoundTrips(t *testing.T) {
	var policy schema.MissingExpiry
	require.NoError(t, json.Unmarshal([]byte(`"reject"`), &policy))
	require.True(t, policy.Reject)
	encoded, err := json.Marshal(policy)
	require.NoError(t, err)
	require.JSONEq(t, `"reject"`, string(encoded))
	require.ErrorContains(t, json.Unmarshal([]byte(`"unknown"`), &policy), "missingExpiry")
	manifestFile, err := os.Open(filepath.Join("..", "connectors", "helpscout", "connector.yaml"))
	require.NoError(t, err)
	defer manifestFile.Close()
	manifest, err := schema.Decode(manifestFile)
	require.NoError(t, err)
	require.True(t, manifest.Spec.Auth.ClientCredentials.MissingExpiry.Reject)
}

func TestGrantExactScopesFalseSurvivesJSONEncoding(t *testing.T) {
	falseValue := false
	encoded, err := json.Marshal(schema.OAuthRefreshGrant{ExactScopes: &falseValue})
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"exactScopes":false`)
}

func TestResponseScopePolicyAndScopeSetValidation(t *testing.T) {
	manifestFile, err := os.Open(filepath.Join("..", "connectors", "salesforce", "connector.yaml"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, manifestFile.Close()) })
	manifest, err := schema.Decode(manifestFile)
	require.NoError(t, err)
	refresh := manifest.Spec.Auth.Methods[0].OAuth2.Refresh
	refresh.ResponseScopes = schema.ResponseScopesIgnore
	require.ErrorContains(t, manifest.Validate(), "responseScopes ignore cannot declare requiredScopes, exactScopes, or scopeSeparators")
	refresh.ResponseScopes = "unknown"
	require.ErrorContains(t, manifest.Validate(), "responseScopes must be ignore")
	refresh.ResponseScopes = ""
	refresh.AcceptedScopeSets = [][]string{{"api", "full"}, {"full", "api"}}
	require.ErrorContains(t, manifest.Validate(), "must not repeat a scope set")
	refresh.AcceptedScopeSets = [][]string{{"api"}}
	refresh.RequiredScopes = []string{"api"}
	require.ErrorContains(t, manifest.Validate(), "cannot combine requiredScopes and acceptedScopeSets")
}

func TestInvalidRefreshScopeSeparatorsAreRejected(t *testing.T) {
	contents, err := os.ReadFile("../connectors/atlassian/jira/connector.yaml")
	require.NoError(t, err)
	invalid := strings.Replace(string(contents), "scopeSeparators: whitespaceAndComma", "scopeSeparators: comma", 1)
	_, err = schema.Decode(strings.NewReader(invalid))
	require.ErrorContains(t, err, "scopeSeparators")
}

func TestNewRefreshDeclarationsRejectInvalidValues(t *testing.T) {
	manifestFile, err := os.Open("../connectors/hubspot/connector.yaml")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, manifestFile.Close()) })
	manifest, err := schema.Decode(manifestFile)
	require.NoError(t, err)
	refresh := manifest.Spec.Auth.Methods[1].OAuth2.Refresh
	refresh.ResponseScopeField = "unknown"
	require.ErrorContains(t, manifest.Validate(), "responseScopeField must be scope or scopesArray")
	refresh.ResponseScopeField = "scopesArray"
	refresh.ExpiryCap = "-1h"
	require.ErrorContains(t, manifest.Validate(), "expiryCap must be a positive duration")
	refresh.ExpiryCap = ""
	refresh.ScopeResourcePrefixCaseInsensitive = true
	require.ErrorContains(t, manifest.Validate(), "scopeResourcePrefixCaseInsensitive requires scopeResourcePrefix")
	refresh.ScopeResourcePrefixCaseInsensitive = false
	refresh.ResponseURLValidation = &schema.ResponseURLValidation{Credential: "access_token", AllowedHosts: []string{"example.com"}}
	require.ErrorContains(t, manifest.Validate(), "responseURLValidation credential must name a URL or string credential")
	refresh.ResponseURLValidation = &schema.ResponseURLValidation{Credential: "instance_url", AllowedHosts: []string{"example.com"}, AllowAnyHTTPS: true}
	require.ErrorContains(t, manifest.Validate(), "cannot combine allowAnyHTTPS and allowedHosts")
	refresh.ResponseURLValidation = nil
	refresh.RefreshWhenCredentialBlank = "oauth_client_id"
	require.ErrorContains(t, manifest.Validate(), "refreshWhenCredentialBlank must name a mapped credential field")
	refresh.RefreshWhenCredentialBlank = ""
	refresh.ExpiryCap = "30m"
	require.NoError(t, manifest.Validate())
}

func TestDecodeTokenGrantDeclarations(t *testing.T) {
	manifest := decodeFixture(t, "token-grants.yaml")
	methods := map[string]schema.AuthMethod{}
	for _, method := range manifest.Spec.Auth.Methods {
		methods[method.ID] = method
	}

	userOAuth := methods["user-oauth"].OAuth2
	require.Equal(t, &schema.ValueSource{Value: schema.ClientSecretBasicAuthMethod}, userOAuth.TokenEndpointAuthMethod)
	require.Equal(t, schema.JSONTokenRequestEncoding, userOAuth.TokenRequestEncoding)
	require.Equal(t, &schema.OAuthRefresh{
		TerminalErrorCodes:   []string{"invalid_grant", "invalid_client"},
		MissingExpiry:        &schema.MissingExpiry{Lifetime: "2h"},
		StoredMissingExpiry:  schema.StoredMissingExpiryRefresh,
		AdditionalParameters: map[string]string{"scope": "offline_access files.read"},
	}, userOAuth.Refresh)

	botRefresh := methods["bot-oauth"].OAuth2.Refresh
	require.Equal(t, &schema.MissingExpiry{}, botRefresh.MissingExpiry, "keep is the empty lifetime")
	require.Len(t, botRefresh.Grants, 2)
	require.Equal(t, schema.UserScopes, botRefresh.Grants[1].GrantedScopes)

	appOnly := methods["app-only"].ClientCredentials
	require.Equal(t, schema.ValueSource{Template: "https://login.example.com/{tenant_id}/token"}, appOnly.TokenEndpoint)
	require.Equal(t, []string{"https://api.example.com/.default"}, appOnly.Scopes)

	delegation := methods["delegation"].ServiceAccount.JWTBearer
	require.Equal(t, schema.ValueSource{FromKey: "private_key"}, delegation.PrivateKey)
	require.Equal(t, &schema.ValueSource{FromCredential: "delegated_user"}, delegation.Subject)
	require.Equal(t, []schema.CredentialCopy{{Credential: "primary_email", FromCredential: "delegated_user"}}, delegation.CredentialCopies)

	jwtBearer := methods["jwt-bearer"].ServiceAccount.JWTBearer
	require.Equal(t, "https://test.example.com/token", jwtBearer.TokenEndpoint.Values["sandbox"])
	require.Equal(t, "https://login.example.com", jwtBearer.Audience.Values[""], "a blank credential has its own translation")
}

func TestTokenGrantDeclarationsMarshalForReleaseConsumers(t *testing.T) {
	manifest := decodeFixture(t, "token-grants.yaml")
	encoded, err := json.Marshal(manifest.Spec.Auth.Methods)
	require.NoError(t, err)
	var methods []struct {
		ID     string `json:"id"`
		OAuth2 *struct {
			Refresh map[string]any `json:"refresh"`
		} `json:"oauth2"`
		ClientCredentials map[string]any `json:"clientCredentials"`
		ServiceAccount    *struct {
			JWTBearer map[string]any `json:"jwtBearer"`
		} `json:"serviceAccount"`
	}
	require.NoError(t, json.Unmarshal(encoded, &methods))
	require.Equal(t, map[string]any{"lifetime": "2h"}, methods[0].OAuth2.Refresh["missingExpiry"])
	require.Equal(t, "refresh", methods[0].OAuth2.Refresh["storedMissingExpiry"])
	require.Equal(t, "keep", methods[1].OAuth2.Refresh["missingExpiry"])
	require.Equal(t, map[string]any{"template": "https://login.example.com/{tenant_id}/token"}, methods[2].ClientCredentials["tokenEndpoint"])
	require.Equal(t, "refresh", methods[2].ClientCredentials["storedMissingExpiry"])
	require.Equal(t, map[string]any{"fromKey": "client_email"}, methods[3].ServiceAccount.JWTBearer["issuer"])
	require.Equal(t, "1h", methods[3].ServiceAccount.JWTBearer["lifetime"])
	require.Equal(t, "refresh", methods[3].ServiceAccount.JWTBearer["storedMissingExpiry"])

	literal, err := json.Marshal(schema.ValueSource{Value: "https://login.example.com/token"})
	require.NoError(t, err)
	require.JSONEq(t, `"https://login.example.com/token"`, string(literal), "a literal stays a plain string")

	var decoded []schema.AuthMethod
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	require.Equal(t, manifest.Spec.Auth.Methods, decoded, "release metadata decodes back into the manifest")
}

func TestTokenGrantDeclarationsRejectInvalidReferences(t *testing.T) {
	for name, testCase := range map[string]struct {
		edits   []manifestEdit
		problem string
	}{
		"an unknown client authentication": {edits: []manifestEdit{
			{"tokenEndpointAuthMethod: client_secret_basic", "tokenEndpointAuthMethod: private_key_jwt"},
		}, problem: "tokenEndpointAuthMethod must be client_secret_post or client_secret_basic"},
		"a client authentication setting with other values": {edits: []manifestEdit{
			{"tokenEndpointAuthMethod: client_secret_basic", "tokenEndpointAuthMethod: {fromSetting: tenantId}"},
		}, problem: "tokenEndpointAuthMethod must name an enum field or translate its value with values"},
		"a client authentication template": {edits: []manifestEdit{
			{"tokenEndpointAuthMethod: client_secret_basic", `tokenEndpointAuthMethod: {template: "https://example.com/{tenantId}/x"}`},
		}, problem: "tokenEndpointAuthMethod cannot be a template"},
		"an unknown request encoding": {edits: []manifestEdit{
			{"tokenRequestEncoding: json", "tokenRequestEncoding: xml"},
		}, problem: "tokenRequestEncoding must be form or json"},
		"a missing expiry that is neither keep nor a lifetime": {edits: []manifestEdit{
			{"missingExpiry: keep", "missingExpiry: refresh"},
		}, problem: "missingExpiry must be reject, keep, or {lifetime: <duration>}"},
		"an unknown stored missing expiry policy": {edits: []manifestEdit{
			{"storedMissingExpiry: refresh", "storedMissingExpiry: never"},
		}, problem: "storedMissingExpiry must be keep, refresh, or refreshIfRefreshTokenPresent"},
		"a negative missing expiry lifetime": {edits: []manifestEdit{
			{"missingExpiry: {lifetime: 2h}\n            additionalParameters", "missingExpiry: {lifetime: -2h}\n            additionalParameters"},
		}, problem: "missingExpiry lifetime must be a positive duration"},
		"an additional parameter that replaces the grant": {edits: []manifestEdit{
			{"additionalParameters: {scope: offline_access files.read}", "additionalParameters: {refresh_token: fixed}"},
		}, problem: "additionalParameters must be non-empty and cannot replace refresh_token"},
		"a refresh grant of a non-secret field": {edits: []manifestEdit{
			{"refreshTokenCredential: user_refresh_token", "refreshTokenCredential: client_id"},
		}, problem: "grants require unique secretString refreshTokenCredential fields"},
		"an unknown granted scope set": {edits: []manifestEdit{
			{"grantedScopes: userScopes", "grantedScopes: botScopes"},
		}, problem: "grantedScopes must be scopes or userScopes"},
		"client credentials on an OAuth method": {edits: []manifestEdit{
			{"        type: apiKey\n        connectionKind: example-app-only", "        type: serviceAccount\n        connectionKind: example-app-only"},
		}, problem: "clientCredentials requires apiKey auth"},
		"a template placeholder that names nothing": {edits: []manifestEdit{
			{"{tenant_id}/token", "{tenant}/token"},
		}, problem: "template placeholder tenant must name a credential field or setting of the method"},
		"a template rule for another field": {edits: []manifestEdit{
			{`tokenEndpoint: {template: "https://login.example.com/{tenant_id}/token"}`, `tokenEndpoint: {template: "https://login.example.com/{tenant_id}/token", templateValueRules: {other: {trimSpace: true}}}`},
		}, problem: "templateValueRules other must name a template placeholder"},
		"an unanchored template rule": {edits: []manifestEdit{
			{`tokenEndpoint: {template: "https://login.example.com/{tenant_id}/token"}`, `tokenEndpoint: {template: "https://login.example.com/{tenant_id}/token", templateValueRules: {tenant_id: {patterns: [example]}}}`},
		}, problem: "patterns must use ^(?:...)$"},
		"a pattern with inline flags": {edits: []manifestEdit{
			{`tokenEndpoint: {template: "https://login.example.com/{tenant_id}/token"}`, `tokenEndpoint: {template: "https://login.example.com/{tenant_id}/token", templateValueRules: {tenant_id: {patterns: ['^(?:(?i)example)$']}}}`},
		}, problem: "pattern uses non-portable syntax"},
		"a pattern with Go Unicode properties": {edits: []manifestEdit{
			{`tokenEndpoint: {template: "https://login.example.com/{tenant_id}/token"}`, `tokenEndpoint: {template: "https://login.example.com/{tenant_id}/token", templateValueRules: {tenant_id: {patterns: ['^(?:\pL+)$']}}}`},
		}, problem: "pattern uses non-portable syntax"},
		"a template that fills the host": {edits: []manifestEdit{
			{"https://login.example.com/{tenant_id}/token", "https://{tenant_id}.example.com/token"},
		}, problem: "template must start with a fixed HTTPS origin"},
		"a plain HTTP token endpoint": {edits: []manifestEdit{
			{"sandbox: https://test.example.com/token", "sandbox: http://test.example.com/token"},
		}, problem: "tokenEndpoint values must be absolute HTTPS URLs"},
		"a client credentials endpoint from a setting that is not a required url": {edits: []manifestEdit{
			{`tokenEndpoint: {template: "https://login.example.com/{tenant_id}/token"}`, "tokenEndpoint: {fromSetting: tenantId}"},
		}, problem: "clientCredentials tokenEndpoint fromSetting tenantId must name a url configuration field"},
		"a literal list where one value belongs": {edits: []manifestEdit{
			{"issuer: {fromKey: client_email}", "issuer: [a, b]"},
		}, problem: "issuer requires exactly one of a literal, fromCredential, fromSetting, fromKey, or template"},
		"a client credentials endpoint from a key member": {edits: []manifestEdit{
			{`tokenEndpoint: {template: "https://login.example.com/{tenant_id}/token"}`, "tokenEndpoint: {fromKey: token_uri}"},
		}, problem: "clientCredentials tokenEndpoint fromKey requires keyCredential"},
		"a key member without a key": {edits: []manifestEdit{
			{"            keyCredential: service_account_key\n", ""},
		}, problem: "fromKey requires keyCredential"},
		"a source with two values": {edits: []manifestEdit{
			{"subject: {fromCredential: delegated_user}", "subject: {fromCredential: delegated_user, fromKey: subject}"},
		}, problem: "subject requires exactly one of a literal, fromCredential, fromSetting, fromKey, or template"},
		"an unknown source": {edits: []manifestEdit{
			{"issuer: {fromKey: client_email}", "issuer: {fromEnvironment: CLIENT_EMAIL}"},
		}, problem: "field fromEnvironment is not supported"},
		"a missing assertion lifetime": {edits: []manifestEdit{
			{"            lifetime: 1h\n", ""},
		}, problem: "lifetime must be a positive duration"},
		"a copy from an unknown field": {edits: []manifestEdit{
			{"{credential: primary_email, fromCredential: delegated_user}", "{credential: primary_email, fromCredential: user}"},
		}, problem: "credentialCopies must name credential fields"},
		"a literal private key": {edits: []manifestEdit{
			{"privateKey: {fromCredential: private_key}", "privateKey: inline-private-key"},
		}, problem: "privateKey must be {fromKey} or {fromCredential} of a secretString field, without values"},
		"a private key from a setting": {edits: []manifestEdit{
			{"privateKey: {fromCredential: private_key}", "privateKey: {fromSetting: tenantId}"},
		}, problem: "privateKey must be {fromKey} or {fromCredential} of a secretString field, without values"},
		"a private key template": {edits: []manifestEdit{
			{"privateKey: {fromCredential: private_key}", `privateKey: {template: "https://keys.example.com/{client_id}"}`},
		}, problem: "privateKey must be {fromKey} or {fromCredential} of a secretString field, without values"},
		"a private key from a field that is not secret": {edits: []manifestEdit{
			{"privateKey: {fromCredential: private_key}", "privateKey: {fromCredential: client_id}"},
		}, problem: "privateKey must be {fromKey} or {fromCredential} of a secretString field, without values"},
		"a translated private key": {edits: []manifestEdit{
			{"privateKey: {fromCredential: private_key}", "privateKey: {fromCredential: private_key, values: {sandbox: inline-private-key}}"},
		}, problem: "privateKey must be {fromKey} or {fromCredential} of a secretString field, without values"},
		"a copy of a host-kept field": {edits: []manifestEdit{
			{"{credential: primary_email, fromCredential: delegated_user}", "{credential: access_token, fromCredential: service_account_key}"},
		}, problem: "credentialCopies cannot copy host-kept field service_account_key"},
		"a mint without refreshable": {edits: []manifestEdit{
			{"    refreshable: true\n", ""},
		}, problem: "spec.auth.refreshable must be true"},
	} {
		t.Run(name, func(t *testing.T) {
			contents := readFixture(t, "token-grants.yaml")
			for _, edit := range testCase.edits {
				require.Contains(t, contents, edit.old)
				contents = strings.Replace(contents, edit.old, edit.replacement, 1)
			}
			_, err := schema.Decode(strings.NewReader(contents))
			require.ErrorContains(t, err, testCase.problem)
		})
	}
}

func TestOAuth2FieldsTakeOnlyLiteralsOrSettings(t *testing.T) {
	for name, testCase := range map[string]struct {
		edit    manifestEdit
		problem string
	}{
		"a template token endpoint": {manifestEdit{
			"          tokenEndpoint: https://login.example.com/token\n",
			"          tokenEndpoint: {template: \"https://login.example.com/{tenantId}/token\"}\n",
		}, "oauth2 auth requires connectionKind, endpoints, and scopes"},
		"a translated client authentication": {manifestEdit{
			"tokenEndpointAuthMethod: client_secret_basic",
			"tokenEndpointAuthMethod: {fromSetting: tenantId, values: {common: client_secret_basic}}",
		}, "oauth2 tokenEndpointAuthMethod must be a literal or {fromSetting}"},
	} {
		t.Run(name, func(t *testing.T) {
			contents := readFixture(t, "token-grants.yaml")
			require.Contains(t, contents, testCase.edit.old)
			_, err := schema.Decode(strings.NewReader(strings.Replace(contents, testCase.edit.old, testCase.edit.replacement, 1)))
			require.ErrorContains(t, err, testCase.problem)
		})
	}

	appOnly := map[string]schema.AuthMethod{}
	for _, method := range decodeFixture(t, "token-grants.yaml").Spec.Auth.Methods {
		appOnly[method.ID] = method
	}
	require.Equal(t, schema.ValueSource{Template: "https://login.example.com/{tenant_id}/token"},
		appOnly["app-only"].ClientCredentials.TokenEndpoint, "client credentials keep templates")
}

func TestRefreshableRequiresADeclaredGrant(t *testing.T) {
	contents := strings.Replace(readFixture(t, "google-oauth.yaml"), "  auth:\n", "  auth:\n    refreshable: true\n", 1)
	_, err := schema.Decode(strings.NewReader(contents))
	require.ErrorContains(t, err, "spec.auth.refreshable requires a method that maps a refresh token or declares clientCredentials or serviceAccount.jwtBearer")
}

func TestClientAuthenticationFromAnEnumSetting(t *testing.T) {
	manifest := decodeEditedFixture(t, "token-grants.yaml",
		manifestEdit{"      - {name: tenantId,", "      - {name: clientAuthentication, goName: ClientAuthentication, type: enum, enum: [client_secret_post, client_secret_basic], description: Token endpoint client authentication., required: false}\n      - {name: tenantId,"},
		manifestEdit{"tokenEndpointAuthMethod: client_secret_basic", "tokenEndpointAuthMethod: {fromSetting: clientAuthentication}"})
	oauth := manifest.Spec.Auth.Methods[0].OAuth2
	require.Equal(t, &schema.ValueSource{FromSetting: "clientAuthentication"}, oauth.TokenEndpointAuthMethod)
	encoded, err := json.Marshal(oauth.TokenEndpointAuthMethod)
	require.NoError(t, err)
	require.JSONEq(t, `{"fromSetting": "clientAuthentication"}`, string(encoded))
}
