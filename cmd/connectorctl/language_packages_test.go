// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLanguageCapabilitiesCommandUsesValidatedManifest(t *testing.T) {
	var output bytes.Buffer
	manifestPath := filepath.Join("..", "..", "connectors", "newsapi", "connector.yaml")
	require.NoError(t, languageCapabilitiesCommand(manifestPath, "python", &output))
	require.JSONEq(t, `{"operations":["searchArticles"],"triggers":[]}`, output.String())
	output.Reset()
	require.ErrorContains(t, languageCapabilitiesCommand(manifestPath, "ruby", &output), "unsupported language")
}

func TestValidateLanguagePackageFilesAndVersions(t *testing.T) {
	repositoryRoot := filepath.Join(t.TempDir(), "connectors", "parent")
	require.NoError(t, os.MkdirAll(repositoryRoot, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(repositoryRoot, "catalog.yaml"), []byte("connectors: []\n"), 0o600))
	connectorDirectory := filepath.Join(repositoryRoot, "connectors", "google", "gmail")
	require.NoError(t, os.MkdirAll(filepath.Join(connectorDirectory, "python"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(connectorDirectory, "typescript"), 0o755))
	manifestPath := filepath.Join(connectorDirectory, "connector.yaml")
	manifest := strings.Replace(readFixtureManifest(t), "spec:\n", "spec:\n  languages:\n    python: {version: v0.2.0, operations: [appendRows], triggers: []}\n    typescript: {version: v0.3.0, operations: [appendRows], triggers: []}\n", 1)
	require.NoError(t, os.WriteFile(manifestPath, []byte(manifest), 0o600))
	pyprojectPath := filepath.Join(connectorDirectory, "python", "pyproject.toml")
	packagePath := filepath.Join(connectorDirectory, "typescript", "package.json")
	require.NoError(t, os.WriteFile(pyprojectPath, []byte("[project]\nname = \"dex-connector-google-gmail\"\nversion = \"0.2.0\"\n"), 0o600))
	require.NoError(t, os.WriteFile(packagePath, []byte(`{"name":"@superdurable/connector-google-gmail","version":"0.3.0"}`), 0o600))
	_, err := load(manifestPath)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(packagePath, []byte(`{"name":"@superdurable/connector-google-gmail","version":"0.3.0","files":["dist"]}`), 0o600))
	_, err = load(manifestPath)
	require.ErrorContains(t, err, "files must include dex-connector.json")
	require.NoError(t, os.WriteFile(packagePath, []byte(`{"name":"@superdurable/connector-google-gmail","version":"0.3.0","files":["dist","dex-connector.json"]}`), 0o600))
	_, err = load(manifestPath)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(packagePath, []byte(`{"name":"@superdurable/connector-google-gmail","version":"0.3.0","exports":{".":"./dist/index.js"}}`), 0o600))
	_, err = load(manifestPath)
	require.ErrorContains(t, err, `exports must map "./dex-connector.json"`)
	require.NoError(t, os.WriteFile(packagePath, []byte(`{"name":"@superdurable/connector-google-gmail","version":"0.3.0","exports":{".":"./dist/index.js","./dex-connector.json":"./dist/dex-connector.json"}}`), 0o600))
	_, err = load(manifestPath)
	require.ErrorContains(t, err, `exports must map "./dex-connector.json"`)
	require.NoError(t, os.WriteFile(packagePath, []byte(`{"name":"@superdurable/connector-google-gmail","version":"0.3.0","exports":{".":"./dist/index.js","./dex-connector.json":"./dex-connector.json"}}`), 0o600))
	_, err = load(manifestPath)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(packagePath, []byte(`{"name":"@superdurable/connector-google-gmail","version":"0.2.0"}`), 0o600))
	_, err = load(manifestPath)
	require.ErrorContains(t, err, "spec.languages.typescript")
	require.NoError(t, os.Remove(pyprojectPath))
	_, err = load(manifestPath)
	require.ErrorContains(t, err, "spec.languages.python requires")
}

func TestValidateJavaAndRustPackageFilesAndVersions(t *testing.T) {
	repositoryRoot := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(repositoryRoot, "catalog.yaml"), []byte("connectors: []\n"), 0o600))
	connectorDirectory := filepath.Join(repositoryRoot, "connectors", "google", "gmail")
	require.NoError(t, os.MkdirAll(filepath.Join(connectorDirectory, "java"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(connectorDirectory, "rust"), 0o755))
	manifestPath := filepath.Join(connectorDirectory, "connector.yaml")
	manifest := strings.Replace(readFixtureManifest(t), "spec:\n", "spec:\n  languages:\n    java: {version: v0.2.0, operations: [appendRows], triggers: []}\n    rust: {version: v0.3.0, operations: [appendRows], triggers: []}\n", 1)
	require.NoError(t, os.WriteFile(manifestPath, []byte(manifest), 0o600))
	propertiesPath := filepath.Join(connectorDirectory, "java", "gradle.properties")
	cargoPath := filepath.Join(connectorDirectory, "rust", "Cargo.toml")
	require.NoError(t, os.WriteFile(propertiesPath, []byte("# Maven Central coordinate\ngroup=io.superdurable\nartifactId=dex-connector-google-gmail\nversion=0.2.0\ndexConnectorsSdkVersion=0.1.0\n"), 0o600))
	require.NoError(t, os.WriteFile(cargoPath, []byte("[package]\nname = \"dex-connector-google-gmail\"\nversion = \"0.3.0\"\n\n[dependencies]\ndex-connectors-sdk = \"=0.1.0\"\n"), 0o600))
	_, err := load(manifestPath)
	require.NoError(t, err)

	var capabilities bytes.Buffer
	require.NoError(t, languageCapabilitiesCommand(manifestPath, "rust", &capabilities))
	require.JSONEq(t, `{"operations":["appendRows"],"triggers":[]}`, capabilities.String())

	require.NoError(t, os.WriteFile(propertiesPath, []byte("group=com.example\nartifactId=dex-connector-google-gmail\nversion=0.2.0\n"), 0o600))
	_, err = load(manifestPath)
	require.ErrorContains(t, err, "group, artifactId, and version must match spec.languages.java (io.superdurable:dex-connector-google-gmail v0.2.0)")
	require.NoError(t, os.WriteFile(propertiesPath, []byte("group=io.superdurable\nartifactId=dex-connector-google-gmail\nversion=0.1.0\n"), 0o600))
	_, err = load(manifestPath)
	require.ErrorContains(t, err, "spec.languages.java")
	require.NoError(t, os.Remove(propertiesPath))
	_, err = load(manifestPath)
	require.ErrorIs(t, err, errLanguagePackageFileMissing)
	require.ErrorContains(t, err, "spec.languages.java requires")

	require.NoError(t, os.WriteFile(propertiesPath, []byte("group=io.superdurable\nartifactId=dex-connector-google-gmail\nversion=0.2.0\n"), 0o600))
	require.NoError(t, os.WriteFile(cargoPath, []byte("[package]\nname = \"dex-connector-gmail\"\nversion = \"0.3.0\"\n"), 0o600))
	_, err = load(manifestPath)
	require.ErrorContains(t, err, "[package] name and version must match spec.languages.rust (dex-connector-google-gmail v0.3.0)")
	require.NoError(t, os.Remove(cargoPath))
	_, err = load(manifestPath)
	require.ErrorIs(t, err, errLanguagePackageFileMissing)
	require.ErrorContains(t, err, "spec.languages.rust requires")
}

func TestLanguagePackageNamesFollowEachRegistryConvention(t *testing.T) {
	require.Equal(t, "dex-connector-google-gmail", languagePackageName("connectors/google/gmail", "python", ""))
	require.Equal(t, "@superdurable/connector-google-gmail", languagePackageName("connectors/google/gmail", "typescript", ""))
	require.Equal(t, "io.superdurable:dex-connector-google-gmail", languagePackageName("connectors/google/gmail", "java", ""))
	require.Equal(t, "dex-connector-slack", languagePackageName("connectors/slack", "rust", ""))
	require.Equal(t, "io.superdurable:dex-connector-mail", languagePackageName("connectors/google/gmail", "java", "io.superdurable:dex-connector-mail"))
	require.Panics(t, func() { languagePackageName("connectors/slack", "go", "") })
}
