// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSendCapsFollowTheRequiredSDKRelease(t *testing.T) {
	for _, scenario := range []struct {
		name        string
		goModule    string
		hasSendCaps bool
	}{
		{name: "earlier release", goModule: "require " + connectorSDKModulePath + " v0.36.0\n"},
		{name: "first release with send caps", goModule: "require " + connectorSDKModulePath + " v0.37.0\n", hasSendCaps: true},
		{name: "later release", goModule: "require " + connectorSDKModulePath + " v0.38.2\n", hasSendCaps: true},
		{name: "prerelease", goModule: "require " + connectorSDKModulePath + " v0.37.0-rc.1\n"},
		{name: "pseudo-version", goModule: "require " + connectorSDKModulePath + " v0.37.1-0.20261114000000-abcdefabcdef\n"},
		{name: "replaced", goModule: "require " + connectorSDKModulePath + " v0.37.0\n\nreplace " + connectorSDKModulePath + " => ../../sdkgo\n"},
		{name: "no requirement", goModule: ""},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			directory := t.TempDir()
			require.NoError(t, os.Mkdir(filepath.Join(directory, "go"), 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(directory, "go", "go.mod"), []byte("module example.com/connector\n\ngo 1.24.0\n\n"+scenario.goModule), 0o600))
			hasSendCaps, err := hasRepeatOnUnknownSendCaps(filepath.Join(directory, "connector.yaml"))
			require.NoError(t, err)
			require.Equal(t, scenario.hasSendCaps, hasSendCaps)
			options, err := repeatOnUnknownGenerateOptions(filepath.Join(directory, "connector.yaml"))
			require.NoError(t, err)
			require.Equal(t, scenario.hasSendCaps, len(options) == 1)
		})
	}
	hasSendCaps, err := hasRepeatOnUnknownSendCaps(filepath.Join(t.TempDir(), "connector.yaml"))
	require.NoError(t, err)
	require.False(t, hasSendCaps, "a missing Go module keeps the older output")
}

func TestLanguageSendCapsFollowEachPackageSDKRequirement(t *testing.T) {
	for _, scenario := range []struct {
		name        string
		language    string
		contents    string
		hasSendCaps bool
	}{
		{name: "python earlier pin", language: "python", contents: pythonProject(`"dex-connectors-sdk==0.9.0"`)},
		{name: "python earlier floor", language: "python", contents: pythonProject(`"dex-connectors-sdk>=0.9.0,<1"`)},
		{name: "python first pin", language: "python", contents: pythonProject(`"dex-connectors-sdk==0.10.0"`), hasSendCaps: true},
		{name: "python first floor", language: "python", contents: pythonProject(`"dex-connectors-sdk>=0.10.0,<1"`), hasSendCaps: true},
		{name: "python later floor", language: "python", contents: pythonProject(`'dex-connectors-sdk>=0.12.3,<1'`), hasSendCaps: true},
		{name: "python open floor", language: "python", contents: pythonProject(`"dex-connectors-sdk>=0.10.0"`), hasSendCaps: true},
		{name: "python two-segment floor", language: "python", contents: pythonProject(`"dex-connectors-sdk >= 0.10, < 1"`), hasSendCaps: true},
		{name: "python compatible release", language: "python", contents: pythonProject(`"dex-connectors-sdk~=0.10.0"`), hasSendCaps: true},
		{name: "python multiple specifiers raise the floor", language: "python", contents: pythonProject(`"dex-connectors-sdk>=0.9.0,!=0.9.5,>=0.10.1,<1"`), hasSendCaps: true},
		{name: "python multiple specifiers keep an earlier floor", language: "python", contents: pythonProject(`"dex-connectors-sdk>=0.9.0,!=0.9.5,<0.11"`)},
		{name: "python exclusive floor at an earlier release", language: "python", contents: pythonProject(`"dex-connectors-sdk>0.9.9"`)},
		{name: "python upper bound only", language: "python", contents: pythonProject(`"dex-connectors-sdk<1"`)},
		{name: "python unpinned", language: "python", contents: pythonProject(`"dex-connectors-sdk"`)},
		{name: "python wildcard", language: "python", contents: pythonProject(`"dex-connectors-sdk==0.10.*"`)},
		{name: "python prerelease", language: "python", contents: pythonProject(`"dex-connectors-sdk>=0.10.0rc1"`)},
		{name: "python arbitrary equality", language: "python", contents: pythonProject(`"dex-connectors-sdk===0.10.0"`)},
		{name: "python direct reference", language: "python", contents: pythonProject(`"dex-connectors-sdk @ https://example.com/sdk.whl"`)},
		{name: "python extras", language: "python", contents: pythonProject(`"dex-connectors-sdk[otel]==0.10.0"`), hasSendCaps: true},
		{name: "python marker", language: "python", contents: pythonProject(`"dex-connectors-sdk==0.10.0; python_version >= '3.11'"`)},
		{name: "python normalized name", language: "python", contents: pythonProject(`"Dex_Connectors.SDK==0.10.0"`), hasSendCaps: true},
		{name: "python two requirements", language: "python", contents: pythonProject(`"dex-connectors-sdk==0.10.0", "dex-connectors-sdk>=0.10.0,<1"`)},
		{name: "python no requirement", language: "python", contents: pythonProject(`"httpx>=0.27"`)},
		{name: "python missing project table", language: "python", contents: `dependencies = ["dex-connectors-sdk==0.10.0"]`},
		{name: "python optional dependency only", language: "python", contents: pythonProject(`"dex-connectors-sdk>=0.9.0"`) +
			"\n[project.optional-dependencies]\ndev = [\"dex-connectors-sdk==0.10.0\"]\n"},
		{name: "python dependency group only", language: "python", contents: pythonProject(`"httpx>=0.27"`) +
			"\n[dependency-groups]\ndev = [\"dex-connectors-sdk==0.10.0\"]\n"},
		{name: "typescript earlier pin", language: "typescript", contents: `{"dependencies":{"@superdurable/connectors-sdk":"0.10.0"}}`},
		{name: "typescript development dependency", language: "typescript", contents: `{"dependencies":{"@superdurable/connectors-sdk":"0.10.0"},"devDependencies":{"@superdurable/connectors-sdk":"0.11.0"}}`},
		{name: "typescript peer dependency only", language: "typescript", contents: `{"peerDependencies":{"@superdurable/connectors-sdk":"0.11.0"}}`},
		{name: "typescript first pin", language: "typescript", contents: `{"dependencies":{"@superdurable/connectors-sdk":"0.11.0"}}`, hasSendCaps: true},
		{name: "typescript later pin", language: "typescript", contents: `{"dependencies":{"@superdurable/connectors-sdk":"1.0.0"}}`, hasSendCaps: true},
		{name: "typescript range", language: "typescript", contents: `{"dependencies":{"@superdurable/connectors-sdk":"^0.11.0"}}`},
		{name: "typescript prerelease", language: "typescript", contents: `{"dependencies":{"@superdurable/connectors-sdk":"0.11.0-rc.1"}}`},
		{name: "typescript no requirement", language: "typescript", contents: `{"dependencies":{}}`},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			directory := t.TempDir()
			packageFile := map[string]string{"python": "pyproject.toml", "typescript": "package.json"}[scenario.language]
			require.NoError(t, os.Mkdir(filepath.Join(directory, scenario.language), 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(directory, scenario.language, packageFile), []byte(scenario.contents), 0o600))
			gate := map[string]func(string) (bool, error){"python": hasPythonRepeatOnUnknownSendCaps, "typescript": hasTypeScriptRepeatOnUnknownSendCaps}[scenario.language]
			hasSendCaps, err := gate(filepath.Join(directory, "connector.yaml"))
			require.NoError(t, err)
			require.Equal(t, scenario.hasSendCaps, hasSendCaps)
		})
	}
	missing := filepath.Join(t.TempDir(), "connector.yaml")
	for _, gate := range []func(string) (bool, error){hasPythonRepeatOnUnknownSendCaps, hasTypeScriptRepeatOnUnknownSendCaps} {
		hasSendCaps, err := gate(missing)
		require.NoError(t, err)
		require.False(t, hasSendCaps, "a missing package keeps the older output")
	}
	directory := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(directory, "python"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(directory, "python", "pyproject.toml"), []byte("[project\n"), 0o600))
	_, err := hasPythonRepeatOnUnknownSendCaps(filepath.Join(directory, "connector.yaml"))
	require.ErrorContains(t, err, "parse", "malformed TOML is an error, not a silent gate")
}

func pythonProject(dependencies string) string {
	return "[project]\nname = \"example\"\ndependencies = [" + dependencies + "]\n"
}

// TestGeneratedRepeatTiersFollowEachLanguageGate regenerates a copy of Stripe, whose Python and TypeScript
// packages implement limited Mutations and the agent-exposed safe createRefund, under each SDK gate.
// Its Go module keeps an sdkgo before v0.37.0, so the Go gate stays off in every case.
func TestGeneratedRepeatTiersFollowEachLanguageGate(t *testing.T) {
	for _, scenario := range []struct {
		name                       string
		pythonSDK, typeScriptSDK   string
		hasPythonTiers, hasTSTiers bool
	}{
		{name: "earlier SDKs", pythonSDK: "0.9.0", typeScriptSDK: "0.10.0"},
		{name: "Python only", pythonSDK: "0.10.0", typeScriptSDK: "0.10.0", hasPythonTiers: true},
		{name: "TypeScript only", pythonSDK: "0.9.0", typeScriptSDK: "0.11.0", hasTSTiers: true},
		{name: "both languages", pythonSDK: "0.10.0", typeScriptSDK: "0.11.0", hasPythonTiers: true, hasTSTiers: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			connectorDirectory := copyStripeConnectorForGeneration(t)
			replaceOnePatternInFile(t, filepath.Join(connectorDirectory, "python", "pyproject.toml"), regexp.MustCompile(`dex-connectors-sdk==[0-9.]+`), "dex-connectors-sdk=="+scenario.pythonSDK)
			replaceOnePatternInFile(t, filepath.Join(connectorDirectory, "typescript", "package.json"), regexp.MustCompile(`"@superdurable/connectors-sdk": "[0-9.]+"`), `"@superdurable/connectors-sdk": "`+scenario.typeScriptSDK+`"`)
			manifestPath := filepath.Join(connectorDirectory, "connector.yaml")
			require.NoError(t, generate([]string{manifestPath}))
			require.NoError(t, generate([]string{"--check", manifestPath}))

			goSource := readTestFile(t, filepath.Join(connectorDirectory, "go", "zz_generated_connector.go"))
			require.NotContains(t, goSource, "RepeatOnUnknown", "a language package's SDK never enables Go tiers")

			pythonPackage := filepath.Join(connectorDirectory, "python", "src", "dex_connectors", "stripe")
			pythonDefinitions := readTestFile(t, filepath.Join(pythonPackage, "_generated", "definitions.py"))
			pythonSteps := readTestFile(t, filepath.Join(pythonPackage, "_generated", "steps.py"))
			typeScriptDefinitions := readTestFile(t, filepath.Join(connectorDirectory, "typescript", "src", "generated", "definitions.ts"))
			if scenario.hasPythonTiers {
				require.Contains(t, pythonDefinitions, "    RepeatOnUnknown,\n")
				require.Contains(t, pythonDefinitions, "    ),\n    repeat_on_unknown=RepeatOnUnknown.LIMITED,\n    max_sends=3,\n)\n")
				require.Contains(t, pythonDefinitions, "    ),\n    repeat_on_unknown=RepeatOnUnknown.SAFE,\n)\n")
				require.Equal(t, 2, strings.Count(pythonSteps, "    max_sends_override: int | None = None,\n"), "only the two limited Mutations take a send cap")
				require.Equal(t, 2, strings.Count(pythonSteps, "            max_sends_override=max_sends_override,\n"))
			} else {
				require.NotContains(t, pythonDefinitions, "RepeatOnUnknown")
				require.NotContains(t, pythonDefinitions, "max_sends")
				require.NotContains(t, pythonSteps, "max_sends")
			}
			if scenario.hasTSTiers {
				require.Contains(t, typeScriptDefinitions, "  repeatOnUnknown: \"limited\",\n  maxSends: 3,\n};\n")
				require.Contains(t, typeScriptDefinitions, "  repeatOnUnknown: \"safe\",\n};\n")
			} else {
				require.NotContains(t, typeScriptDefinitions, "repeatOnUnknown")
				require.NotContains(t, typeScriptDefinitions, "maxSends")
			}

			pythonRefund := generatedFactory(t, filepath.Join(pythonPackage, "dex-connector.json"), "createRefund")
			typeScriptRefund := generatedFactory(t, filepath.Join(connectorDirectory, "typescript", "dex-connector.json"), "createRefund")
			for _, published := range []struct {
				agent          any
				hasPackageTier bool
			}{{pythonRefund["agent"], scenario.hasPythonTiers}, {typeScriptRefund["agent"], scenario.hasTSTiers}} {
				agent := published.agent.(map[string]any)
				require.Equal(t, "safe", agent["retry"])
				if published.hasPackageTier {
					require.Equal(t, "safe", agent["repeatOnUnknown"])
				} else {
					require.NotContains(t, agent, "repeatOnUnknown", "a package advertises a tier only past its own SDK gate")
				}
				require.NotContains(t, agent, "maxSends")
			}
		})
	}
}

func TestGeneratedConformanceRecorderAcceptsPriorSendCountFromSDKGoV037(t *testing.T) {
	for _, scenario := range []struct {
		goSDK            string
		hasSendCountKeys bool
	}{{goSDK: "v0.36.0"}, {goSDK: "v0.37.0", hasSendCountKeys: true}} {
		t.Run(scenario.goSDK, func(t *testing.T) {
			repository := t.TempDir()
			connectorDirectory := filepath.Join(repository, "connectors", "example")
			for path, contents := range map[string]string{
				filepath.Join(repository, "catalog.yaml"):                                           "directories: []\n",
				filepath.Join(repository, "conformance", "connectors", "example", "scenarios.json"): "{}\n",
				filepath.Join(connectorDirectory, "go", "go.mod"):                                   "module example.com/example\n\ngo 1.24.0\n\nrequire " + connectorSDKModulePath + " " + scenario.goSDK + "\n",
				filepath.Join(connectorDirectory, "go", "conformance_adapter_test.go"):              "package example\n",
			} {
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
				require.NoError(t, os.WriteFile(path, []byte(contents), 0o644))
			}
			files, err := generatedConformanceTestFiles(filepath.Join(connectorDirectory, "connector.yaml"))
			require.NoError(t, err)
			source := string(files[filepath.Join(connectorDirectory, "go", "zz_generated_conformance_test.go")])
			require.NotContains(t, source, "{{PRIOR_HEARTBEAT")
			require.Equal(t, scenario.hasSendCountKeys, strings.Contains(source, `[]string{"value", "callId", "state", "providerId", "sendCount"}`))
			require.Equal(t, scenario.hasSendCountKeys, strings.Contains(source, `[]string{"providerId", "sendCount"}`))
			require.Equal(t, scenario.hasSendCountKeys, strings.Contains(source, `errors.New("invalid prior heartbeat send count")`))
		})
	}
}

// copyStripeConnectorForGeneration copies the generator inputs of the catalog Stripe connector into a new catalog.
func copyStripeConnectorForGeneration(t *testing.T) string {
	t.Helper()
	sourceRepository := filepath.Join("..", "..")
	repository := t.TempDir()
	connectorDirectory := filepath.Join(repository, "connectors", "stripe")
	sourceDirectory := filepath.Join(sourceRepository, "connectors", "stripe")
	require.NoError(t, filepath.WalkDir(sourceDirectory, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && (entry.Name() == "node_modules" || entry.Name() == ".venv" || entry.Name() == "__pycache__") {
			return filepath.SkipDir
		}
		if !entry.Type().IsRegular() && !entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(sourceDirectory, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(connectorDirectory, relative)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o755)
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(destination, contents, 0o644)
	}))
	for _, shared := range []string{filepath.Join("conformance", "connectors", "stripe", "scenarios.json"), filepath.Join("conformance", "runners", "matcher_cases.json")} {
		contents, err := os.ReadFile(filepath.Join(sourceRepository, shared))
		require.NoError(t, err)
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(repository, shared)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(repository, shared), contents, 0o644))
	}
	require.NoError(t, os.WriteFile(filepath.Join(repository, "catalog.yaml"), []byte("directories:\n  - connectors/stripe\n"), 0o644))
	return connectorDirectory
}

func replaceOnePatternInFile(t *testing.T, path string, pattern *regexp.Regexp, replacement string) {
	t.Helper()
	contents := readTestFile(t, path)
	require.Equal(t, 1, len(pattern.FindAllString(contents, -1)), "%s must match %s once", path, pattern)
	require.NoError(t, os.WriteFile(path, []byte(pattern.ReplaceAllLiteralString(contents, replacement)), 0o644))
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(contents)
}
