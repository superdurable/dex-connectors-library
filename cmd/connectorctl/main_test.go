// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/schema"
	"gopkg.in/yaml.v3"
)

func TestCatalogLoadsRepositoryCatalogSource(t *testing.T) {
	catalog := filepath.Join("..", "..", "catalog.yaml")
	entries, err := loadConnectorDirectoryEntries(catalog)
	require.NoError(t, err)
	directories := registeredConnectorDirectories(t)
	require.Len(t, entries, len(directories))
	names := map[string]bool{}
	for index, entry := range entries {
		require.Equal(t, directories[index], entry.Directory)
		identity := readManifestIdentity(t, entry.Directory)
		require.Equal(t, identity.Name, entry.Manifest.Metadata.Name, entry.Directory)
		require.Equal(t, identity.Version, entry.Manifest.Metadata.Version, entry.Directory)
		require.Falsef(t, names[identity.Name], "duplicate connector ID %s", identity.Name)
		names[identity.Name] = true
	}
	require.Len(t, names, len(directories))
}

func TestRegisteredOperationsKeepOnlyHappyPathBranchesRequired(t *testing.T) {
	entries, err := loadConnectorDirectoryEntries(filepath.Join("..", "..", "catalog.yaml"))
	require.NoError(t, err)
	for _, entry := range entries {
		for _, operation := range entry.Manifest.Spec.Operations {
			required := []string{}
			for _, branch := range operation.Branches {
				if !branch.Optional {
					required = append(required, branch.ID)
				}
			}
			require.Lenf(t, required, 1, "%s %s", entry.Directory, operation.Name)
		}
	}
}

// Google reports an alias grant under its canonical URI, so a literal scope check never matches the alias.
func TestRegisteredGoogleConnectorsRequestNoAliasOAuthScopes(t *testing.T) {
	canonicalScopeByGoogleAlias := map[string]string{
		"email":   "https://www.googleapis.com/auth/userinfo.email",
		"profile": "https://www.googleapis.com/auth/userinfo.profile",
	}
	entries, err := loadConnectorDirectoryEntries(filepath.Join("..", "..", "catalog.yaml"))
	require.NoError(t, err)
	checkedDirectories := []string{}
	for _, entry := range entries {
		if entry.Manifest.Spec.Provider != "google" {
			continue
		}
		oauthConfigurations := []*schema.OAuth2{entry.Manifest.Spec.Auth.OAuth2}
		for _, method := range entry.Manifest.Spec.Auth.Methods {
			oauthConfigurations = append(oauthConfigurations, method.OAuth2)
		}
		checkedConnector := false
		for _, oauth := range oauthConfigurations {
			if oauth == nil {
				continue
			}
			checkedConnector = true
			for _, scope := range append(append([]string{}, oauth.Scopes...), oauth.UserScopes...) {
				canonicalScope, isGoogleAlias := canonicalScopeByGoogleAlias[scope]
				require.Falsef(t, isGoogleAlias, "%s requests Google alias scope %q; request %q instead", entry.Directory, scope, canonicalScope)
			}
		}
		if checkedConnector {
			checkedDirectories = append(checkedDirectories, entry.Directory)
		}
	}
	require.Equal(t, []string{"connectors/google/drive", "connectors/google/gmail", "connectors/google/spreadsheet"}, checkedDirectories)
}

func TestCatalogCommandWritesDeterministicYAML(t *testing.T) {
	catalog := filepath.Join("..", "..", "catalog.yaml")
	require.NoError(t, catalogCommand([]string{"--check", "--catalog", catalog}))
	first := filepath.Join(t.TempDir(), "catalog.yaml")
	second := filepath.Join(t.TempDir(), "catalog.yaml")
	require.NoError(t, catalogCommand([]string{"--catalog", catalog, "--output", first}))
	require.NoError(t, catalogCommand([]string{"--catalog", catalog, "--output", second}))
	firstContent, err := os.ReadFile(first)
	require.NoError(t, err)
	secondContent, err := os.ReadFile(second)
	require.NoError(t, err)
	require.Equal(t, firstContent, secondContent)
	var publicCatalog connectorCatalog
	require.NoError(t, yaml.Unmarshal(firstContent, &publicCatalog))
	require.Equal(t, connectorCatalogAPIVersion, publicCatalog.APIVersion)
	require.Equal(t, "ConnectorCatalog", publicCatalog.Kind)
	require.Len(t, publicCatalog.Connectors, len(registeredConnectorDirectories(t)))
	for index, directory := range registeredConnectorDirectories(t) {
		require.Equal(t, directory, publicCatalog.Connectors[index].Directory)
	}
}

func TestReleaseArtifactIsDeterministicAndVersioned(t *testing.T) {
	directory := t.TempDir()
	first := filepath.Join(directory, "connector-release.json")
	firstDigest := first + ".sha256"
	second := filepath.Join(directory, "connector-release-copy.json")
	secondDigest := second + ".sha256"
	manifest := filepath.Join("..", "..", "schema", "testdata", "google-oauth.yaml")
	arguments := func(output, digest string) []string {
		return []string{
			"--manifest", manifest,
			"--module-path", "example.com/connectors/google-fixture",
			"--version", "v0.1.0",
			"--tag", "connectors/google-fixture/v0.1.0",
			"--source-sha", strings.Repeat("a", 40),
			"--output", output,
			"--digest-output", digest,
		}
	}
	require.NoError(t, releaseArtifact(arguments(first, firstDigest)))
	require.NoError(t, releaseArtifact(arguments(second, secondDigest)))
	firstContent, err := os.ReadFile(first)
	require.NoError(t, err)
	secondContent, err := os.ReadFile(second)
	require.NoError(t, err)
	require.Equal(t, firstContent, secondContent)
	require.Contains(t, string(firstContent), `"version": "v0.1.0"`)
	require.Contains(t, string(firstContent), `"tag": "connectors/google-fixture/v0.1.0"`)
	for _, absentKey := range []string{`"selection"`, `"methodLabel"`, `"studioUnit"`} {
		require.NotContains(t, string(firstContent), absentKey, "manifests without the field publish no key")
	}
	digest, err := os.ReadFile(firstDigest)
	require.NoError(t, err)
	require.Equal(t, fmt.Sprintf("%x  connector-release.json\n", sha256.Sum256(firstContent)), string(digest))
	mismatchedArguments := arguments(filepath.Join(directory, "mismatch.json"), filepath.Join(directory, "mismatch.json.sha256"))
	mismatchedArguments[5] = "v0.1.1"
	require.ErrorContains(t, releaseArtifact(mismatchedArguments), "does not match manifest version")
}

func TestGeneratedConnectorsAreCurrent(t *testing.T) {
	for _, directory := range registeredConnectorDirectories(t) {
		path := filepath.Join("..", "..", filepath.FromSlash(directory), "connector.yaml")
		require.NoError(t, generate([]string{"--check", path}), directory)
	}
}

// registeredConnectorDirectories reads catalog.yaml directly, independent of the loader under test.
func registeredConnectorDirectories(t *testing.T) []string {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("..", "..", "catalog.yaml"))
	require.NoError(t, err)
	var catalogSource connectorCatalogSource
	require.NoError(t, yaml.Unmarshal(contents, &catalogSource))
	require.NotEmpty(t, catalogSource.Directories)
	return catalogSource.Directories
}

type manifestIdentity struct {
	Name    string
	Version string
}

// readManifestIdentity reads one registered manifest's ID and release version directly from YAML.
func readManifestIdentity(t *testing.T, directory string) manifestIdentity {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(directory), "connector.yaml"))
	require.NoError(t, err)
	var manifest struct {
		Metadata manifestIdentity `yaml:"metadata"`
	}
	require.NoError(t, yaml.Unmarshal(contents, &manifest))
	require.NotEmpty(t, manifest.Metadata.Name, directory)
	require.NotEmpty(t, manifest.Metadata.Version, directory)
	return manifest.Metadata
}

func TestStudioUIArtifactIsDeterministicAndIncludedInRelease(t *testing.T) {
	directory := t.TempDir()
	uiRoot := filepath.Join(directory, "dist")
	require.NoError(t, os.MkdirAll(uiRoot, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(uiRoot, "index.html"), []byte("<main>Sheets</main>"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(uiRoot, "icon.svg"), []byte("<svg xmlns=\"http://www.w3.org/2000/svg\"/>"), 0o600))
	fixture, err := os.ReadFile(filepath.Join("..", "..", "schema", "testdata", "google-oauth.yaml"))
	require.NoError(t, err)
	fixture = []byte(strings.Replace(string(fixture), "  operations:\n", `  studio:
    setup:
      entrypoint: index.html
      hostApiRange: ">=0.1.0 <0.2.0"
      backendCapabilities: [configuration.write, sheets.models-list]
      mockScenarios: [not-configured, ready]
      icon: icon.svg
    commands:
      - id: listModels
        capability: sheets.models-list
        request:
          method: GET
          url: https://models.example.com/v1/models
          credential: {field: access_token, scheme: header, header: x-goog-api-key}
          fixedHeaders: {x-api-version: "2023-06-01"}
  operations:
`, 1))
	manifest := filepath.Join(directory, "connector.yaml")
	require.NoError(t, os.WriteFile(manifest, fixture, 0o600))
	first := filepath.Join(directory, "connector-ui.tgz")
	firstDigest := first + ".sha256"
	second := filepath.Join(directory, "connector-ui-copy.tgz")
	secondDigest := second + ".sha256"
	require.NoError(t, uiArtifact([]string{"--manifest", manifest, "--ui-root", uiRoot, "--output", first, "--digest-output", firstDigest}))
	require.NoError(t, uiArtifact([]string{"--manifest", manifest, "--ui-root", uiRoot, "--output", second, "--digest-output", secondDigest}))
	firstBytes, err := os.ReadFile(first)
	require.NoError(t, err)
	secondBytes, err := os.ReadFile(second)
	require.NoError(t, err)
	require.Equal(t, firstBytes, secondBytes)

	release := filepath.Join(directory, "connector-release.json")
	releaseDigest := release + ".sha256"
	require.NoError(t, releaseArtifact([]string{
		"--manifest", manifest,
		"--module-path", "github.com/superdurable/dex-connectors-library/connectors/google-fixture",
		"--version", "v0.1.0", "--tag", "connectors/google-fixture/v0.1.0",
		"--source-sha", strings.Repeat("a", 40), "--ui-artifact", first, "--ui-digest", firstDigest,
		"--output", release, "--digest-output", releaseDigest,
	}))
	releaseBytes, err := os.ReadFile(release)
	require.NoError(t, err)
	require.Contains(t, string(releaseBytes), `"artifact": "connector-ui.tgz"`)
	require.Contains(t, string(releaseBytes), `"entrypoint": "index.html"`)
	var releaseWire struct {
		Manifest struct {
			Spec struct {
				Studio struct {
					Commands []struct {
						Request struct {
							Credential   map[string]string `json:"credential"`
							FixedHeaders map[string]string `json:"fixedHeaders"`
						} `json:"request"`
					} `json:"commands"`
				} `json:"studio"`
			} `json:"spec"`
		} `json:"manifest"`
	}
	require.NoError(t, json.Unmarshal(releaseBytes, &releaseWire))
	require.Len(t, releaseWire.Manifest.Spec.Studio.Commands, 1)
	commandRequest := releaseWire.Manifest.Spec.Studio.Commands[0].Request
	require.Equal(t, map[string]string{"field": "access_token", "scheme": "header", "header": "x-goog-api-key"}, commandRequest.Credential)
	require.Equal(t, map[string]string{"x-api-version": "2023-06-01"}, commandRequest.FixedHeaders)
}

func TestReleaseArtifactPublishesMultipleAuthSelectionManifestFields(t *testing.T) {
	directory := t.TempDir()
	uiRoot := filepath.Join(directory, "dist")
	require.NoError(t, os.MkdirAll(uiRoot, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(uiRoot, "index.html"), []byte("<main>Providers</main>"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(uiRoot, "icon.svg"), []byte("<svg xmlns=\"http://www.w3.org/2000/svg\"/>"), 0o600))
	manifest := filepath.Join("..", "..", "schema", "testdata", "multiple-auth-selection.yaml")
	uiTarball := filepath.Join(directory, "connector-ui.tgz")
	require.NoError(t, uiArtifact([]string{"--manifest", manifest, "--ui-root", uiRoot, "--output", uiTarball, "--digest-output", uiTarball + ".sha256"}))
	release := filepath.Join(directory, "connector-release.json")
	require.NoError(t, releaseArtifact([]string{
		"--manifest", manifest,
		"--module-path", "github.com/superdurable/dex-connectors-library/connectors/example/providers",
		"--version", "v0.1.0", "--tag", "connectors/example/providers/v0.1.0",
		"--source-sha", strings.Repeat("a", 40), "--ui-artifact", uiTarball, "--ui-digest", uiTarball + ".sha256",
		"--output", release, "--digest-output", release + ".sha256",
	}))
	releaseBytes, err := os.ReadFile(release)
	require.NoError(t, err)

	type wireField struct {
		Name       string            `json:"name"`
		Required   bool              `json:"required"`
		StudioUnit map[string]string `json:"studioUnit"`
	}
	var releaseWire struct {
		Manifest struct {
			Spec struct {
				Configuration struct {
					Fields []wireField `json:"fields"`
				} `json:"configuration"`
				Auth struct {
					Selection     string `json:"selection"`
					MethodLabel   string `json:"methodLabel"`
					DefaultMethod string `json:"defaultMethod"`
					Methods       []struct {
						ID            string `json:"id"`
						Configuration *struct {
							Fields []wireField `json:"fields"`
						} `json:"configuration"`
					} `json:"methods"`
				} `json:"auth"`
			} `json:"spec"`
		} `json:"manifest"`
	}
	require.NoError(t, json.Unmarshal(releaseBytes, &releaseWire))
	spec := releaseWire.Manifest.Spec
	require.Equal(t, "multiple", spec.Auth.Selection)
	require.Equal(t, "Provider", spec.Auth.MethodLabel)
	require.Equal(t, "openai", spec.Auth.DefaultMethod)
	require.Len(t, spec.Auth.Methods, 3)
	require.Equal(t, []wireField{{Name: "openaiProjectId", Required: true}}, spec.Auth.Methods[0].Configuration.Fields)
	require.Equal(t, []wireField{{Name: "anthropicWorkspaceId"}}, spec.Auth.Methods[1].Configuration.Fields)
	require.Equal(t, []wireField{{Name: "model", StudioUnit: map[string]string{"unit": "modelPicker", "port": "model"}}}, spec.Configuration.Fields)
}

func TestStudioUIArtifactRejectsActiveSVG(t *testing.T) {
	directory := t.TempDir()
	uiRoot := filepath.Join(directory, "dist")
	require.NoError(t, os.MkdirAll(uiRoot, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(uiRoot, "index.html"), []byte("<main>Fixture</main>"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(uiRoot, "icon.svg"), []byte(`<svg xmlns="http://www.w3.org/2000/svg"><image onerror = "alert(1)"/></svg>`), 0o600))
	fixture, err := os.ReadFile(filepath.Join("..", "..", "schema", "testdata", "google-oauth.yaml"))
	require.NoError(t, err)
	fixture = []byte(strings.Replace(string(fixture), "  operations:\n", `  studio:
    setup:
      entrypoint: index.html
      hostApiRange: ">=0.1.0 <0.2.0"
      backendCapabilities: [configuration.write]
      mockScenarios: [ready]
      icon: icon.svg
  operations:
`, 1))
	manifest := filepath.Join(directory, "connector.yaml")
	require.NoError(t, os.WriteFile(manifest, fixture, 0o600))
	err = uiArtifact([]string{
		"--manifest", manifest, "--ui-root", uiRoot,
		"--output", filepath.Join(directory, "ui.tgz"),
		"--digest-output", filepath.Join(directory, "ui.tgz.sha256"),
	})
	require.ErrorContains(t, err, "prohibited active content")
}

func TestGenerateCheckDetectsDrift(t *testing.T) {
	source := filepath.Join("..", "..", "connectors", "openai", "connector.yaml")
	content, err := os.ReadFile(source)
	require.NoError(t, err)
	directory := t.TempDir()
	manifest := filepath.Join(directory, "connector.yaml")
	require.NoError(t, os.WriteFile(manifest, content, 0o600))
	require.NoError(t, generate([]string{manifest}))
	require.NoError(t, generate([]string{"--check", manifest}))
	require.NoError(t, os.WriteFile(filepath.Join(directory, "zz_generated_connector.go"), []byte("stale"), 0o600))
	require.ErrorContains(t, generate([]string{"--check", manifest}), "stale")
}
