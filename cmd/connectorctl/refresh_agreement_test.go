// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/internal/codegen"
)

func TestGenerateWritesChecksAndRemovesTheRefreshAgreementTest(t *testing.T) {
	directory := t.TempDir()
	fixture, err := os.ReadFile(filepath.Join("..", "..", "schema", "testdata", "token-grants.yaml"))
	require.NoError(t, err)
	manifestPath := filepath.Join(directory, "connector.yaml")
	require.NoError(t, os.WriteFile(manifestPath, fixture, 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(directory, "go"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(directory, "go", "go.mod"), []byte("module example.com/tokengrants\n\ngo 1.24\n"), 0o600))
	agreementPath := filepath.Join(directory, "go", codegen.RefreshAgreementTestOutputFile)
	transportHookPath := filepath.Join(directory, "go", codegen.RefreshAgreementTransportHookOutputFile)

	require.ErrorContains(t, generate([]string{"--check", manifestPath}), "generated connector is missing")
	require.NoError(t, generate([]string{manifestPath}))
	require.FileExists(t, agreementPath)
	require.FileExists(t, transportHookPath)
	require.NoError(t, generate([]string{"--check", manifestPath}))

	require.NoError(t, os.MkdirAll(filepath.Join(directory, "go", "testdata"), 0o755))
	samplesPath := filepath.Join(directory, "go", "testdata", codegen.RefreshAgreementSamplesFile)
	require.NoError(t, os.WriteFile(samplesPath, []byte(`{"*": {"response": {"instance_url": "https://acme.example.com"}}}`), 0o600))
	require.ErrorContains(t, generate([]string{"--check", manifestPath}), "generated file is stale", "samples are generation input")
	require.NoError(t, os.WriteFile(samplesPath, []byte(`{"*": {"responses": {}}}`), 0o600))
	require.ErrorContains(t, generate([]string{manifestPath}), `unknown field "responses"`)
	require.NoError(t, os.Remove(samplesPath))

	withoutGrants, err := os.ReadFile(filepath.Join("..", "..", "schema", "testdata", "google-oauth.yaml"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(manifestPath, withoutGrants, 0o600))
	require.NoError(t, generate([]string{manifestPath}))
	require.NoFileExists(t, agreementPath, "a manifest without token grants keeps no agreement test")
	require.NoFileExists(t, transportHookPath, "a manifest without token grants keeps no transport hook")
	require.NoError(t, os.WriteFile(agreementPath, []byte("package tokengrants_test\n"), 0o600))
	require.ErrorContains(t, generate([]string{"--check", manifestPath}), "credential refresh agreement test is stale")
}

func TestLanguageRefreshAgreementTimeoutRequiresConnectorSDKZeroEight(t *testing.T) {
	cases := []byte(`[{"name":"refresh","tokenExchangeTimeout":"8s","tokenExchangeTimeoutNanoseconds":8000000000}]`)
	for _, language := range []string{"python", "typescript"} {
		for _, version := range []string{"0.6.0", "0.7.0", "0.8.0"} {
			t.Run(language+"/"+version, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "package.json")
				contents := `{"dependencies":{"@superdurable/connectors-sdk":"` + version + `"}}`
				if language == "python" {
					contents = `[project]` + "\n" + `dependencies = ["dex-connectors-sdk==` + version + `"]`
				}
				require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
				generated, err := languageRefreshAgreementCases(cases, path, language)
				require.NoError(t, err)
				if version == "0.8.0" {
					require.Contains(t, string(generated), `"tokenExchangeTimeout":"8s"`)
				} else {
					require.NotContains(t, string(generated), "tokenExchangeTimeout")
				}
			})
		}
	}
}

func TestLanguageRefreshAgreementTimeoutFollowsThePythonSDKFloor(t *testing.T) {
	cases := []byte(`[{"name":"refresh","tokenExchangeTimeout":"8s","tokenExchangeTimeoutNanoseconds":8000000000}]`)
	for _, test := range []struct {
		requirement    string
		includeTimeout bool
	}{
		{requirement: `"dex-connectors-sdk>=0.7.0,<1"`, includeTimeout: false},
		{requirement: `"dex-connectors-sdk>=0.8.0,<1"`, includeTimeout: true},
		{requirement: `'dex-connectors-sdk>=0.9.1,<1'`, includeTimeout: true},
	} {
		t.Run(test.requirement, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "pyproject.toml")
			contents := "[project]\ndependencies = [" + test.requirement + `, "dex-python-sdk>=1.28.0"]` + "\n"
			require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
			generated, err := languageRefreshAgreementCases(cases, path, "python")
			require.NoError(t, err)
			require.Equal(t, test.includeTimeout, strings.Contains(string(generated), `"tokenExchangeTimeout":"8s"`))
		})
	}
}

func TestLanguageRefreshAgreementRejectsOtherPythonSDKRequirements(t *testing.T) {
	cases := []byte(`[{"name":"refresh"}]`)
	for _, dependencies := range []string{
		`"dex-connectors-sdk>=0.8.0"`,
		`"dex-connectors-sdk>=0.8.0,<2"`,
		`"dex-connectors-sdk>=0.8.0,<1.0"`,
		`"dex-connectors-sdk>=0.8.0, <1"`,
		`"dex-connectors-sdk~=0.8"`,
		`"dex-connectors-sdk>=1.0.0,<1"`,
		`"dex-connectors-sdk>=0.8.0rc1,<1"`,
		`"dex-connectors-sdk==0.8.0", "dex-connectors-sdk>=0.8.0,<1"`,
		`"dex-python-sdk>=1.28.0"`,
	} {
		t.Run(dependencies, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "pyproject.toml")
			require.NoError(t, os.WriteFile(path, []byte("[project]\ndependencies = ["+dependencies+"]\n"), 0o600))
			_, err := languageRefreshAgreementCases(cases, path, "python")
			require.ErrorContains(t, err, "must require dex-connectors-sdk once")
		})
	}
}

func TestGeneratedRefreshAgreementFilesIncludeLanguageTests(t *testing.T) {
	manifestPath := filepath.Join("..", "..", "connectors", "google", "gmail", "connector.yaml")
	manifest, err := load(manifestPath)
	require.NoError(t, err)
	files, err := generatedRefreshAgreementFiles(manifestPath, manifest)
	require.NoError(t, err)
	pythonTest := filepath.Join("..", "..", "connectors", "google", "gmail", "python", "tests", "test_refresh_agreement.py")
	typeScriptTest := filepath.Join("..", "..", "connectors", "google", "gmail", "typescript", "test", "refresh_agreement.test.ts")
	for _, path := range []string{pythonTest, typeScriptTest} {
		source, exists := files[path]
		require.True(t, exists, path)
		require.True(t, strings.Contains(string(source), "Code generated by connectorctl generate"))
		require.True(t, strings.Contains(string(source), "assertion"))
		require.True(t, strings.Contains(string(source), "clientAuth"))
	}
}
