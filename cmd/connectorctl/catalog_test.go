// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/schema"
)

func TestValidateConnectorDirectoryAllowsAnyDepth(t *testing.T) {
	for _, directory := range []string{
		"connectors/github",
		"connectors/google/gmail",
		"connectors/google/gcp/cloudsql",
		"connectors/company/platform/product/service/resource",
	} {
		require.NoError(t, validateConnectorDirectory(directory))
	}
	for _, directory := range []string{"connectors", "connectors/../outside", "/connectors/github", `connectors\github`} {
		require.Error(t, validateConnectorDirectory(directory))
	}
}

func TestConnectorVersionTransitions(t *testing.T) {
	testCases := []struct {
		baseline string
		target   string
		pending  bool
		hasError bool
	}{
		{target: "v0.1.0", pending: true},
		{target: "v0.2.0", hasError: true},
		{baseline: "v0.7.0", target: "v0.7.0"},
		{baseline: "v0.7.0", target: "v0.7.1", pending: true},
		{baseline: "v0.7.0", target: "v0.8.0", pending: true},
		{baseline: "v0.7.4", target: "v1.0.0", pending: true},
		{baseline: "v0.7.0", target: "v0.9.0", hasError: true},
		{baseline: "v0.7.0", target: "v0.6.0", hasError: true},
	}
	for _, testCase := range testCases {
		pending, err := validateConnectorVersionTransition(testCase.baseline, testCase.target)
		if testCase.hasError {
			require.Error(t, err)
			continue
		}
		require.NoError(t, err)
		require.Equal(t, testCase.pending, pending)
	}
}

func TestDirectoryRegistryRejectsUnregisteredManifest(t *testing.T) {
	repositoryRoot := t.TempDir()
	writeConnectorFixture(t, repositoryRoot, "connectors/company/product", "example.com/connectors/company/product")
	writeConnectorFixture(t, repositoryRoot, "connectors/company/platform/deep/service", "example.com/connectors/company/platform/deep/service")
	registryPath := filepath.Join(repositoryRoot, "connectors.yaml")
	require.NoError(t, os.WriteFile(registryPath, []byte(`apiVersion: connectors.dex.dev/directory-list/v1alpha1
kind: ConnectorDirectoryList
directories:
  - connectors/company/product
`), 0o600))
	_, err := loadConnectorDirectoryEntries(registryPath)
	require.ErrorContains(t, err, "not registered")
}

func TestDirectoryRegistryRejectsSymlinkedDirectory(t *testing.T) {
	repositoryRoot := t.TempDir()
	writeConnectorFixture(t, repositoryRoot, "actual/example", "example.com/connectors/company/example")
	require.NoError(t, os.MkdirAll(filepath.Join(repositoryRoot, "connectors", "company"), 0o755))
	require.NoError(t, os.Symlink(filepath.Join(repositoryRoot, "actual", "example"), filepath.Join(repositoryRoot, "connectors", "company", "example")))
	registryPath := filepath.Join(repositoryRoot, "connectors.yaml")
	require.NoError(t, os.WriteFile(registryPath, []byte(`apiVersion: connectors.dex.dev/directory-list/v1alpha1
kind: ConnectorDirectoryList
directories:
  - connectors/company/example
`), 0o600))
	_, err := loadConnectorDirectoryEntries(registryPath)
	require.ErrorContains(t, err, "cannot contain symlinks")
}

func TestDirectoryRegistryRejectsMissingDuplicateAndUnsortedDirectories(t *testing.T) {
	repositoryRoot := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(repositoryRoot, "connectors", "missing"), 0o755))
	registryPath := filepath.Join(repositoryRoot, "connectors.yaml")
	require.NoError(t, os.WriteFile(registryPath, []byte(`apiVersion: connectors.dex.dev/directory-list/v1alpha1
kind: ConnectorDirectoryList
directories:
  - connectors/missing
`), 0o600))
	_, err := loadConnectorDirectoryEntries(registryPath)
	require.ErrorContains(t, err, "connector manifest is missing")

	require.NoError(t, os.WriteFile(registryPath, []byte(`apiVersion: connectors.dex.dev/directory-list/v1alpha1
kind: ConnectorDirectoryList
directories:
  - connectors/missing
  - connectors/missing
`), 0o600))
	_, err = loadConnectorDirectoryEntries(registryPath)
	require.ErrorContains(t, err, "duplicate connector directory")

	require.NoError(t, os.WriteFile(registryPath, []byte(`apiVersion: connectors.dex.dev/directory-list/v1alpha1
kind: ConnectorDirectoryList
directories:
  - connectors/zeta
  - connectors/alpha
`), 0o600))
	_, err = loadConnectorDirectoryEntries(registryPath)
	require.ErrorContains(t, err, "must be sorted")
}

func TestReleaseMatrixFindsEveryDeclaredRepositoryVersion(t *testing.T) {
	githubOutput := filepath.Join(t.TempDir(), "github-output")
	require.NoError(t, releaseMatrixCommand([]string{
		"--registry", filepath.Join("..", "..", "connectors.yaml"),
		"--include-published",
		"--github-output", githubOutput,
	}))
	contents, err := os.ReadFile(githubOutput)
	require.NoError(t, err)
	require.Contains(t, string(contents), "count=6")
	require.Contains(t, string(contents), `"directory":"connectors/google/gmail"`)
	require.Contains(t, string(contents), `"version":"v0.10.0"`)
}

func TestDirectoryRegistryRejectsMissingCompanyLogo(t *testing.T) {
	repositoryRoot := t.TempDir()
	writeConnectorFixture(t, repositoryRoot, "connectors/acme/mail", "example.com/connectors/acme/mail")
	require.NoError(t, os.Remove(filepath.Join(repositoryRoot, "connectors", "acme", "logo.svg")))
	registryPath := filepath.Join(repositoryRoot, "connectors.yaml")
	require.NoError(t, os.WriteFile(registryPath, []byte(`apiVersion: connectors.dex.dev/directory-list/v1alpha1
kind: ConnectorDirectoryList
directories:
  - connectors/acme/mail
`), 0o600))
	_, err := loadConnectorDirectoryEntries(registryPath)
	require.ErrorContains(t, err, "company logo")
}

func TestDirectoryRegistryRejectsCompanyDirectoryMismatch(t *testing.T) {
	repositoryRoot := t.TempDir()
	writeConnectorFixture(t, repositoryRoot, "connectors/acme/mail", "example.com/connectors/acme/mail")
	manifestPath := filepath.Join(repositoryRoot, "connectors", "acme", "mail", "connector.yaml")
	manifest, err := os.ReadFile(manifestPath)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(manifestPath, []byte(strings.Replace(string(manifest), "company: acme", "company: Other", 1)), 0o600))
	registryPath := filepath.Join(repositoryRoot, "connectors.yaml")
	require.NoError(t, os.WriteFile(registryPath, []byte(`apiVersion: connectors.dex.dev/directory-list/v1alpha1
kind: ConnectorDirectoryList
directories:
  - connectors/acme/mail
`), 0o600))
	_, err = loadConnectorDirectoryEntries(registryPath)
	require.ErrorContains(t, err, "must match directory acme")
}

func TestCatalogCheckAcceptsRepositoryCompanies(t *testing.T) {
	require.NoError(t, catalogCommand([]string{"--check", "--registry", filepath.Join("..", "..", "connectors.yaml")}))
}

func TestCatalogIncludesUIUnitsOperationsAndTriggers(t *testing.T) {
	entries := []connectorDirectoryEntry{{
		Directory: "connectors/acme/chat",
		Manifest: schema.Manifest{
			Metadata: schema.Metadata{
				Name: "acme-chat", DisplayName: "Acme Chat", Description: "Catalog fixture.", Company: "Acme", Version: "v0.1.0",
			},
			Spec: schema.Spec{
				Studio: &schema.Studio{Units: []schema.StudioUnit{
					{ID: "channelPicker", Description: "Select one channel."},
					{ID: "searchQueryInput", Description: "Enter a search query."},
				}},
				Triggers: []schema.Trigger{{Name: "messageReceived", Description: "Receive one message."}},
				Operations: []schema.Operation{
					{Name: "getMessage", Kind: "query", Description: "Read one message."},
					{Name: "sendMessage", Kind: "mutation", Description: "Send one message."},
				},
			},
		},
	}}
	encoded, err := encodeConnectorCatalog(entries)
	require.NoError(t, err)
	catalog := string(encoded)
	require.Contains(t, catalog, "name: channelPicker")
	require.Contains(t, catalog, "name: searchQueryInput")
	require.Contains(t, catalog, "name: messageReceived")
	require.Contains(t, catalog, "name: getMessage")
	require.Contains(t, catalog, "kind: query")
	require.Contains(t, catalog, "kind: mutation")
}

func writeConnectorFixture(t *testing.T, repositoryRoot, directory, modulePath string) {
	t.Helper()
	connectorDirectory := filepath.Join(repositoryRoot, filepath.FromSlash(directory))
	require.NoError(t, os.MkdirAll(connectorDirectory, 0o755))
	fixture, err := os.ReadFile(filepath.Join("..", "..", "schema", "testdata", "google-oauth.yaml"))
	require.NoError(t, err)
	contents := strings.Replace(string(fixture), "name: google-sheets-fixture", "name: fixture-connector", 1)
	if strings.HasPrefix(directory, "connectors/") {
		companyDir := strings.Split(strings.TrimPrefix(directory, "connectors/"), "/")[0]
		contents = strings.Replace(contents, "company: Google", "company: "+companyDir, 1)
		logoDirectory := filepath.Join(repositoryRoot, "connectors", companyDir)
		require.NoError(t, os.MkdirAll(logoDirectory, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(logoDirectory, "logo.svg"), []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"></svg>`), 0o600))
	}
	require.NoError(t, os.WriteFile(filepath.Join(connectorDirectory, "connector.yaml"), []byte(contents), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(connectorDirectory, "go.mod"), []byte("module "+modulePath+"\n\ngo 1.24\n"), 0o600))
}
