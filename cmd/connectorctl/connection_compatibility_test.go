// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	releasedScopes = "scopes: [https://www.googleapis.com/auth/drive.file]"
	// addedScope breaks every connection authorized for releasedScopes alone.
	addedScope = "scopes: [https://www.googleapis.com/auth/drive.file, https://www.googleapis.com/auth/drive.readonly]"
)

func TestReleaseMatrixRequiresANewMajorVersionToBreakConnections(t *testing.T) {
	repositoryRoot := releaseCandidateMainRepository(t)
	manifestPath := filepath.Join(repositoryRoot, releaseCandidateChat, "connector.yaml")
	catalogPath := filepath.Join(repositoryRoot, "catalog.yaml")
	replaceInFile(t, manifestPath, releasedScopes, addedScope)

	replaceInFile(t, manifestPath, "version: v0.1.0", "version: v0.2.0")
	err := releaseMatrixCommand([]string{"--catalog", catalogPath})
	require.ErrorContains(t, err, releaseCandidateChat+" v0.2.0 breaks connections made with v0.1.0, so it must be v1.0.0")
	require.ErrorContains(t, err, "spec.auth OAuth scopes add https://www.googleapis.com/auth/drive.readonly")

	replaceInFile(t, manifestPath, "version: v0.2.0", "version: v1.0.0")
	githubOutput := filepath.Join(t.TempDir(), "github-output")
	require.NoError(t, releaseMatrixCommand([]string{"--catalog", catalogPath, "--github-output", githubOutput}),
		"a new major version has no released contract to keep")
	require.Equal(t, "v1.0.0", readReleaseMatrixOutput(t, githubOutput).Include[0].Version)
}

func TestLanguageReleaseMatrixChecksSharedConnectionCompatibility(t *testing.T) {
	repositoryRoot := releaseCandidateMainRepository(t)
	manifestPath := filepath.Join(repositoryRoot, releaseCandidateChat, "connector.yaml")
	packageDirectory := filepath.Join(repositoryRoot, releaseCandidateChat, "python")
	require.NoError(t, os.MkdirAll(packageDirectory, 0o755))
	packageName := languagePackageName(releaseCandidateChat, "python", "")
	require.NoError(t, os.WriteFile(filepath.Join(packageDirectory, "pyproject.toml"),
		[]byte("[project]\nname = \""+packageName+"\"\nversion = \"0.1.0\"\n"), 0o600))
	replaceInFile(t, manifestPath, "spec:\n", "spec:\n  languages:\n    python: {version: v0.1.0}\n")
	runReleaseCandidateGit(t, repositoryRoot, "add", ".")
	runReleaseCandidateGit(t, repositoryRoot, "commit", "-m", "connector(acme): add Python package")
	runReleaseCandidateGit(t, repositoryRoot, "tag", releaseCandidateChat+"/python/v0.1.0")
	replaceInFile(t, manifestPath, "python: {version: v0.1.0}", "python: {version: v0.2.0}")
	replaceInFile(t, filepath.Join(packageDirectory, "pyproject.toml"), "0.1.0", "0.2.0")
	replaceInFile(t, manifestPath, releasedScopes, addedScope)
	err := releaseMatrixCommand([]string{"--catalog", filepath.Join(repositoryRoot, "catalog.yaml")})
	require.ErrorContains(t, err, releaseCandidateChat+" v0.2.0 breaks connections made with v0.1.0")
}

func TestLanguageReleaseCandidateChecksSharedConnectionCompatibility(t *testing.T) {
	repositoryRoot := releaseCandidateMainRepository(t)
	manifestPath := filepath.Join(repositoryRoot, releaseCandidateChat, "connector.yaml")
	packageDirectory := filepath.Join(repositoryRoot, releaseCandidateChat, "python")
	require.NoError(t, os.MkdirAll(packageDirectory, 0o755))
	packageName := languagePackageName(releaseCandidateChat, "python", "")
	require.NoError(t, os.WriteFile(filepath.Join(packageDirectory, "pyproject.toml"),
		[]byte("[project]\nname = \""+packageName+"\"\nversion = \"0.1.0\"\n"), 0o600))
	replaceInFile(t, manifestPath, "spec:\n", "spec:\n  languages:\n    python: {version: v0.1.0}\n")
	runReleaseCandidateGit(t, repositoryRoot, "add", ".")
	runReleaseCandidateGit(t, repositoryRoot, "commit", "-m", "connector(acme): add Python package")
	runReleaseCandidateGit(t, repositoryRoot, "tag", releaseCandidateChat+"/python/v0.1.0")
	commit, candidateRoot := releaseCandidateCommit(t, repositoryRoot, func(candidateRoot string) {
		candidateManifest := filepath.Join(candidateRoot, releaseCandidateChat, "connector.yaml")
		replaceInFile(t, candidateManifest, "python: {version: v0.1.0}", "python: {version: v0.2.0}")
		replaceInFile(t, candidateManifest, releasedScopes, addedScope)
		replaceInFile(t, filepath.Join(candidateRoot, releaseCandidateChat, "python", "pyproject.toml"), "0.1.0", "0.2.0")
	})
	_, err := planConnectorReleaseCandidateLanguage(filepath.Join(repositoryRoot, "catalog.yaml"),
		candidateRoot, releaseCandidateChat, commit, "v0.2.0-rc.1", "python")
	require.ErrorContains(t, err, releaseCandidateChat+" v0.2.0 breaks connections made with v0.1.0")
}

func TestReleaseMatrixAcceptsAMinorReleaseThatKeepsConnections(t *testing.T) {
	repositoryRoot := releaseCandidateMainRepository(t)
	manifestPath := filepath.Join(repositoryRoot, releaseCandidateChat, "connector.yaml")
	replaceInFile(t, manifestPath, "  auth:\n",
		"      - {name: region, goName: Region, type: string, description: Region., required: false}\n  auth:\n")
	replaceInFile(t, manifestPath, "version: v0.1.0", "version: v0.2.0")
	require.NoError(t, releaseMatrixCommand([]string{"--catalog", filepath.Join(repositoryRoot, "catalog.yaml")}))

	replaceInFile(t, manifestPath, releasedScopes, addedScope)
	replaceInFile(t, manifestPath, "version: v0.2.0", "version: v0.1.0")
	require.NoError(t, releaseMatrixCommand([]string{"--catalog", filepath.Join(repositoryRoot, "catalog.yaml")}),
		"an unchanged version defers the release, so nothing is compared yet")
}

func TestReleaseCandidateRequiresANewMajorVersionToBreakConnections(t *testing.T) {
	repositoryRoot := releaseCandidateMainRepository(t)
	catalogPath := filepath.Join(repositoryRoot, "catalog.yaml")
	commit, candidateRoot := releaseCandidateCommit(t, repositoryRoot, func(candidateRoot string) {
		manifestPath := filepath.Join(candidateRoot, releaseCandidateChat, "connector.yaml")
		replaceInFile(t, manifestPath, releasedScopes, addedScope)
		replaceInFile(t, manifestPath, "version: v0.1.0", "version: v0.2.0")
	})
	_, err := planConnectorReleaseCandidate(catalogPath, candidateRoot, releaseCandidateChat, commit, "v0.2.0-rc.1")
	require.ErrorContains(t, err, releaseCandidateChat+" v0.2.0 breaks connections made with v0.1.0, so it must be v1.0.0")

	replaceInFile(t, filepath.Join(candidateRoot, releaseCandidateChat, "connector.yaml"), "version: v0.2.0", "version: v1.0.0")
	runReleaseCandidateGit(t, candidateRoot, "commit", "-am", "connector(acme): release the breaking change as v1")
	commit = runReleaseCandidateGit(t, candidateRoot, "rev-parse", "HEAD")
	candidate, err := planConnectorReleaseCandidate(catalogPath, candidateRoot, releaseCandidateChat, commit, "v1.0.0-rc.1")
	require.NoError(t, err)
	require.Equal(t, releaseCandidateChat+"/v1.0.0-rc.1", candidate.Tag)

	replaceInFile(t, filepath.Join(candidateRoot, releaseCandidateChat, "connector.yaml"), addedScope, releasedScopes)
	runReleaseCandidateGit(t, candidateRoot, "commit", "-am", "connector(acme): settle the v1 scopes")
	commit = runReleaseCandidateGit(t, candidateRoot, "rev-parse", "HEAD")
	_, err = planConnectorReleaseCandidate(catalogPath, candidateRoot, releaseCandidateChat, commit, "v1.0.0-rc.2")
	require.NoError(t, err, "candidates of a major version without a release may still change its contract")
}

func TestReleaseCandidateComparesWithTheLatestReleaseOfItsMajorVersion(t *testing.T) {
	repositoryRoot := releaseCandidateMainRepository(t)
	manifestPath := filepath.Join(repositoryRoot, releaseCandidateChat, "connector.yaml")
	replaceInFile(t, manifestPath, releasedScopes, addedScope)
	replaceInFile(t, manifestPath, "version: v0.1.0", "version: v1.0.0")
	runReleaseCandidateGit(t, repositoryRoot, "commit", "-am", "connector(acme): release v1 (breaking)")
	runReleaseCandidateGit(t, repositoryRoot, "tag", releaseCandidateChat+"/v1.0.0")

	commit, candidateRoot := releaseCandidateCommit(t, repositoryRoot, func(candidateRoot string) {
		candidateManifestPath := filepath.Join(candidateRoot, releaseCandidateChat, "connector.yaml")
		replaceInFile(t, candidateManifestPath, addedScope, releasedScopes)
		replaceInFile(t, candidateManifestPath, "version: v1.0.0", "version: v1.1.0")
	})
	_, err := planConnectorReleaseCandidate(filepath.Join(repositoryRoot, "catalog.yaml"), candidateRoot, releaseCandidateChat, commit, "v1.1.0-rc.1")
	require.ErrorContains(t, err, releaseCandidateChat+" v1.1.0 breaks connections made with v1.0.0, so it must be v2.0.0")
	require.ErrorContains(t, err, "spec.auth OAuth scopes remove https://www.googleapis.com/auth/drive.readonly")
}

func TestReleaseMatrixKeepsTheHostKeptCredentialsOfAReleaseThatPublishedThem(t *testing.T) {
	for name, isReleasedUnderTheRule := range map[string]bool{"published": true, "released before the rule": false} {
		t.Run(name, func(t *testing.T) {
			repositoryRoot := releaseCandidateMainRepository(t)
			manifestPath := filepath.Join(repositoryRoot, releaseCandidateChat, "connector.yaml")
			replaceInFile(t, manifestPath, "        required: true\n    guide:", "        required: true\n"+
				"      - {name: client_id, goName: ClientID, type: string, description: Client ID., required: true}\n"+
				"      - {name: client_secret, goName: ClientSecret, type: secretString, description: Client secret., required: true}\n    guide:")
			replaceInFile(t, manifestPath, "version: v0.1.0", "version: v1.0.0")
			if isReleasedUnderTheRule {
				rulePath := filepath.Join(repositoryRoot, filepath.FromSlash(hostKeptCredentialRulePath))
				require.NoError(t, os.MkdirAll(filepath.Dir(rulePath), 0o755))
				require.NoError(t, os.WriteFile(rulePath, []byte("package schema\n"), 0o600))
			}
			runReleaseCandidateGit(t, repositoryRoot, "add", ".")
			runReleaseCandidateGit(t, repositoryRoot, "commit", "-m", "connector(acme): release v1 (breaking)")
			runReleaseCandidateGit(t, repositoryRoot, "tag", releaseCandidateChat+"/v1.0.0")

			replaceInFile(t, manifestPath, "      pkce: true", "      pkce: true\n      clientIDCredential: client_id\n      clientSecretCredential: client_secret")
			replaceInFile(t, manifestPath, "version: v1.0.0", "version: v1.1.0")
			err := releaseMatrixCommand([]string{"--catalog", filepath.Join(repositoryRoot, "catalog.yaml")})
			if !isReleasedUnderTheRule {
				require.NoError(t, err, "no application of a release before the rule received a credential without host-kept fields")
				return
			}
			require.ErrorContains(t, err, releaseCandidateChat+" v1.1.0 breaks connections made with v1.0.0, so it must be v2.0.0")
			require.ErrorContains(t, err, "spec.auth host-kept credentials add client_secret")
		})
	}
}

// A moved rule file would silently stop the host-kept comparison, because every release would look older than it.
func TestTheHostKeptCredentialRuleIsWhereReleasesAreChecked(t *testing.T) {
	_, err := os.Stat(filepath.Join("..", "..", filepath.FromSlash(hostKeptCredentialRulePath)))
	require.NoError(t, err)
}
