// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/superdurable/dex-connectors-library/schema"
)

const (
	versionBumpMail = "connectors/acme/mail"
	versionBumpChat = "connectors/acme/chat"
)

// versionBumpFixtureVersions are the Go, Python, and TypeScript versions that a fixture's files declare.
type versionBumpFixtureVersions struct {
	goVersion, pythonVersion, typeScriptVersion string
}

func TestBumpUpdatesEveryVersionFileOfAGoPythonTypeScriptConnector(t *testing.T) {
	repositoryRoot := writeVersionBumpRepository(t)
	runReleaseCandidateGit(t, repositoryRoot, "tag", versionBumpChat+"/go/v0.3.0")
	runReleaseCandidateGit(t, repositoryRoot, "tag", versionBumpChat+"/python/v0.1.0")
	runReleaseCandidateGit(t, repositoryRoot, "tag", versionBumpChat+"/typescript/v1.2.3")
	connectorPath := filepath.Join(repositoryRoot, filepath.FromSlash(versionBumpChat))

	output := runVersionBump(t, connectorPath, "--go", "minor", "--python", "patch", "--typescript", "major")

	bumped := versionBumpFixtureVersions{goVersion: "v0.4.0", pythonVersion: "v0.1.1", typeScriptVersion: "v2.0.0"}
	requireVersionBumpChatFiles(t, repositoryRoot, bumped)
	require.Equal(t, `go: latest release connectors/acme/chat/go/v0.3.0 -> v0.4.0
python: latest release connectors/acme/chat/python/v0.1.0 -> v0.1.1
typescript: latest release connectors/acme/chat/typescript/v1.2.3 -> v2.0.0
updated connectors/acme/chat/README.md
updated connectors/acme/chat/connector.yaml
updated connectors/acme/chat/go/examples/send/README.md
updated connectors/acme/chat/python/README.md
updated connectors/acme/chat/python/pyproject.toml
updated connectors/acme/chat/python/src/dex_connectors/acme_chat/dex-connector.json
updated connectors/acme/chat/python/uv.lock
updated connectors/acme/chat/typescript/README.md
updated connectors/acme/chat/typescript/dex-connector.json
updated connectors/acme/chat/typescript/package-lock.json
updated connectors/acme/chat/typescript/package.json
`, output)

	rerunOutput := runVersionBump(t, "--go", "minor", "--python", "patch", "--typescript", "major", connectorPath)
	require.True(t, strings.HasSuffix(rerunOutput, "\nno files changed\n"), rerunOutput)
	requireVersionBumpChatFiles(t, repositoryRoot, bumped)
}

func TestBumpLeavesAnOmittedPackageUnchangedExceptItsGoReleaseTag(t *testing.T) {
	repositoryRoot := writeVersionBumpRepository(t)
	runReleaseCandidateGit(t, repositoryRoot, "tag", versionBumpChat+"/go/v0.1.0")
	connectorPath := filepath.Join(repositoryRoot, filepath.FromSlash(versionBumpChat))

	output := runVersionBump(t, connectorPath, "--go", "patch")

	requireVersionBumpChatFiles(t, repositoryRoot, versionBumpFixtureVersions{goVersion: "v0.1.1", pythonVersion: "v0.1.0", typeScriptVersion: "v0.1.0"})
	require.Contains(t, output, "updated connectors/acme/chat/typescript/dex-connector.json\n")
	require.NotContains(t, output, "package.json")
	require.NotContains(t, output, "pyproject.toml")
}

func TestBumpGoOnlyConnectorFromFirstReleaseThenFromItsTag(t *testing.T) {
	repositoryRoot := writeVersionBumpRepository(t)
	connectorPath := filepath.Join(repositoryRoot, filepath.FromSlash(versionBumpMail))

	output := runVersionBump(t, connectorPath, "--go", "major")
	require.Equal(t, `go: first release -> v0.1.0
updated connectors/acme/mail/README.md
updated connectors/acme/mail/connector.yaml
`, output)
	requireVersionBumpMailFiles(t, repositoryRoot, "v0.1.0")
	require.Equal(t, "go: first release -> v0.1.0\nno files changed\n", runVersionBump(t, connectorPath, "--go", "major"))

	runReleaseCandidateGit(t, repositoryRoot, "commit", "-am", "Release mail v0.1.0")
	runReleaseCandidateGit(t, repositoryRoot, "tag", versionBumpMail+"/go/v0.1.0")
	runVersionBump(t, connectorPath, "--go", "patch")
	requireVersionBumpMailFiles(t, repositoryRoot, "v0.1.1")
	require.Contains(t, runVersionBump(t, connectorPath, "--go", "patch"), "no files changed")
}

func TestBumpGoModuleMigrationRequiresOneMinorStep(t *testing.T) {
	repositoryRoot := writeVersionBumpRepository(t)
	runReleaseCandidateGit(t, repositoryRoot, "tag", versionBumpMail+"/v0.3.0")
	connectorPath := filepath.Join(repositoryRoot, filepath.FromSlash(versionBumpMail))

	require.ErrorContains(t, bumpCommand([]string{connectorPath, "--go", "patch"}, &bytes.Buffer{}),
		"Go module migration requires exactly one minor bump from connectors/acme/mail/v0.3.0, got --go patch")
	runVersionBump(t, connectorPath, "--go", "minor")
	requireVersionBumpMailFiles(t, repositoryRoot, "v0.4.0")
}

func TestBumpWritesNothingWhenTheProposedFilesDoNotValidate(t *testing.T) {
	repositoryRoot := writeVersionBumpRepository(t)
	connectorPath := filepath.Join(repositoryRoot, filepath.FromSlash(versionBumpChat))
	replaceInFile(t, filepath.Join(connectorPath, "python", "pyproject.toml"), `name = "dex-connector-acme-chat"`, `name = "dex-connector-other"`)
	pyproject, err := os.ReadFile(filepath.Join(connectorPath, "python", "pyproject.toml"))
	require.NoError(t, err)

	require.ErrorContains(t, bumpCommand([]string{connectorPath, "--go", "minor"}, &bytes.Buffer{}),
		"bumped files would not validate, so nothing was written")

	runReleaseCandidateGit(t, repositoryRoot, "diff", "--exit-code", "--", ":!"+versionBumpChat+"/python/pyproject.toml")
	current, err := os.ReadFile(filepath.Join(connectorPath, "python", "pyproject.toml"))
	require.NoError(t, err)
	require.Equal(t, string(pyproject), string(current))
}

func TestBumpRestoresEveryFileWhenAReplaceFailsAfterTheTemporaryWrite(t *testing.T) {
	repositoryRoot := writeVersionBumpRepository(t)
	connectorPath := filepath.Join(repositoryRoot, filepath.FromSlash(versionBumpChat))
	runReleaseCandidateGit(t, repositoryRoot, "tag", versionBumpChat+"/typescript/v0.1.0")
	failingPath := filepath.Join(connectorPath, "typescript", "package.json")
	require.NoError(t, os.Chmod(failingPath, 0o640))

	err := runVersionBumpWithRename(t, connectorPath, map[string]string{"go": "minor", "typescript": "minor"}, renameFailingFor(t, failingPath))

	require.ErrorContains(t, err, "write "+failingPath+": injected rename failure")
	requireVersionBumpChatFiles(t, repositoryRoot, versionBumpFixtureVersions{goVersion: "v0.1.0", pythonVersion: "v0.1.0", typeScriptVersion: "v0.1.0"})
	require.Empty(t, runReleaseCandidateGit(t, repositoryRoot, "status", "--porcelain", "--untracked-files=all"))
	info, err := os.Stat(failingPath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o640), info.Mode().Perm())
}

func TestBumpRemovesACreatedFileOnRollback(t *testing.T) {
	repositoryRoot := writeVersionBumpRepository(t)
	connectorPath := filepath.Join(repositoryRoot, filepath.FromSlash(versionBumpChat))
	runReleaseCandidateGit(t, repositoryRoot, "tag", versionBumpChat+"/typescript/v0.1.0")
	createdPath := filepath.Join(connectorPath, "typescript", "dex-connector.json")
	require.NoError(t, os.Remove(createdPath))
	failingPath := filepath.Join(connectorPath, "typescript", "package.json")

	err := runVersionBumpWithRename(t, connectorPath, map[string]string{"typescript": "minor"}, renameFailingFor(t, failingPath))

	require.ErrorContains(t, err, "injected rename failure")
	require.NoFileExists(t, createdPath)
	require.Equal(t, "D "+versionBumpChat+"/typescript/dex-connector.json",
		runReleaseCandidateGit(t, repositoryRoot, "status", "--porcelain", "--untracked-files=all"))
}

func TestBumpRefusesASymlinkedREADMEOutsideTheConnector(t *testing.T) {
	repositoryRoot := writeVersionBumpRepository(t)
	connectorPath := filepath.Join(repositoryRoot, filepath.FromSlash(versionBumpMail))
	runReleaseCandidateGit(t, repositoryRoot, "tag", versionBumpMail+"/go/v0.2.0")
	outsidePath := filepath.Join(repositoryRoot, "outside.md")
	require.NoError(t, os.WriteFile(outsidePath, []byte(versionBumpMailREADME("v0.2.0")), 0o600))
	readmePath := filepath.Join(connectorPath, "docs", "README.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(readmePath), 0o755))
	require.NoError(t, os.Symlink(outsidePath, readmePath))

	require.ErrorContains(t, bumpCommand([]string{connectorPath, "--go", "minor"}, &bytes.Buffer{}),
		"refusing to edit "+readmePath+": it is a symbolic link")

	outside, err := os.ReadFile(outsidePath)
	require.NoError(t, err)
	require.Equal(t, versionBumpMailREADME("v0.2.0"), string(outside))
	requireVersionBumpMailFiles(t, repositoryRoot, "v0.2.0")
}

// renameFailingFor renames every file except failingPath, whose replacement fails after its temporary file is written.
func renameFailingFor(t *testing.T, failingPath string) func(oldPath, newPath string) error {
	t.Helper()
	return func(oldPath, newPath string) error {
		if newPath == failingPath {
			require.FileExists(t, oldPath)
			return errors.New("injected rename failure")
		}
		return os.Rename(oldPath, newPath)
	}
}

func runVersionBumpWithRename(t *testing.T, connectorPath string, levels map[string]string, renameFile func(oldPath, newPath string) error) error {
	t.Helper()
	bump, err := newConnectorVersionBump(connectorPath, newAtomicFileWriter(renameFile))
	require.NoError(t, err)
	return bump.bumpVersions(levels, &bytes.Buffer{})
}

func TestReplaceUVLockPackageVersionEditsOnlyTheRootPackage(t *testing.T) {
	const registryEntry = "[[package]]\nname = \"dex-connector-acme-chat\"\nversion = \"0.1.0\"\nsource = { registry = \"https://pypi.org/simple\" }\n"
	const virtualRoot = "[[package]]\nname = \"dex_connector.acme-chat\"\nversion = \"0.1.0\"\nsource = { virtual = \".\" }\n"

	updated, err := replaceUVLockPackageVersion([]byte("version = 1\n\n"+registryEntry+"\n"+virtualRoot), "dex-connector-acme-chat", "0.2.0")
	require.NoError(t, err)
	require.Equal(t, "version = 1\n\n"+registryEntry+"\n"+strings.Replace(virtualRoot, `"0.1.0"`, `"0.2.0"`, 1), string(updated))

	_, err = replaceUVLockPackageVersion([]byte(registryEntry), "dex-connector-acme-chat", "0.2.0")
	require.ErrorContains(t, err, "must lock root package dex-connector-acme-chat once with an editable or virtual \".\" source and a version, found 0")
	_, err = replaceUVLockPackageVersion([]byte(virtualRoot+"\n"+virtualRoot), "dex-connector-acme-chat", "0.2.0")
	require.ErrorContains(t, err, "found 2")
}

func TestStaleREADMEVersionPinsReportsEveryDeclaredPackage(t *testing.T) {
	repositoryRoot := writeVersionBumpRepository(t)
	connectorPath := filepath.Join(repositoryRoot, filepath.FromSlash(versionBumpChat))
	replaceInFile(t, filepath.Join(connectorPath, "python", "README.md"), "dex-connector-acme-chat==0.1.0", "dex-connector-acme-chat==0.0.9")
	replaceInFile(t, filepath.Join(connectorPath, "typescript", "README.md"), "@superdurable/connector-acme-chat@0.1.0", "@superdurable/connector-acme-chat@0.0.8")

	require.Equal(t, []string{
		filepath.Join(connectorPath, "python", "README.md") + ` pins " dex-connector-acme-chat==0.0.9", not 0.1.0`,
		filepath.Join(connectorPath, "typescript", "README.md") + " pins \"`@superdurable/connector-acme-chat@0.0.8\", not 0.1.0",
	}, staleREADMEVersionPins(t, connectorPath, versionBumpChat))
}

func TestBumpRejectsABranchBehindARelease(t *testing.T) {
	repositoryRoot := writeVersionBumpRepository(t)
	runReleaseCandidateGit(t, repositoryRoot, "tag", versionBumpChat+"/python/v0.1.0")
	runReleaseCandidateGit(t, repositoryRoot, "checkout", "-b", "released")
	runReleaseCandidateGit(t, repositoryRoot, "commit", "--allow-empty", "-m", "Release chat Python v0.2.0")
	runReleaseCandidateGit(t, repositoryRoot, "tag", versionBumpChat+"/python/v0.2.0")
	runReleaseCandidateGit(t, repositoryRoot, "tag", versionBumpChat+"/python/v0.3.0-rc.1")
	runReleaseCandidateGit(t, repositoryRoot, "checkout", "-")
	connectorPath := filepath.Join(repositoryRoot, filepath.FromSlash(versionBumpChat))

	require.ErrorContains(t, bumpCommand([]string{connectorPath, "--python", "minor"}, &bytes.Buffer{}),
		"release tag connectors/acme/chat/python/v0.2.0 is not reachable from HEAD; rebase onto the latest origin/main")
	runVersionBump(t, connectorPath, "--typescript", "minor")
	requireVersionBumpChatFiles(t, repositoryRoot, versionBumpFixtureVersions{goVersion: "v0.1.0", pythonVersion: "v0.1.0", typeScriptVersion: "v0.1.0"})
}

func TestBumpRejectsAGoMajorReleaseWithoutTheMajorVersionModulePath(t *testing.T) {
	repositoryRoot := writeVersionBumpRepository(t)
	runReleaseCandidateGit(t, repositoryRoot, "tag", versionBumpMail+"/go/v1.2.0")
	connectorPath := filepath.Join(repositoryRoot, filepath.FromSlash(versionBumpMail))

	require.ErrorContains(t, bumpCommand([]string{connectorPath, "--go", "major"}, &bytes.Buffer{}),
		"Go v2.0.0 requires the major-version module path example.com/connectors/acme/mail/go/v2")
	requireVersionBumpMailFiles(t, repositoryRoot, "v0.2.0")
}

func TestBumpRejectsAMissingPackageAndInvalidArguments(t *testing.T) {
	repositoryRoot := writeVersionBumpRepository(t)
	connectorPath := filepath.Join(repositoryRoot, filepath.FromSlash(versionBumpMail))

	require.ErrorContains(t, bumpCommand([]string{connectorPath, "--python", "minor"}, &bytes.Buffer{}),
		"connectors/acme/mail has no python package to bump: connector.yaml does not declare spec.languages.python")
	require.ErrorContains(t, bumpCommand([]string{connectorPath}, &bytes.Buffer{}), "at least one of --go, --python, or --typescript")
	require.ErrorContains(t, bumpCommand([]string{connectorPath, "--go", "minor", "extra"}, &bytes.Buffer{}), bumpUsage)
	require.ErrorContains(t, bumpCommand([]string{connectorPath, "--go", "breaking"}, &bytes.Buffer{}), `--go must be patch, minor, or major, got "breaking"`)
	requireVersionBumpMailFiles(t, repositoryRoot, "v0.2.0")
}

func runVersionBump(t *testing.T, args ...string) string {
	t.Helper()
	var output bytes.Buffer
	require.NoError(t, bumpCommand(args, &output))
	return output.String()
}

// writeVersionBumpRepository commits a Go-only mail connector declaring an untagged v0.2.0 and a chat connector
// with Go, Python, and TypeScript packages at v0.1.0, without any release tag.
func writeVersionBumpRepository(t *testing.T) string {
	t.Helper()
	repositoryRoot := t.TempDir()
	writeFixtureCatalog(t, repositoryRoot, []string{versionBumpChat, versionBumpMail})
	writeConnectorFixture(t, repositoryRoot, versionBumpMail, "example.com/"+versionBumpMail)
	writeConnectorFixture(t, repositoryRoot, versionBumpChat, "example.com/"+versionBumpChat)
	mailPath := filepath.Join(repositoryRoot, filepath.FromSlash(versionBumpMail))
	replaceInFile(t, filepath.Join(mailPath, "connector.yaml"), "  version: v0.1.0\n", "  version: v0.2.0\n")
	require.NoError(t, os.WriteFile(filepath.Join(mailPath, "README.md"), []byte(versionBumpMailREADME("v0.2.0")), 0o600))
	replaceInFile(t, filepath.Join(repositoryRoot, filepath.FromSlash(versionBumpChat), "connector.yaml"), "  provider: google\n",
		"  provider: google\n  languages:\n    python:\n      version: v0.1.0\n      operations: [appendRows]\n      triggers: []\n"+
			"    typescript:\n      version: v0.1.0\n      operations: [appendRows]\n      triggers: []\n")
	initial := versionBumpFixtureVersions{goVersion: "v0.1.0", pythonVersion: "v0.1.0", typeScriptVersion: "v0.1.0"}
	for path, contents := range versionBumpChatFiles(initial) {
		fullPath := filepath.Join(repositoryRoot, filepath.FromSlash(path))
		require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o755))
		require.NoError(t, os.WriteFile(fullPath, []byte(contents), 0o600))
	}
	manifestPath := filepath.Join(repositoryRoot, filepath.FromSlash(versionBumpChat), "connector.yaml")
	manifest, err := load(manifestPath)
	require.NoError(t, err)
	generated, err := generatedLanguageManifestFiles(manifestPath, manifest, newConnectorGoPackage(manifestPath))
	require.NoError(t, err)
	require.Len(t, generated, 2)
	require.NoError(t, writeGeneratedFiles(generated))
	for _, args := range [][]string{
		{"init"},
		{"config", "user.name", "Connector version bump test"},
		{"config", "user.email", "connector-version-bump-test@example.com"},
		{"config", "commit.gpgsign", "false"},
		{"add", "."},
		{"commit", "-m", "Add connector version bump fixtures"},
	} {
		runReleaseCandidateGit(t, repositoryRoot, args...)
	}
	return repositoryRoot
}

func versionBumpMailREADME(goVersion string) string {
	return "# Mail\n\nInstall with `go get example.com/" + versionBumpMail + "/go@" + goVersion + "`.\n" +
		"Version v0.0.9 predates the move and stays as history.\n"
}

// versionBumpChatFiles are the chat files at versions; SDK pins share the initial version and must not change.
func versionBumpChatFiles(versions versionBumpFixtureVersions) map[string]string {
	pythonVersion := strings.TrimPrefix(versions.pythonVersion, "v")
	typeScriptVersion := strings.TrimPrefix(versions.typeScriptVersion, "v")
	return map[string]string{
		versionBumpChat + "/README.md": "# Chat\n\nInstall with `go get example.com/" + versionBumpChat + "/go@" + versions.goVersion + "`.\n" +
			"The Python package is `dex-connector-acme-chat==" + pythonVersion + "` and the TypeScript package is\n" +
			"`@superdurable/connector-acme-chat@" + typeScriptVersion + "`. Version 0.1.0 is described as history.\n" +
			"Candidates `" + versionBumpChat + "/go/v0.1.0-rc.1`, `dex-connector-acme-chat==0.1.0-rc.1`, and\n" +
			"`@superdurable/connector-acme-chat@0.1.0+build.1` name other releases.\n" +
			"The SDK is `dex-connectors-sdk==0.1.0` and `@superdurable/connectors-sdk@0.1.0`.\n",
		versionBumpChat + "/go/examples/send/README.md": "# Send\n\n```bash\ngo run ./cmd/connectorctl release-artifact \\\n" +
			"  --version " + versions.goVersion + " --tag " + versionBumpChat + "/go/" + versions.goVersion + " \\\n" +
			"  --source-sha \"$(git rev-parse HEAD)\"\n```\n",
		versionBumpChat + "/python/README.md": "# Chat for Python\n\nInstall with `pip install dex-connector-acme-chat==" + pythonVersion + "`.\n",
		versionBumpChat + "/python/pyproject.toml": "[build-system]\nrequires = [\"hatchling\"]\n\n[project]\n" +
			"name = \"dex-connector-acme-chat\"\nversion = \"" + pythonVersion + "\"  # release version\n" +
			"dependencies = [\"dex-connectors-sdk==0.1.0\"]\n\n[tool.example]\nversion = \"9.9.9\"\n",
		versionBumpChat + "/python/uv.lock": "version = 1\nrevision = 3\n\n[[package]]\nname = \"dex-connector-acme-chat\"\n" +
			"version = \"" + pythonVersion + "\"\nsource = { editable = \".\" }\ndependencies = [\n    { name = \"dex-connectors-sdk\" },\n]\n\n" +
			"[package.metadata]\nrequires-dist = [{ name = \"dex-connectors-sdk\", specifier = \"==0.1.0\" }]\n\n" +
			"[[package]]\nname = \"dex-connectors-sdk\"\nversion = \"0.1.0\"\nsource = { registry = \"https://pypi.org/simple\" }\n",
		versionBumpChat + "/typescript/README.md": "# Chat for TypeScript\n\nInstall `@superdurable/connector-acme-chat@" + typeScriptVersion + "`.\n",
		versionBumpChat + "/typescript/package.json": "{\n  \"name\": \"@superdurable/connector-acme-chat\",\n" +
			"  \"version\": \"" + typeScriptVersion + "\",\n  \"license\": \"MIT\",\n" +
			"  \"dependencies\": { \"@superdurable/connectors-sdk\": \"0.1.0\" },\n  \"engines\": {\"node\":\">=22\", \"version\": \"0.1.0\"}\n}\n",
		versionBumpChat + "/typescript/package-lock.json": "{\n  \"name\": \"@superdurable/connector-acme-chat\",\n" +
			"  \"version\": \"" + typeScriptVersion + "\",\n  \"lockfileVersion\": 3,\n  \"requires\": true,\n  \"packages\": {\n" +
			"    \"\": {\n      \"name\": \"@superdurable/connector-acme-chat\",\n      \"version\": \"" + typeScriptVersion + "\",\n" +
			"      \"dependencies\": {\n        \"@superdurable/connectors-sdk\": \"0.1.0\"\n      }\n    },\n" +
			"    \"node_modules/@superdurable/connectors-sdk\": {\n      \"version\": \"0.1.0\"\n    }\n  }\n}\n",
	}
}

// requireVersionBumpChatFiles checks the chat files byte for byte and as the release validators do.
func requireVersionBumpChatFiles(t *testing.T, repositoryRoot string, versions versionBumpFixtureVersions) {
	t.Helper()
	for path, expected := range versionBumpChatFiles(versions) {
		contents, err := os.ReadFile(filepath.Join(repositoryRoot, filepath.FromSlash(path)))
		require.NoError(t, err)
		require.Equal(t, expected, string(contents), path)
	}
	connectorPath := filepath.Join(repositoryRoot, filepath.FromSlash(versionBumpChat))
	manifestPath := filepath.Join(connectorPath, "connector.yaml")
	manifest := requireBumpedManifest(t, manifestPath, versions.goVersion)
	require.Equal(t, versions.pythonVersion, manifest.Spec.Languages.Python.Version)
	require.Equal(t, versions.typeScriptVersion, manifest.Spec.Languages.TypeScript.Version)
	generated, err := generatedLanguageManifestFiles(manifestPath, manifest, newConnectorGoPackage(manifestPath))
	require.NoError(t, err)
	require.NoError(t, checkGeneratedFiles(generated))
	for path, expectedVersion := range map[string]string{
		filepath.Join(connectorPath, "python", "src", "dex_connectors", "acme_chat", "dex-connector.json"): versions.pythonVersion,
		filepath.Join(connectorPath, "typescript", "dex-connector.json"):                                   versions.typeScriptVersion,
	} {
		contents, err := os.ReadFile(path)
		require.NoError(t, err)
		var packageManifest dexConnectorPackageManifest
		require.NoError(t, json.Unmarshal(contents, &packageManifest))
		require.Equal(t, versionBumpChat+"/go/"+versions.goVersion, packageManifest.ReleaseTag)
		require.Equal(t, strings.TrimPrefix(expectedVersion, "v"), packageManifest.Package.Version)
	}
	require.Empty(t, staleREADMEVersionPins(t, connectorPath, versionBumpChat))
}

func requireVersionBumpMailFiles(t *testing.T, repositoryRoot, goVersion string) {
	t.Helper()
	connectorPath := filepath.Join(repositoryRoot, filepath.FromSlash(versionBumpMail))
	contents, err := os.ReadFile(filepath.Join(connectorPath, "README.md"))
	require.NoError(t, err)
	require.Equal(t, versionBumpMailREADME(goVersion), string(contents))
	requireBumpedManifest(t, filepath.Join(connectorPath, "connector.yaml"), goVersion)
	require.Empty(t, staleREADMEVersionPins(t, connectorPath, versionBumpMail))
}

// requireBumpedManifest also checks the unquoted metadata.version line that validate-language-package reads.
func requireBumpedManifest(t *testing.T, manifestPath, goVersion string) schema.Manifest {
	t.Helper()
	manifest, err := load(manifestPath)
	require.NoError(t, err)
	require.Equal(t, goVersion, manifest.Metadata.Version)
	contents, err := os.ReadFile(manifestPath)
	require.NoError(t, err)
	require.Contains(t, string(contents), "\n  version: "+goVersion+"\n")
	return manifest
}
