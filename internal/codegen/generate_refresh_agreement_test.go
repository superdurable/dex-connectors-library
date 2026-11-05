// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package codegen_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/internal/codegen"
	"github.com/superdurable/dex-connectors-library/schema"
)

func TestTypeScriptRefreshAgreementCompilesWithExactOptionalProperties(t *testing.T) {
	if os.Getenv("TS_CODEGEN_COMPILE") != "1" {
		t.Skip("set TS_CODEGEN_COMPILE=1 after npm ci in connectors/google/gmail/typescript")
	}
	packageDirectory, err := filepath.Abs(filepath.Join("..", "..", "connectors", "google", "gmail", "typescript"))
	require.NoError(t, err)
	generatedTest, err := os.ReadFile(filepath.Join(packageDirectory, "test", "refresh_agreement.test.ts"))
	require.NoError(t, err)
	require.True(t, bytes.Equal(codegen.GenerateTypeScriptRefreshAgreementTest(), generatedTest), "Gmail refresh agreement test differs from generated output")
	compiler := filepath.Join(packageDirectory, "node_modules", ".bin", "tsc")
	configuration, err := json.Marshal(map[string]any{
		"extends":         filepath.Join(packageDirectory, "tsconfig.json"),
		"files":           []string{filepath.Join(packageDirectory, "test", "refresh_agreement.test.ts")},
		"include":         []string{},
		"compilerOptions": map[string]any{"exactOptionalPropertyTypes": true},
	})
	require.NoError(t, err)
	configurationDirectory, err := os.MkdirTemp(packageDirectory, ".refresh-agreement-compile-")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(configurationDirectory)) })
	configurationPath := filepath.Join(configurationDirectory, "tsconfig.json")
	require.NoError(t, os.WriteFile(configurationPath, configuration, 0o644))
	command := exec.Command(compiler, "--noEmit", "-p", configurationPath)
	output, err := command.CombinedOutput()
	require.NoError(t, err, "generated TypeScript refresh agreement does not typecheck: %s", output)
}

func TestGenerateRefreshAgreementCasesReusesGoSamples(t *testing.T) {
	manifestFile, err := os.Open(filepath.Join("..", "..", "connectors", "google", "gmail", "connector.yaml"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, manifestFile.Close()) })
	manifest, err := schema.Decode(manifestFile)
	require.NoError(t, err)
	encoded, err := codegen.GenerateRefreshAgreementCases(manifest, nil)
	require.NoError(t, err)
	var cases []codegen.RefreshAgreementCase
	require.NoError(t, json.Unmarshal(encoded, &cases))
	require.Len(t, cases, 19)
	require.Equal(t, "google-oauth refresh", cases[0].Name)
	require.Equal(t, "https://oauth2.googleapis.com/token", cases[0].Endpoint)
	require.Equal(t, "manifest-client-id", cases[0].Parameters["client_id"])
	require.Equal(t, "manifest-client-secret", cases[0].Parameters["client_secret"])
	require.Equal(t, "manifest-refresh-token-2", cases[0].Updates["refresh_token"])
	require.Equal(t, "retryable", cases[1].ExpectedError)
	require.Equal(t, "reauthorization", cases[3].ExpectedError)
	require.Equal(t, "workspace-domain-delegation jwtBearer", cases[10].Name)
	require.Equal(t, "private_key", cases[10].PrivateKeyMember)
	require.Equal(t, "manifest-issuer@example.com", cases[10].Assertion.Issuer)
	require.Equal(t, int64(3600), cases[10].Assertion.LifetimeSeconds)
}

func TestGenerateRefreshAgreementCasesEncodeTerminalCodesAsArrays(t *testing.T) {
	manifestFile, err := os.Open(filepath.Join("..", "..", "connectors", "atlassian", "jira", "connector.yaml"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, manifestFile.Close()) })
	manifest, err := schema.Decode(manifestFile)
	require.NoError(t, err)
	encoded, err := codegen.GenerateRefreshAgreementCases(manifest, nil)
	require.NoError(t, err)
	var cases []map[string]any
	require.NoError(t, json.Unmarshal(encoded, &cases))
	require.NotEmpty(t, cases)
	for _, agreementCase := range cases {
		require.IsType(t, []any{}, agreementCase["terminalErrorCodes"], agreementCase["name"])
	}
	require.Contains(t, string(codegen.GeneratePythonRefreshAgreementTest("atlassian_jira")),
		`terminal_code in case["terminalErrorCodes"]`)
	require.Contains(t, string(codegen.GenerateTypeScriptRefreshAgreementTest()),
		`agreement.terminalErrorCodes?.includes(terminalCode)`)
}

func TestMicrosoftTemplateRulesAgreeOnValidTenantGUID(t *testing.T) {
	for _, connector := range []string{"entra-id", "onedrive", "outlook-calendar", "outlook-mail"} {
		t.Run(connector, func(t *testing.T) {
			manifestFile, err := os.Open(filepath.Join("..", "..", "connectors", "microsoft", connector, "connector.yaml"))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, manifestFile.Close()) })
			manifest, err := schema.Decode(manifestFile)
			require.NoError(t, err)
			encoded, err := codegen.GenerateRefreshAgreementCases(manifest, nil)
			require.NoError(t, err)
			var cases []codegen.RefreshAgreementCase
			require.NoError(t, json.Unmarshal(encoded, &cases))
			found := map[string]bool{}
			placeholder := "tenant_id"
			if connector == "outlook-calendar" {
				placeholder = "tenantId"
			}
			for _, agreementCase := range cases {
				if strings.Contains(agreementCase.Name, "01234567-89ab-cdef-0123-456789abcdef") {
					require.Contains(t, agreementCase.Endpoint, "01234567-89ab-cdef-0123-456789abcdef")
					found["guid"] = true
				}
				if strings.Contains(agreementCase.Name, "Manifest.Example.com") {
					if connector == "entra-id" || connector == "onedrive" {
						require.Contains(t, agreementCase.Endpoint, "manifest.example.com")
					} else {
						require.Contains(t, agreementCase.Endpoint, "Manifest.Example.com")
					}
					found["mixed"] = true
				}
				if strings.Contains(agreementCase.Name, "with oversized ") {
					require.Equal(t, "reauthorization", agreementCase.ExpectedError)
					value := agreementCase.Credentials[placeholder]
					if connector == "outlook-calendar" {
						value = agreementCase.Settings[placeholder]
					}
					require.Len(t, []byte(value), 254)
					found["oversized"] = true
				}
				if strings.Contains(agreementCase.Name, "with non-ASCII ") {
					require.Equal(t, "reauthorization", agreementCase.ExpectedError)
					value := agreementCase.Credentials[placeholder]
					if connector == "outlook-calendar" {
						value = agreementCase.Settings[placeholder]
					}
					require.Contains(t, value, "é")
					found["nonASCII"] = true
				}
				if strings.Contains(agreementCase.Name, "with rejected "+placeholder+" common") {
					require.Equal(t, "reauthorization", agreementCase.ExpectedError)
					found["alias"] = true
				}
			}
			require.Equal(t, map[string]bool{"guid": true, "mixed": true, "oversized": true, "nonASCII": true, "alias": true}, found)
			contents, err := os.ReadFile(filepath.Join("..", "..", "connectors", "microsoft", connector, "connector.yaml"))
			require.NoError(t, err)
			broken := strings.Replace(string(contents), "-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12})$", "-[0-9A-Fa-f]{12})$", 1)
			require.NotEqual(t, string(contents), broken)
			brokenManifest, err := schema.Decode(strings.NewReader(broken))
			require.NoError(t, err)
			_, err = codegen.GenerateRefreshAgreementCases(brokenManifest, nil)
			require.ErrorContains(t, err, "rejects valid GUID sample")
		})
	}
}

func TestZohoBlankStoredAPIDomainRequiresRefresh(t *testing.T) {
	manifestFile, err := os.Open(filepath.Join("..", "..", "connectors", "zoho", "crm", "connector.yaml"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, manifestFile.Close()) })
	manifest, err := schema.Decode(manifestFile)
	require.NoError(t, err)
	encoded, err := codegen.GenerateRefreshAgreementCases(manifest, nil)
	require.NoError(t, err)
	var cases []codegen.RefreshAgreementCase
	require.NoError(t, json.Unmarshal(encoded, &cases))
	foundBlank := false
	for _, agreementCase := range cases {
		if !strings.Contains(agreementCase.Name, "without provider or stored URL") {
			continue
		}
		foundBlank = true
		require.Empty(t, agreementCase.Credentials["api_domain"])
		for _, refreshCase := range agreementCase.RefreshRequiredCases {
			if refreshCase.Name == "fresh token" {
				require.True(t, refreshCase.Expected, agreementCase.Name)
			}
			require.NotEqual(t, "blank mapped credential", refreshCase.Name, agreementCase.Name)
		}
	}
	require.True(t, foundBlank)
}

func TestHubSpotCommaInsideArrayScopeRequiresAtomicMatching(t *testing.T) {
	manifestFile, err := os.Open(filepath.Join("..", "..", "connectors", "hubspot", "connector.yaml"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, manifestFile.Close()) })
	manifest, err := schema.Decode(manifestFile)
	require.NoError(t, err)
	encoded, err := codegen.GenerateRefreshAgreementCases(manifest, nil)
	require.NoError(t, err)
	var cases []codegen.RefreshAgreementCase
	require.NoError(t, json.Unmarshal(encoded, &cases))
	var requiredScopes []string
	for _, agreementCase := range cases {
		if agreementCase.Name == "hubspot-oauth refresh" {
			responseScopes, isScopeArray := agreementCase.Response["scopes"].([]any)
			require.True(t, isScopeArray)
			for _, responseScope := range responseScopes {
				requiredScopes = append(requiredScopes, responseScope.(string))
			}
		}
	}
	require.Greater(t, len(requiredScopes), 1)
	for _, agreementCase := range cases {
		if agreementCase.Name != "hubspot-oauth refresh with comma inside one array scope" {
			continue
		}
		require.Equal(t, "reauthorization", agreementCase.ExpectedError)
		require.Equal(t, []any{strings.Join(requiredScopes, ",")}, agreementCase.Response["scopes"])
		return
	}
	t.Fatal("HubSpot atomic array scope variation is missing")
}

func TestGenerateRefreshAgreementCasesCoverAlternativeScopes(t *testing.T) {
	for _, connector := range []string{"google/youtube", "salesforce"} {
		manifestFile, err := os.Open(filepath.Join("..", "..", "connectors", connector, "connector.yaml"))
		require.NoError(t, err)
		manifest, err := schema.Decode(manifestFile)
		require.NoError(t, err)
		require.NoError(t, manifestFile.Close())
		encoded, err := codegen.GenerateRefreshAgreementCases(manifest, nil)
		require.NoError(t, err)
		var cases []codegen.RefreshAgreementCase
		require.NoError(t, json.Unmarshal(encoded, &cases))
		var acceptedAlternatives int
		for _, agreementCase := range cases {
			if strings.Contains(agreementCase.Name, "accepted scope set") {
				require.Empty(t, agreementCase.ExpectedError)
				acceptedAlternatives++
			}
		}
		require.Positive(t, acceptedAlternatives, connector)
	}
}

func TestHelpScoutRejectsMissingResponseExpiryAsRetryable(t *testing.T) {
	manifestFile, err := os.Open(filepath.Join("..", "..", "connectors", "helpscout", "connector.yaml"))
	require.NoError(t, err)
	manifest, err := schema.Decode(manifestFile)
	require.NoError(t, err)
	require.NoError(t, manifestFile.Close())
	encoded, err := codegen.GenerateRefreshAgreementCases(manifest, nil)
	require.NoError(t, err)
	var cases []codegen.RefreshAgreementCase
	require.NoError(t, json.Unmarshal(encoded, &cases))
	for _, agreementCase := range cases {
		if strings.Contains(agreementCase.Name, "without expires_in") {
			require.Equal(t, "retryable", agreementCase.ExpectedError)
			return
		}
	}
	t.Fatal("Help Scout missing expires_in case was not generated")
}

func TestGenerateRefreshAgreementCasesCoverGrantResponses(t *testing.T) {
	manifestFile, err := os.Open(filepath.Join("..", "..", "connectors", "slack", "connector.yaml"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, manifestFile.Close()) })
	manifest, err := schema.Decode(manifestFile)
	require.NoError(t, err)
	encoded, err := codegen.GenerateRefreshAgreementCases(manifest, nil)
	require.NoError(t, err)
	var cases []codegen.RefreshAgreementCase
	require.NoError(t, json.Unmarshal(encoded, &cases))
	require.NotEmpty(t, cases)
	require.Equal(t, "bot", cases[0].Response["token_type"])
	require.Equal(t, "client_secret_basic", cases[0].ClientAuth)
	for _, agreementCase := range cases {
		if strings.Contains(agreementCase.Name, "different reported scope") {
			require.Equal(t, "reauthorization", agreementCase.ExpectedError)
			require.Equal(t, "manifest-ungranted-scope", agreementCase.Response["scope"])
		}
	}
}

func TestReportedScopeVariationFollowsGoPolicy(t *testing.T) {
	for _, connector := range []struct {
		path          string
		expectedError string
	}{{"calendly", ""}, {"google/gmail", "reauthorization"}} {
		manifestFile, err := os.Open(filepath.Join("..", "..", "connectors", connector.path, "connector.yaml"))
		require.NoError(t, err)
		manifest, err := schema.Decode(manifestFile)
		require.NoError(t, err)
		require.NoError(t, manifestFile.Close())
		encoded, err := codegen.GenerateRefreshAgreementCases(manifest, nil)
		require.NoError(t, err)
		var cases []codegen.RefreshAgreementCase
		require.NoError(t, json.Unmarshal(encoded, &cases))
		found := false
		for _, agreementCase := range cases {
			if strings.Contains(agreementCase.Name, "different reported scope") {
				require.Equal(t, connector.expectedError, agreementCase.ExpectedError)
				found = true
			}
		}
		require.True(t, found, connector.path)
	}
}

func TestLanguageAgreementCoversExpiryAndRefreshTokenRetention(t *testing.T) {
	manifestFile, err := os.Open(filepath.Join("..", "..", "schema", "testdata", "token-grants.yaml"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, manifestFile.Close()) })
	manifest, err := schema.Decode(manifestFile)
	require.NoError(t, err)
	encoded, err := codegen.GenerateRefreshAgreementCases(manifest, nil)
	require.NoError(t, err)
	var cases []codegen.RefreshAgreementCase
	require.NoError(t, json.Unmarshal(encoded, &cases))
	foundLifetime, foundKeep, foundRetainedToken := false, false, false
	for _, agreementCase := range cases {
		if strings.Contains(agreementCase.Name, "without expires_in") && agreementCase.ExpectedExpiryMilliseconds != nil {
			switch *agreementCase.ExpectedExpiryMilliseconds {
			case 7_200_000:
				foundLifetime = true
			case 0:
				foundKeep = true
			default:
				t.Fatalf("unexpected expiry policy for %s", agreementCase.Name)
			}
		}
		if strings.Contains(agreementCase.Name, "without refresh_token") {
			require.NotContains(t, agreementCase.Updates, "refresh_token")
			foundRetainedToken = true
		}
	}
	require.True(t, foundLifetime)
	require.True(t, foundKeep)
	require.True(t, foundRetainedToken)
}

const refreshAgreementGoldenPath = "testdata/tokengrants/" + codegen.RefreshAgreementTestOutputFile

func TestGenerateRefreshAgreementTestMatchesGolden(t *testing.T) {
	generated := generateTokenGrantsAgreement(t, nil)
	if *shouldUpdateGoldenFiles {
		require.NoError(t, os.MkdirAll(filepath.Dir(refreshAgreementGoldenPath), 0o755))
		require.NoError(t, os.WriteFile(refreshAgreementGoldenPath, generated, 0o644))
	}
	golden, err := os.ReadFile(refreshAgreementGoldenPath)
	require.NoError(t, err)
	require.Equal(t, string(golden), string(generated), "run go test ./internal/codegen -run MatchesGolden -update-golden")
}

func TestGenerateRefreshAgreementTestDeclaresEveryGrant(t *testing.T) {
	text := string(generateTokenGrantsAgreement(t, nil))
	for _, expected := range []string{
		`name:     "user-oauth refresh",`,
		`clientAuth: "client_secret_basic",`,
		`encoding:   "json",`,
		`"refresh_token": "manifest-refresh-token", "scope": "offline_access files.read"}`,
		`name:     "bot-oauth refresh user_refresh_token",`,
		`"scope": "history.read"`,
		`name:     "app-only clientCredentials",`,
		`endpoint:   "https://login.example.com/manifest.example.com/token",`,
		`name:     "delegation jwtBearer",`,
		`ServiceAccountKey: sdkgo.NewSecretString(manifestJSONKey(privateKeyPEM, "private_key", map[string]string{"client_email": "manifest-issuer@example.com", "token_uri": "https://manifest.example.com/token"})),`,
		`"primary_email": "manifest-user@example.com"`,
		`name:     "jwt-bearer jwtBearer with login_environment \"sandbox\"",`,
		`audience: "https://test.example.com"`,
		`missingExpiry:      time.Duration(7200000000000),`,
	} {
		require.Contains(t, text, expected)
	}
	require.NotContains(t, text, `"scope": "offline_access files.read", "token_type"`, "a response does not report offline_access")
}

func TestGenerateRefreshAgreementTestReplaysResponseVariations(t *testing.T) {
	generated := string(generateTokenGrantsAgreement(t, nil))
	for _, suffix := range []string{
		"with unaccepted token type", "without token type", "with different reported scope",
		"without expires_in",
	} {
		require.Contains(t, generated, `name: " `+suffix+`"`)
	}
	require.Contains(t, generated, "requireManifestResponseVariations(t, grant, privateKeyPEM)")
	githubFile, err := os.Open("../../connectors/github/connector.yaml")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, githubFile.Close()) })
	githubManifest, err := schema.Decode(githubFile)
	require.NoError(t, err)
	githubGenerated, err := codegen.GenerateRefreshAgreementTest(githubManifest, "example.com/github", nil)
	require.NoError(t, err)
	require.Contains(t, string(githubGenerated), `name: " with extra scopes"`)
}

func TestGenerateRefreshAgreementTestAppliesSamplesFromGeneralToSpecific(t *testing.T) {
	text := string(generateTokenGrantsAgreement(t, map[string]codegen.RefreshAgreementSample{
		"*":                                    {Response: map[string]string{"instance_url": "https://general.example.com"}},
		"user-oauth":                           {Response: map[string]string{"instance_url": "https://method.example.com"}},
		"user-oauth refresh":                   {Credentials: map[string]string{"instance_url": "https://stored.example.com"}},
		"bot-oauth refresh user_refresh_token": {Response: map[string]string{"token_type": "user"}},
	}))
	require.Contains(t, text, `"instance_url": "https://method.example.com"`, "a method sample overrides the general one")
	require.Contains(t, text, `"instance_url": "https://general.example.com"`, "the general sample applies to the other methods")
	require.Contains(t, text, `"https://stored.example.com"`)
	require.Contains(t, text, `"token_type": "user"`)
}

func TestGenerateRefreshAgreementTestCoversEveryClientAuthenticationOfASetting(t *testing.T) {
	fixture, err := os.ReadFile("../../schema/testdata/token-grants.yaml")
	require.NoError(t, err)
	contents := strings.Replace(string(fixture), "      - {name: tenantId,", "      - {name: clientAuthentication, goName: ClientAuthentication, type: enum, enum: [client_secret_post, client_secret_basic], description: Client authentication., required: false}\n      - {name: tenantId,", 1)
	contents = strings.Replace(contents, "tokenEndpointAuthMethod: client_secret_basic", "tokenEndpointAuthMethod: {fromSetting: clientAuthentication}", 1)
	manifest, err := schema.Decode(strings.NewReader(contents))
	require.NoError(t, err)
	generated, err := codegen.GenerateRefreshAgreementTest(manifest, "example.com/tokengrants", nil)
	require.NoError(t, err)
	text := string(generated)
	for _, expected := range []string{
		"return newCredentialRefreshDriverWithSettings(httpClient, settings)",
		`name:     "user-oauth refresh with clientAuthentication \"client_secret_basic\"",`,
		`name:     "user-oauth refresh with clientAuthentication \"client_secret_post\"",`,
		`"clientAuthentication": "client_secret_basic"},`,
		`clientAuth: "client_secret_post",`,
	} {
		require.Contains(t, text, expected)
	}
}

func generateTokenGrantsAgreement(t *testing.T, samples map[string]codegen.RefreshAgreementSample) []byte {
	t.Helper()
	file, err := os.Open("../../schema/testdata/token-grants.yaml")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })
	manifest, err := schema.Decode(file)
	require.NoError(t, err)
	require.True(t, codegen.HasDeclaredTokenGrants(manifest))
	generated, err := codegen.GenerateRefreshAgreementTest(manifest, "example.com/tokengrants", samples)
	require.NoError(t, err)
	return generated
}
