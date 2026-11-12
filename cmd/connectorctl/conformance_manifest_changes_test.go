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

	"github.com/stretchr/testify/require"
)

const conformanceManifestBase = `apiVersion: connectors.dex.dev/v1alpha1
kind: Connector
metadata:
  name: example
  displayName: Example
  description: Example connector.
  company: Example
  version: v0.1.0
spec:
  auth:
    type: apiKey
  operations:
    - name: createThing
      kind: mutation
      description: Create a thing.
      branches:
        - id: created
        - id: rejected
          optional: true
`

func TestHasConformanceRelevantManifestChange(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		head      string
		isChanged bool
	}{
		{"identical", conformanceManifestBase, false},
		{"display metadata and comments only", strings.NewReplacer(
			"  displayName: Example\n", "  displayName: Example Inc\n  categories: [ai, crm]\n",
			"  description: Example connector.\n", "  # Display copy.\n  description: Better copy.\n",
		).Replace(conformanceManifestBase), false},
		{"metadata version", strings.Replace(conformanceManifestBase, "v0.1.0", "v0.2.0", 1), true},
		{"auth type", strings.Replace(conformanceManifestBase, "type: apiKey", "type: oauth2", 1), true},
		{"operation branch", strings.Replace(conformanceManifestBase, "id: rejected", "id: denied", 1), true},
		{"operation description", strings.Replace(conformanceManifestBase, "Create a thing.", "Make a thing.", 1), true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			isChanged, err := hasConformanceRelevantManifestChange([]byte(conformanceManifestBase), []byte(testCase.head))
			require.NoError(t, err)
			require.Equal(t, testCase.isChanged, isChanged)
		})
	}
}

func TestHasConformanceRelevantManifestChangeRejectsInvalidHeadYAML(t *testing.T) {
	_, err := hasConformanceRelevantManifestChange([]byte(conformanceManifestBase), []byte("spec: [unclosed\n"))
	require.Error(t, err)
}

func TestHasConformanceRelevantManifestChangeTreatsInvalidBaseYAMLAsChanged(t *testing.T) {
	isChanged, err := hasConformanceRelevantManifestChange([]byte("spec: [unclosed\n"), []byte(conformanceManifestBase))
	require.NoError(t, err)
	require.True(t, isChanged)
}

func TestConformanceManifestChangesCommandComparesRevisions(t *testing.T) {
	repository := t.TempDir()
	runGit := func(args ...string) string {
		command := exec.Command("git", append([]string{"-c", "user.name=Test", "-c", "user.email=test@example.com"}, args...)...)
		command.Dir = repository
		output, err := command.CombinedOutput()
		require.NoError(t, err, string(output))
		return strings.TrimSpace(string(output))
	}
	writeManifest := func(path, contents string) {
		require.NoError(t, os.MkdirAll(filepath.Join(repository, filepath.Dir(path)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(repository, path), []byte(contents), 0o644))
	}
	runGit("init", "-q")
	writeManifest("connectors/display/connector.yaml", conformanceManifestBase)
	writeManifest("connectors/behavior/connector.yaml", conformanceManifestBase)
	runGit("add", ".")
	runGit("commit", "-q", "-m", "base")
	baseRevision := runGit("rev-parse", "HEAD")
	writeManifest("connectors/display/connector.yaml",
		strings.Replace(conformanceManifestBase, "  displayName: Example\n", "  displayName: Example\n  categories: [ai]\n", 1))
	writeManifest("connectors/behavior/connector.yaml", strings.Replace(conformanceManifestBase, "id: rejected", "id: denied", 1))
	writeManifest("connectors/added/connector.yaml", conformanceManifestBase)
	runGit("add", ".")
	runGit("commit", "-q", "-m", "head")

	t.Chdir(repository)
	var output bytes.Buffer
	require.NoError(t, conformanceManifestChangesCommand([]string{baseRevision, "HEAD",
		"connectors/display/connector.yaml", "connectors/behavior/connector.yaml", "connectors/added/connector.yaml"}, &output))
	var changedManifests map[string]bool
	require.NoError(t, json.Unmarshal(output.Bytes(), &changedManifests))
	require.Equal(t, map[string]bool{
		"connectors/display/connector.yaml":  false,
		"connectors/behavior/connector.yaml": true,
		"connectors/added/connector.yaml":    true,
	}, changedManifests)
}

func TestConformanceManifestChangesCommandRejectsUnknownRevision(t *testing.T) {
	repository := t.TempDir()
	runGit := func(args ...string) {
		command := exec.Command("git", append([]string{"-c", "user.name=Test", "-c", "user.email=test@example.com"}, args...)...)
		command.Dir = repository
		output, err := command.CombinedOutput()
		require.NoError(t, err, string(output))
	}
	runGit("init", "-q")
	require.NoError(t, os.WriteFile(filepath.Join(repository, "connector.yaml"), []byte(conformanceManifestBase), 0o644))
	runGit("add", ".")
	runGit("commit", "-q", "-m", "base")

	t.Chdir(repository)
	var output bytes.Buffer
	err := conformanceManifestChangesCommand([]string{"0123456789abcdef0123456789abcdef01234567", "HEAD", "connector.yaml"}, &output)
	require.ErrorContains(t, err, "resolve revision 0123456789abcdef0123456789abcdef01234567")
	require.Empty(t, output.String())
}
