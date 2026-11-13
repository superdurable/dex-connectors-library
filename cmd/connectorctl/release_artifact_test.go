// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const releaseArtifactFixtureDirectory = "connectors/google/sheets-fixture"

// releaseArtifactFixture builds the fixture's Studio UI tarball and returns the manifest, tarball, and digest paths.
func releaseArtifactFixture(t *testing.T) (manifestPath, uiPath, uiDigestPath string) {
	t.Helper()
	fixture := filepath.Join("testdata", "release-artifact")
	manifestPath = filepath.Join(fixture, filepath.FromSlash(releaseArtifactFixtureDirectory), "connector.yaml")
	uiPath = filepath.Join(t.TempDir(), "connector-ui.tgz")
	uiDigestPath = uiPath + ".sha256"
	require.NoError(t, uiArtifact([]string{
		"--manifest", manifestPath, "--ui-root", filepath.Join(fixture, "ui-dist"),
		"--output", uiPath, "--digest-output", uiDigestPath,
	}))
	return manifestPath, uiPath, uiDigestPath
}

// requireReleaseArtifactGolden compares the release metadata and its digest line with a checked-in golden file.
// Set UPDATE_RELEASE_ARTIFACT_GOLDEN=1 to rewrite the golden file.
func requireReleaseArtifactGolden(t *testing.T, goldenName string, arguments []string) {
	t.Helper()
	output := filepath.Join(t.TempDir(), "connector-release.json")
	digestOutput := output + ".sha256"
	require.NoError(t, releaseArtifact(append(arguments, "--output", output, "--digest-output", digestOutput)))
	actual, err := os.ReadFile(output)
	require.NoError(t, err)
	digest, err := os.ReadFile(digestOutput)
	require.NoError(t, err)
	require.Equal(t, fmt.Sprintf("%x  connector-release.json\n", sha256.Sum256(actual)), string(digest))
	goldenPath := filepath.Join("testdata", "release-artifact", goldenName)
	if os.Getenv("UPDATE_RELEASE_ARTIFACT_GOLDEN") == "1" {
		require.NoError(t, os.WriteFile(goldenPath, actual, 0o644))
	}
	expected, err := os.ReadFile(goldenPath)
	require.NoError(t, err)
	require.Equal(t, string(expected), string(actual))
}

func TestReleaseArtifactGoOutputMatchesGolden(t *testing.T) {
	manifestPath, uiPath, uiDigestPath := releaseArtifactFixture(t)
	requireReleaseArtifactGolden(t, "go.golden.json", []string{
		"--manifest", manifestPath,
		"--module-path", "github.com/superdurable/dex-connectors-library/" + releaseArtifactFixtureDirectory + "/go",
		"--version", "v0.3.0", "--tag", releaseArtifactFixtureDirectory + "/go/v0.3.0",
		"--source-sha", strings.Repeat("a", 40), "--ui-artifact", uiPath, "--ui-digest", uiDigestPath,
	})
}

func TestReleaseArtifactLanguageOutputsMatchGolden(t *testing.T) {
	manifestPath, uiPath, uiDigestPath := releaseArtifactFixture(t)
	for _, testCase := range []struct {
		language, version, packageName, golden string
	}{
		{language: "typescript", version: "v0.5.0", packageName: "@superdurable/connector-google-sheets-fixture", golden: "typescript.golden.json"},
		{language: "python", version: "v0.2.1", packageName: "dex-connector-google-sheets-fixture", golden: "python.golden.json"},
	} {
		t.Run(testCase.language, func(t *testing.T) {
			requireReleaseArtifactGolden(t, testCase.golden, []string{
				"--manifest", manifestPath, "--language", testCase.language, "--package", testCase.packageName,
				"--version", testCase.version, "--tag", releaseArtifactFixtureDirectory + "/" + testCase.language + "/" + testCase.version,
				"--source-sha", strings.Repeat("b", 40), "--ui-artifact", uiPath, "--ui-digest", uiDigestPath,
			})
		})
	}
}

func TestReleaseArtifactDerivesTheLanguagePackageAndAcceptsACandidate(t *testing.T) {
	manifestPath, uiPath, uiDigestPath := releaseArtifactFixture(t)
	output := filepath.Join(t.TempDir(), "connector-release.json")
	require.NoError(t, releaseArtifact([]string{
		"--manifest", manifestPath, "--language", "typescript", "--version", "v0.5.0-rc.2",
		"--tag", releaseArtifactFixtureDirectory + "/typescript/v0.5.0-rc.2", "--source-sha", strings.Repeat("c", 40),
		"--ui-artifact", uiPath, "--ui-digest", uiDigestPath, "--output", output, "--digest-output", output + ".sha256",
	}))
	contents, err := os.ReadFile(output)
	require.NoError(t, err)
	require.Contains(t, string(contents), `"package": "@superdurable/connector-google-sheets-fixture"`)
	require.Contains(t, string(contents), `"version": "v0.5.0-rc.2"`)
	require.NotContains(t, string(contents), `"modulePath"`)
}

func TestReleaseArtifactRejectsALanguageReleaseThatDescribesAnotherRelease(t *testing.T) {
	manifestPath, uiPath, uiDigestPath := releaseArtifactFixture(t)
	typeScriptTag := releaseArtifactFixtureDirectory + "/typescript/v0.5.0"
	for name, testCase := range map[string]struct {
		arguments []string
		message   string
	}{
		"language with a module path": {arguments: []string{"--language", "typescript", "--module-path", "example.com/sheets",
			"--version", "v0.5.0", "--tag", typeScriptTag}, message: "usage:"},
		"Go release without a module path": {arguments: []string{"--version", "v0.3.0",
			"--tag", releaseArtifactFixtureDirectory + "/go/v0.3.0"}, message: "usage:"},
		"Go release with a package": {arguments: []string{"--module-path", "example.com/sheets", "--package", "sheets",
			"--version", "v0.3.0", "--tag", releaseArtifactFixtureDirectory + "/go/v0.3.0"}, message: "usage:"},
		"unknown language": {arguments: []string{"--language", "rust", "--version", "v0.5.0", "--tag", typeScriptTag},
			message: "must be python or typescript"},
		"Go version for a language": {arguments: []string{"--language", "typescript", "--version", "v0.3.0",
			"--tag", releaseArtifactFixtureDirectory + "/typescript/v0.3.0"}, message: "does not match spec.languages.typescript version v0.5.0"},
		"tag of another language": {arguments: []string{"--language", "typescript", "--version", "v0.5.0",
			"--tag", releaseArtifactFixtureDirectory + "/python/v0.5.0"}, message: "must be " + typeScriptTag},
		"another package": {arguments: []string{"--language", "typescript", "--package", "@superdurable/connector-slack",
			"--version", "v0.5.0", "--tag", typeScriptTag}, message: "does not match the manifest's typescript package"},
	} {
		t.Run(name, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "connector-release.json")
			arguments := append([]string{"--manifest", manifestPath, "--source-sha", strings.Repeat("d", 40),
				"--ui-artifact", uiPath, "--ui-digest", uiDigestPath, "--output", output, "--digest-output", output + ".sha256"},
				testCase.arguments...)
			require.ErrorContains(t, releaseArtifact(arguments), testCase.message)
			require.NoFileExists(t, output)
		})
	}
}

func TestReleaseArtifactMarksOnlySourceRejectionsForTheDistinctExitCode(t *testing.T) {
	manifestPath, uiPath, uiDigestPath := releaseArtifactFixture(t)
	arguments := func(extra ...string) []string {
		output := filepath.Join(t.TempDir(), "connector-release.json")
		return append([]string{"--manifest", manifestPath, "--source-sha", strings.Repeat("f", 40),
			"--output", output, "--digest-output", output + ".sha256"}, extra...)
	}
	invalidManifest := filepath.Join(t.TempDir(), "connector.yaml")
	require.NoError(t, os.WriteFile(invalidManifest, []byte("apiVersion: connectors.dex.dev/v1alpha1\nkind: Connector\n"), 0o600))
	for name, testCase := range map[string]struct {
		arguments  []string
		isRejected bool
	}{
		"manifest rules": {arguments: append(arguments("--language", "typescript", "--version", "v0.5.0",
			"--tag", releaseArtifactFixtureDirectory+"/typescript/v0.5.0"), "--manifest", invalidManifest), isRejected: true},
		"language version": {arguments: arguments("--language", "typescript", "--version", "v0.3.0",
			"--tag", releaseArtifactFixtureDirectory+"/typescript/v0.3.0", "--ui-artifact", uiPath, "--ui-digest", uiDigestPath), isRejected: true},
		"missing UI digest file": {arguments: arguments("--language", "typescript", "--version", "v0.5.0",
			"--tag", releaseArtifactFixtureDirectory+"/typescript/v0.5.0", "--ui-artifact", uiPath,
			"--ui-digest", filepath.Join(t.TempDir(), "absent.sha256"))},
		"missing manifest file": {arguments: append(arguments("--language", "typescript", "--version", "v0.5.0",
			"--tag", releaseArtifactFixtureDirectory+"/typescript/v0.5.0"), "--manifest", filepath.Join(t.TempDir(), "absent.yaml"))},
	} {
		t.Run(name, func(t *testing.T) {
			err := releaseArtifact(testCase.arguments)
			require.Error(t, err)
			var rejected releaseSourceRejectedError
			require.Equal(t, testCase.isRejected, errors.As(err, &rejected), err.Error())
		})
	}
}

// copyReleaseArtifactRepository copies the fixture repository so a test can change its files.
func copyReleaseArtifactRepository(t *testing.T) (repository, manifestPath string) {
	t.Helper()
	repository = t.TempDir()
	require.NoError(t, os.CopyFS(repository, os.DirFS(filepath.Join("testdata", "release-artifact"))))
	return repository, filepath.Join(repository, filepath.FromSlash(releaseArtifactFixtureDirectory), "connector.yaml")
}

func TestReleaseArtifactKeepsSnapshotFileFailuresOrdinary(t *testing.T) {
	_, uiPath, uiDigestPath := releaseArtifactFixture(t)
	for name, breakSnapshot := range map[string]func(t *testing.T, repository, connector string){
		"package file vanished": func(t *testing.T, _, connector string) {
			require.NoError(t, os.Remove(filepath.Join(connector, "python", "pyproject.toml")))
		},
		"catalog cannot be read": func(t *testing.T, repository, _ string) {
			catalogPath := filepath.Join(repository, "catalog.yaml")
			require.NoError(t, os.Remove(catalogPath))
			// A self-referencing link makes stat fail with an error other than not-exist.
			require.NoError(t, os.Symlink("catalog.yaml", catalogPath))
		},
		"catalog vanished": func(t *testing.T, repository, _ string) {
			require.NoError(t, os.Remove(filepath.Join(repository, "catalog.yaml")))
		},
	} {
		t.Run(name, func(t *testing.T) {
			repository, manifestPath := copyReleaseArtifactRepository(t)
			breakSnapshot(t, repository, filepath.Dir(manifestPath))
			output := filepath.Join(t.TempDir(), "connector-release.json")
			err := releaseArtifact([]string{"--manifest", manifestPath, "--language", "typescript", "--version", "v0.5.0",
				"--tag", releaseArtifactFixtureDirectory + "/typescript/v0.5.0", "--source-sha", strings.Repeat("f", 40),
				"--ui-artifact", uiPath, "--ui-digest", uiDigestPath, "--output", output, "--digest-output", output + ".sha256"})
			require.Error(t, err)
			var rejected releaseSourceRejectedError
			require.False(t, errors.As(err, &rejected), err.Error())
			require.Equal(t, name == "package file vanished", errors.Is(err, errLanguagePackageFileMissing), err.Error())
		})
	}
}

func TestReleaseArtifactReadsStudioFromTheParsedManifestAtAnyIndentation(t *testing.T) {
	_, uiPath, uiDigestPath := releaseArtifactFixture(t)
	_, manifestPath := copyReleaseArtifactRepository(t)
	contents, err := os.ReadFile(manifestPath)
	require.NoError(t, err)
	// Indent the whole spec subtree two more spaces, so its children such as studio sit four spaces in.
	head, spec, isFound := strings.Cut(string(contents), "\nspec:\n")
	require.True(t, isFound)
	lines := strings.Split(spec, "\n")
	for index, line := range lines {
		if line != "" {
			lines[index] = "  " + line
		}
	}
	lines = append([]string{head + "\nspec:"}, lines...)
	require.NoError(t, os.WriteFile(manifestPath, []byte(strings.Join(lines, "\n")), 0o600))
	require.Contains(t, strings.Join(lines, "\n"), "\n    studio:\n")
	arguments := func(extra ...string) []string {
		output := filepath.Join(t.TempDir(), "connector-release.json")
		return append([]string{"--manifest", manifestPath, "--language", "typescript", "--version", "v0.5.0",
			"--tag", releaseArtifactFixtureDirectory + "/typescript/v0.5.0", "--source-sha", strings.Repeat("f", 40),
			"--output", output, "--digest-output", output + ".sha256"}, extra...)
	}
	err = releaseArtifact(arguments())
	require.ErrorIs(t, err, errUIArtifactRequired)
	var rejected releaseSourceRejectedError
	require.False(t, errors.As(err, &rejected), "a missing UI artifact is the caller's input error")
	require.NoError(t, releaseArtifact(arguments("--ui-artifact", uiPath, "--ui-digest", uiDigestPath)))
}

func TestReleaseArtifactRejectsALanguageTheManifestDoesNotDeclare(t *testing.T) {
	repository := t.TempDir()
	require.NoError(t, os.CopyFS(repository, os.DirFS(filepath.Join("testdata", "release-artifact"))))
	connector := filepath.Join(repository, filepath.FromSlash(releaseArtifactFixtureDirectory))
	manifestPath := filepath.Join(connector, "connector.yaml")
	replaceInFile(t, manifestPath, "    typescript:\n      version: v0.5.0\n      operations: [appendRows]\n      triggers: []\n", "")
	require.NoError(t, os.RemoveAll(filepath.Join(connector, "typescript")))
	output := filepath.Join(t.TempDir(), "connector-release.json")
	require.ErrorContains(t, releaseArtifact([]string{
		"--manifest", manifestPath, "--language", "typescript", "--version", "v0.5.0",
		"--tag", releaseArtifactFixtureDirectory + "/typescript/v0.5.0", "--source-sha", strings.Repeat("e", 40),
		"--output", output, "--digest-output", output + ".sha256",
	}), "manifest does not declare spec.languages.typescript")
}
