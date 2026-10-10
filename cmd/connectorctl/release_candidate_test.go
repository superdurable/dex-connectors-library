// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const releaseCandidateChat = "connectors/acme/chat"

func TestReleaseCandidateAcceptsAConnectorChangeAboveItsLatestRelease(t *testing.T) {
	repositoryRoot := releaseCandidateMainRepository(t)
	commit, candidateRoot := releaseCandidateCommit(t, repositoryRoot, func(candidateRoot string) {
		replaceInFile(t, filepath.Join(candidateRoot, releaseCandidateChat, "connector.yaml"), "version: v0.1.0", "version: v0.2.0")
	})
	catalogPath := filepath.Join(repositoryRoot, "catalog.yaml")

	candidate, err := planConnectorReleaseCandidate(catalogPath, candidateRoot, releaseCandidateChat, commit, "v0.2.0-rc.1")
	require.NoError(t, err)
	require.Equal(t, connectorReleaseCandidate{
		Directory: releaseCandidateChat, ManifestPath: releaseCandidateChat + "/connector.yaml", ModulePath: "example.com/" + releaseCandidateChat,
		Version: "v0.2.0-rc.1", Tag: releaseCandidateChat + "/v0.2.0-rc.1", SourceSHA: commit, DisplayName: "Google Sheets Fixture",
	}, candidate)

	runReleaseCandidateGit(t, repositoryRoot, "tag", releaseCandidateChat+"/v0.2.0-rc.1", commit)
	candidate, err = planConnectorReleaseCandidate(catalogPath, candidateRoot, releaseCandidateChat, commit, "v0.2.0-rc.1")
	require.NoError(t, err)
	require.True(t, candidate.IsTagPublished, "publishing the same candidate again repeats safely")

	runReleaseCandidateGit(t, repositoryRoot, "tag", releaseCandidateChat+"/v0.2.0-rc.2", "HEAD")
	_, err = planConnectorReleaseCandidate(catalogPath, candidateRoot, releaseCandidateChat, commit, "v0.2.0-rc.2")
	require.ErrorContains(t, err, "already names")
}

func TestReleaseCandidateRejectsChangesOutsideTheConnector(t *testing.T) {
	for _, outside := range []string{"sdkgo/sdk.go", ".github/workflows/release-candidate.yml", "catalog.yaml"} {
		repositoryRoot := releaseCandidateMainRepository(t)
		commit, candidateRoot := releaseCandidateCommit(t, repositoryRoot, func(candidateRoot string) {
			replaceInFile(t, filepath.Join(candidateRoot, releaseCandidateChat, "connector.yaml"), "version: v0.1.0", "version: v0.2.0")
			path := filepath.Join(candidateRoot, filepath.FromSlash(outside))
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
			contents, err := os.ReadFile(path)
			if err != nil && !os.IsNotExist(err) {
				require.NoError(t, err)
			}
			require.NoError(t, os.WriteFile(path, append(contents, []byte("# changed by the candidate\n")...), 0o600))
		})
		_, err := planConnectorReleaseCandidate(filepath.Join(repositoryRoot, "catalog.yaml"), candidateRoot, releaseCandidateChat, commit, "v0.2.0-rc.1")
		require.ErrorContains(t, err, "outside "+releaseCandidateChat, outside)
	}
}

func TestReleaseCandidateRequiresTheNextDeclaredRelease(t *testing.T) {
	repositoryRoot := releaseCandidateMainRepository(t)
	unchanged, unchangedRoot := releaseCandidateCommit(t, repositoryRoot, func(candidateRoot string) {
		require.NoError(t, os.WriteFile(filepath.Join(candidateRoot, releaseCandidateChat, "notes.md"), []byte("change\n"), 0o600))
	})
	catalogPath := filepath.Join(repositoryRoot, "catalog.yaml")
	_, err := planConnectorReleaseCandidate(catalogPath, unchangedRoot, releaseCandidateChat, unchanged, "v0.1.0-rc.1")
	require.ErrorContains(t, err, "already released")
	_, err = planConnectorReleaseCandidate(catalogPath, unchangedRoot, releaseCandidateChat, unchanged, "v0.2.0-rc.1")
	require.ErrorContains(t, err, "is a candidate of v0.2.0")
	for _, version := range []string{"v0.2.0", "v0.2.0-rc.0", "v0.2.0-beta.1", "0.2.0-rc.1"} {
		_, err = planConnectorReleaseCandidate(catalogPath, unchangedRoot, releaseCandidateChat, unchanged, version)
		require.ErrorContains(t, err, "is not vX.Y.Z-rc.N", version)
	}
	_, err = planConnectorReleaseCandidate(catalogPath, unchangedRoot, releaseCandidateChat, strings.Repeat("0", 40), "v0.1.0-rc.1")
	require.ErrorContains(t, err, "candidate checkout is at")
}

func TestReleaseCandidateAcceptsANewConnectorWithItsCatalogEntryAndCompanyLogo(t *testing.T) {
	repositoryRoot := releaseCandidateMainRepository(t)
	const newsDirectory = "connectors/zeta/news"
	commit, candidateRoot := releaseCandidateCommit(t, repositoryRoot, func(candidateRoot string) {
		writeConnectorFixture(t, candidateRoot, newsDirectory, "example.com/"+newsDirectory)
		replaceInFile(t, filepath.Join(candidateRoot, newsDirectory, "connector.yaml"), "name: fixture-connector", "name: zeta-news")
		replaceInFile(t, filepath.Join(candidateRoot, "catalog.yaml"), "  - "+releaseCandidateChat+"\n", "  - "+releaseCandidateChat+"\n  - "+newsDirectory+"\n")
	})
	candidate, err := planConnectorReleaseCandidate(filepath.Join(repositoryRoot, "catalog.yaml"), candidateRoot, newsDirectory, commit, "v0.1.0-rc.1")
	require.NoError(t, err)
	require.Equal(t, newsDirectory+"/v0.1.0-rc.1", candidate.Tag)
	require.Equal(t, "example.com/"+newsDirectory, candidate.ModulePath)
}

func TestReleaseArtifactAcceptsACandidateOfTheManifestVersion(t *testing.T) {
	repositoryRoot := t.TempDir()
	writeConnectorFixture(t, repositoryRoot, releaseCandidateChat, "example.com/"+releaseCandidateChat)
	manifestPath := filepath.Join(repositoryRoot, releaseCandidateChat, "connector.yaml")
	output := filepath.Join(t.TempDir(), "connector-release.json")
	arguments := func(version string) []string {
		return []string{
			"--manifest", manifestPath, "--module-path", "example.com/" + releaseCandidateChat, "--version", version,
			"--tag", releaseCandidateChat + "/" + version, "--source-sha", strings.Repeat("a", 40),
			"--output", output, "--digest-output", output + ".sha256",
		}
	}
	require.NoError(t, releaseArtifact(arguments("v0.1.0-rc.3")))
	contents, err := os.ReadFile(output)
	require.NoError(t, err)
	require.Contains(t, string(contents), `"version": "v0.1.0-rc.3"`)
	require.ErrorContains(t, releaseArtifact(arguments("v0.2.0-rc.1")), "does not match manifest version")
}

// releaseCandidateMainRepository is a main branch whose acme/chat connector released v0.1.0.
func releaseCandidateMainRepository(t *testing.T) string {
	t.Helper()
	repositoryRoot := t.TempDir()
	writeConnectorFixture(t, repositoryRoot, releaseCandidateChat, "example.com/"+releaseCandidateChat)
	require.NoError(t, os.MkdirAll(filepath.Join(repositoryRoot, "sdkgo"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(repositoryRoot, "sdkgo", "sdk.go"), []byte("package sdkgo\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(repositoryRoot, "catalog.yaml"), []byte(`apiVersion: connectors.dex.dev/catalog-source/v1alpha1
kind: ConnectorCatalogSource
directories:
  - connectors/acme/chat
`), 0o600))
	initializeConnectorReleaseRepository(t, repositoryRoot, releaseCandidateChat+"/v0.1.0")
	return repositoryRoot
}

// releaseCandidateCommit commits change on a branch off main in its own worktree and returns the commit and the
// worktree, leaving the main checkout at main.
func releaseCandidateCommit(t *testing.T, repositoryRoot string, change func(candidateRoot string)) (string, string) {
	t.Helper()
	candidateRoot := filepath.Join(t.TempDir(), "candidate")
	runReleaseCandidateGit(t, repositoryRoot, "worktree", "add", "-b", "candidate", candidateRoot, "HEAD")
	change(candidateRoot)
	runReleaseCandidateGit(t, candidateRoot, "add", ".")
	runReleaseCandidateGit(t, candidateRoot, "commit", "-m", "connector(acme): candidate change")
	return runReleaseCandidateGit(t, candidateRoot, "rev-parse", "HEAD"), candidateRoot
}

func runReleaseCandidateGit(t *testing.T, directory string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = directory
	output, err := command.CombinedOutput()
	require.NoError(t, err, "git %s: %s", strings.Join(args, " "), output)
	return strings.TrimSpace(string(output))
}
