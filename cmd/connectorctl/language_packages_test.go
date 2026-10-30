// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateLanguagePackageFilesAndVersions(t *testing.T) {
	repositoryRoot := filepath.Join(t.TempDir(), "connectors", "parent")
	require.NoError(t, os.MkdirAll(repositoryRoot, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(repositoryRoot, "catalog.yaml"), []byte("connectors: []\n"), 0o600))
	connectorDirectory := filepath.Join(repositoryRoot, "connectors", "google", "gmail")
	require.NoError(t, os.MkdirAll(filepath.Join(connectorDirectory, "python"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(connectorDirectory, "typescript"), 0o755))
	manifestPath := filepath.Join(connectorDirectory, "connector.yaml")
	manifest := strings.Replace(readFixtureManifest(t), "spec:\n", "spec:\n  languages:\n    python: {version: v0.2.0}\n    typescript: {version: v0.3.0}\n", 1)
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
	require.NoError(t, os.WriteFile(packagePath, []byte(`{"name":"@superdurable/connector-google-gmail","version":"0.2.0"}`), 0o600))
	_, err = load(manifestPath)
	require.ErrorContains(t, err, "spec.languages.typescript")
	require.NoError(t, os.Remove(pyprojectPath))
	_, err = load(manifestPath)
	require.ErrorContains(t, err, "spec.languages.python requires")
}
