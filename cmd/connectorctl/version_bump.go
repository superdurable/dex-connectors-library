// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/superdurable/dex-connectors-library/schema"
)

const bumpUsage = "usage: connectorctl bump <connector directory> [--go patch|minor|major] [--python patch|minor|major] [--typescript patch|minor|major]"

// readmeVersionSuffix captures a prerelease or build suffix, and otherwise ends the version at a word boundary.
const readmeVersionSuffix = `((?:[-+][0-9A-Za-z.-]*[0-9A-Za-z])?)\b`

// connectorPackageLanguages lists the packages of a connector in the order bump plans and reports them.
var connectorPackageLanguages = []string{"go", "python", "typescript"}

var (
	uvLockPackageName    = regexp.MustCompile(`^name = "([^"]+)"$`)
	uvLockPackageVersion = regexp.MustCompile(`^version = "([^"]+)"$`)
	// uvLockRootPackageSource matches the source of the project that uv.lock locks, not of a dependency.
	uvLockRootPackageSource = regexp.MustCompile(`^source = \{ (?:editable|virtual) = "\." \}$`)
	pythonNameSeparators    = regexp.MustCompile(`[-_.]+`)
)

// connectorVersionBump plans and writes one connector's next release versions.
type connectorVersionBump struct {
	// connectorPath is the connector directory as the caller named it.
	connectorPath string
	// directory is the connector directory relative to the repository root, such as connectors/datadog.
	directory      string
	repositoryRoot string
	manifestPath   string
	manifest       schema.Manifest
	fileWriter     *atomicFileWriter
}

// packageVersionTarget is the next release version of one connector package.
type packageVersionTarget struct {
	language      string
	latestRelease reachableConnectorRelease
	target        string
}

// readmeVersionPin is one version that a connector README pins for one of the connector's packages.
type readmeVersionPin struct {
	path       string
	text       string
	version    string
	start, end int
}

// uvLockPackageBlock is the identity of one uv.lock [[package]] entry.
type uvLockPackageBlock struct {
	name         string
	versionLine  int
	isRootSource bool
}

// pendingFileEdits accumulates file contents in memory so a failed bump writes nothing.
type pendingFileEdits struct {
	original map[string][]byte
	updated  map[string][]byte
	// missing holds the paths that did not exist; only a generated file may create one.
	missing    map[string]bool
	fileWriter *atomicFileWriter
}

func newPendingFileEdits(fileWriter *atomicFileWriter) *pendingFileEdits {
	if fileWriter == nil {
		panic("pending file edits require a file writer")
	}
	return &pendingFileEdits{
		original: make(map[string][]byte), updated: make(map[string][]byte), missing: make(map[string]bool),
		fileWriter: fileWriter,
	}
}

// atomicFileWriter replaces a file through a temporary sibling, so a failed write leaves the original intact.
type atomicFileWriter struct {
	// renameFile is os.Rename outside tests; a test injects a failure after the temporary file is written.
	renameFile func(oldPath, newPath string) error
}

func newAtomicFileWriter(renameFile func(oldPath, newPath string) error) *atomicFileWriter {
	if renameFile == nil {
		panic("atomic file writer requires a rename function")
	}
	return &atomicFileWriter{renameFile: renameFile}
}

// jsonStringValueLocator finds the byte span, quotes excluded, of the unescaped string at keyPath.
type jsonStringValueLocator struct {
	decoder    *json.Decoder
	contents   []byte
	keyPath    []string
	matches    int
	start, end int
}

func bumpCommand(args []string, output io.Writer) error {
	connectorPath, levels, err := parseBumpArguments(args)
	if err != nil {
		return err
	}
	bump, err := newConnectorVersionBump(connectorPath, newAtomicFileWriter(os.Rename))
	if err != nil {
		return err
	}
	return bump.bumpVersions(levels, output)
}

// parseBumpArguments accepts the connector directory before or after the flags.
func parseBumpArguments(args []string) (string, map[string]string, error) {
	flags := flag.NewFlagSet("bump", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	requestedLevels := make(map[string]*string, len(connectorPackageLanguages))
	for _, language := range connectorPackageLanguages {
		requestedLevels[language] = flags.String(language, "", "next "+language+" package release: patch, minor, or major")
	}
	connectorPath := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		connectorPath, args = args[0], args[1:]
	}
	if err := flags.Parse(args); err != nil {
		return "", nil, fmt.Errorf("%w\n%s", err, bumpUsage)
	}
	if connectorPath == "" && flags.NArg() == 1 {
		connectorPath = flags.Arg(0)
	} else if flags.NArg() != 0 {
		return "", nil, errors.New(bumpUsage)
	}
	if connectorPath == "" {
		return "", nil, errors.New(bumpUsage)
	}
	levels := make(map[string]string)
	for _, language := range connectorPackageLanguages {
		level := *requestedLevels[language]
		if level == "" {
			continue
		}
		if level != "patch" && level != "minor" && level != "major" {
			return "", nil, fmt.Errorf("--%s must be patch, minor, or major, got %q", language, level)
		}
		levels[language] = level
	}
	if len(levels) == 0 {
		return "", nil, fmt.Errorf("bump requires at least one of --go, --python, or --typescript\n%s", bumpUsage)
	}
	return connectorPath, levels, nil
}

func newConnectorVersionBump(connectorPath string, fileWriter *atomicFileWriter) (*connectorVersionBump, error) {
	if fileWriter == nil {
		return nil, errors.New("connector version bump requires a file writer")
	}
	connectorPath = filepath.Clean(connectorPath)
	manifestPath := filepath.Join(connectorPath, "connector.yaml")
	file, err := os.Open(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", manifestPath, err)
	}
	// Language package metadata is checked after the bump, so a half-edited branch can still be bumped.
	manifest, err := schema.Decode(file)
	closeErr := file.Close()
	if err != nil {
		return nil, errors.Join(fmt.Errorf("validate %s: %w", manifestPath, err), closeErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close %s: %w", manifestPath, closeErr)
	}
	directory, err := connectorDirectoryInCatalog(manifestPath)
	if err != nil {
		return nil, err
	}
	absoluteConnectorPath, err := filepath.Abs(connectorPath)
	if err != nil {
		return nil, err
	}
	repositoryRoot := filepath.Clean(strings.TrimSuffix(absoluteConnectorPath, filepath.FromSlash(directory)))
	return &connectorVersionBump{
		connectorPath: connectorPath, directory: directory, repositoryRoot: repositoryRoot,
		manifestPath: manifestPath, manifest: manifest, fileWriter: fileWriter,
	}, nil
}

// bumpVersions writes every version file and generated language package manifest that the new versions change.
func (bump *connectorVersionBump) bumpVersions(levels map[string]string, output io.Writer) error {
	targets, err := bump.planTargets(levels)
	if err != nil {
		return err
	}
	for _, target := range targets {
		latest := "first release"
		if target.latestRelease.Tag != "" {
			latest = "latest release " + target.latestRelease.Tag
		}
		if _, err := fmt.Fprintf(output, "%s: %s -> %s\n", target.language, latest, target.target); err != nil {
			return err
		}
	}
	edits, err := bump.updatedFiles(targets)
	if err != nil {
		return err
	}
	if err := bump.validateProposedFiles(edits); err != nil {
		return fmt.Errorf("bumped files would not validate, so nothing was written: %w", err)
	}
	if err := bump.requireFilesInsideConnector(edits); err != nil {
		return err
	}
	writtenPaths, err := edits.writeChangedFiles()
	if err != nil {
		return err
	}
	for _, path := range writtenPaths {
		absolutePath, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(bump.repositoryRoot, absolutePath)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(output, "updated %s\n", filepath.ToSlash(relative)); err != nil {
			return err
		}
	}
	if len(writtenPaths) == 0 {
		_, err := fmt.Fprintln(output, "no files changed")
		return err
	}
	return nil
}

// requireFilesInsideConnector refuses, before any write, a symlinked target or one that resolves outside the connector.
func (bump *connectorVersionBump) requireFilesInsideConnector(edits *pendingFileEdits) error {
	connectorRoot, err := filepath.EvalSymlinks(bump.connectorPath)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", bump.connectorPath, err)
	}
	for path := range edits.changedFiles() {
		if info, err := os.Lstat(path); err == nil && info.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("refusing to edit %s: it is a symbolic link; replace it with a regular file", path)
		} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("inspect %s: %w", path, err)
		}
		directory, err := bump.nearestExistingDirectory(filepath.Dir(path))
		if err != nil {
			return err
		}
		resolvedDirectory, err := filepath.EvalSymlinks(directory)
		if err != nil {
			return fmt.Errorf("resolve %s: %w", directory, err)
		}
		if relative, err := filepath.Rel(connectorRoot, resolvedDirectory); err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return fmt.Errorf("refusing to edit %s: it resolves outside %s", path, bump.connectorPath)
		}
	}
	return nil
}

// nearestExistingDirectory returns directory or its closest existing ancestor, for a file the bump creates.
func (bump *connectorVersionBump) nearestExistingDirectory(directory string) (string, error) {
	for {
		if _, err := os.Lstat(directory); err == nil {
			return directory, nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("inspect %s: %w", directory, err)
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return directory, nil
		}
		directory = parent
	}
}

// validateProposedFiles runs load on a scratch copy holding the proposed manifest and package metadata.
func (bump *connectorVersionBump) validateProposedFiles(edits *pendingFileEdits) error {
	scratchRoot, err := os.MkdirTemp("", "connectorctl-bump-")
	if err != nil {
		return err
	}
	// A leftover scratch directory is harmless, so a failed removal does not fail the bump.
	defer func() { _ = os.RemoveAll(scratchRoot) }()
	if err := os.WriteFile(filepath.Join(scratchRoot, "catalog.yaml"), nil, 0o600); err != nil {
		return err
	}
	scratchConnector := filepath.Join(scratchRoot, filepath.FromSlash(bump.directory))
	for _, relative := range []string{"connector.yaml", filepath.Join("python", "pyproject.toml"), filepath.Join("typescript", "package.json")} {
		contents, err := edits.contents(filepath.Join(bump.connectorPath, relative))
		if err != nil {
			return err
		}
		if contents == nil {
			continue
		}
		scratchPath := filepath.Join(scratchConnector, relative)
		if err := os.MkdirAll(filepath.Dir(scratchPath), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(scratchPath, contents, 0o600); err != nil {
			return err
		}
	}
	_, err = load(filepath.Join(scratchConnector, "connector.yaml"))
	return err
}

// planTargets computes each next version from the latest reachable release tag, never from the manifest.
func (bump *connectorVersionBump) planTargets(levels map[string]string) ([]packageVersionTarget, error) {
	for _, language := range connectorPackageLanguages {
		if _, isRequested := levels[language]; isRequested {
			if err := bump.requirePackage(language); err != nil {
				return nil, err
			}
		}
	}
	latestReleases, err := latestReachableConnectorReleases(bump.repositoryRoot)
	if err != nil {
		return nil, err
	}
	targets := make([]packageVersionTarget, 0, len(levels))
	for _, language := range connectorPackageLanguages {
		level, isRequested := levels[language]
		if !isRequested {
			continue
		}
		latestRelease := latestReleases[bump.directory+"/"+language]
		if err := bump.requireNoUnreachableRelease(language, latestRelease); err != nil {
			return nil, err
		}
		target := packageVersionTarget{language: language, latestRelease: latestRelease, target: "v0.1.0"}
		if latestRelease.Version != "" {
			isGoModuleMigration := language == "go" && !strings.HasPrefix(latestRelease.Tag, bump.directory+"/go/")
			if isGoModuleMigration && level != "minor" {
				return nil, fmt.Errorf("%s Go module migration requires exactly one minor bump from %s, got --go %s", bump.directory, latestRelease.Tag, level)
			}
			target.target = nextConnectorVersion(latestRelease.Version, level)
			if err := bump.requireGoMajorVersionModulePath(language, target.target); err != nil {
				return nil, err
			}
		}
		targets = append(targets, target)
	}
	return targets, nil
}

// requireNoUnreachableRelease rejects a branch behind a release, whose next version may already be released.
func (bump *connectorVersionBump) requireNoUnreachableRelease(language string, latestReachableRelease reachableConnectorRelease) error {
	tagPrefixes := []string{bump.directory + "/" + language + "/"}
	if language == "go" {
		tagPrefixes = append(tagPrefixes, bump.directory+"/")
	}
	for _, tagPrefix := range tagPrefixes {
		command := exec.Command("git", "tag", "--list", tagPrefix+"v*")
		command.Dir = bump.repositoryRoot
		output, err := command.Output()
		if err != nil {
			return fmt.Errorf("list %s release tags: %w", tagPrefix, err)
		}
		for _, tag := range strings.Fields(string(output)) {
			version := strings.TrimPrefix(tag, tagPrefix)
			if !connectorVersionPattern.MatchString(version) {
				continue
			}
			if latestReachableRelease.Version == "" || compareConnectorVersions(version, latestReachableRelease.Version) > 0 {
				return fmt.Errorf("release tag %s is not reachable from HEAD; rebase onto the latest origin/main before bumping", tag)
			}
		}
	}
	return nil
}

// requireGoMajorVersionModulePath rejects a Go v2+ release until go.mod has its major-version module path.
func (bump *connectorVersionBump) requireGoMajorVersionModulePath(language, target string) error {
	major := connectorVersionParts(target)[0]
	if language != "go" || major < 2 {
		return nil
	}
	goModPath := filepath.Join(bump.connectorPath, "go", "go.mod")
	modulePath, err := readModulePath(goModPath)
	if err != nil {
		return err
	}
	if suffix := fmt.Sprintf("/v%d", major); !strings.HasSuffix(modulePath, suffix) {
		return fmt.Errorf("%s Go %s requires the major-version module path %s%s in %s first; bump does not migrate it", bump.directory, target, modulePath, suffix, goModPath)
	}
	return nil
}

func (bump *connectorVersionBump) requirePackage(language string) error {
	if language == "go" {
		goModPath := filepath.Join(bump.connectorPath, "go", "go.mod")
		if _, err := os.Stat(goModPath); err != nil {
			return fmt.Errorf("%s has no Go package to bump: %w", bump.directory, err)
		}
		return nil
	}
	if bump.languageDeclaration(language) == nil {
		return fmt.Errorf("%s has no %s package to bump: connector.yaml does not declare spec.languages.%s", bump.directory, language, language)
	}
	return nil
}

func (bump *connectorVersionBump) languageDeclaration(language string) *schema.LanguagePackage {
	if bump.manifest.Spec.Languages == nil {
		return nil
	}
	switch language {
	case "python":
		return bump.manifest.Spec.Languages.Python
	case "typescript":
		return bump.manifest.Spec.Languages.TypeScript
	}
	return nil
}

// updatedFiles computes the new contents of every version file in memory.
func (bump *connectorVersionBump) updatedFiles(targets []packageVersionTarget) (*pendingFileEdits, error) {
	edits := newPendingFileEdits(bump.fileWriter)
	manifestVersions := make(map[string]string, len(targets))
	for _, target := range targets {
		manifestVersions[target.language] = target.target
	}
	if err := edits.edit(bump.manifestPath, func(contents []byte) ([]byte, error) {
		return replaceManifestVersions(contents, manifestVersions)
	}); err != nil {
		return nil, err
	}
	readmeVersions := make(map[string]string, len(targets))
	for _, target := range targets {
		packageVersion := target.target
		if target.language != "go" {
			packageVersion = strings.TrimPrefix(packageVersion, "v")
			if err := bump.editLanguagePackageFiles(edits, target.language, packageVersion); err != nil {
				return nil, err
			}
		}
		readmeVersions[target.language] = packageVersion
	}
	if err := bump.editREADMEPins(edits, readmeVersions); err != nil {
		return nil, err
	}
	if err := bump.regenerateLanguageManifests(edits); err != nil {
		return nil, err
	}
	return edits, nil
}

func (bump *connectorVersionBump) editLanguagePackageFiles(edits *pendingFileEdits, language, version string) error {
	packageDirectory := filepath.Join(bump.connectorPath, language)
	if language == "python" {
		packageName := languagePackageName(bump.directory, "python", bump.languageDeclaration("python").Package)
		if err := edits.edit(filepath.Join(packageDirectory, "pyproject.toml"), func(contents []byte) ([]byte, error) {
			return replacePyprojectVersion(contents, version)
		}); err != nil {
			return err
		}
		return edits.edit(filepath.Join(packageDirectory, "uv.lock"), func(contents []byte) ([]byte, error) {
			return replaceUVLockPackageVersion(contents, packageName, version)
		})
	}
	if err := edits.edit(filepath.Join(packageDirectory, "package.json"), func(contents []byte) ([]byte, error) {
		return replaceJSONStringValues(contents, version, []string{"version"})
	}); err != nil {
		return err
	}
	return edits.edit(filepath.Join(packageDirectory, "package-lock.json"), func(contents []byte) ([]byte, error) {
		return replaceJSONStringValues(contents, version, []string{"version"}, []string{"packages", "", "version"})
	})
}

// editREADMEPins rewrites every README pin of a bumped package, keyed by language, to its new version.
func (bump *connectorVersionBump) editREADMEPins(edits *pendingFileEdits, versions map[string]string) error {
	packageNames := make(map[string]string)
	for _, language := range []string{"python", "typescript"} {
		if declaration := bump.languageDeclaration(language); declaration != nil {
			packageNames[language] = languagePackageName(bump.directory, language, declaration.Package)
		}
	}
	readmePaths, nestedConnectorDirectories, err := connectorREADMEPaths(bump.connectorPath)
	if err != nil {
		return err
	}
	for _, path := range readmePaths {
		contents, err := edits.contents(path)
		if err != nil {
			return err
		}
		edits.set(path, bump.replaceREADMEPins(path, contents, versions, packageNames, nestedConnectorDirectories))
	}
	return nil
}

// replaceREADMEPins replaces from the end so earlier pin offsets stay valid.
func (bump *connectorVersionBump) replaceREADMEPins(path string, contents []byte, versions, packageNames map[string]string, nestedConnectorDirectories []string) []byte {
	var pins []readmeVersionPin
	for language, version := range versions {
		for _, pin := range findREADMEVersionPins(path, contents, bump.directory, nestedConnectorDirectories, language, packageNames[language]) {
			if pin.version != version {
				pin.version = version
				pins = append(pins, pin)
			}
		}
	}
	sort.Slice(pins, func(i, j int) bool { return pins[i].start > pins[j].start })
	for _, pin := range pins {
		contents = slices.Concat(contents[:pin.start], []byte(pin.version), contents[pin.end:])
	}
	return contents
}

// regenerateLanguageManifests runs the dex-connector.json generator on the bumped manifest, which embeds the versions.
func (bump *connectorVersionBump) regenerateLanguageManifests(edits *pendingFileEdits) error {
	if bump.manifest.Spec.Languages == nil || (bump.manifest.Spec.Languages.Python == nil && bump.manifest.Spec.Languages.TypeScript == nil) {
		return nil
	}
	manifestContents, err := edits.contents(bump.manifestPath)
	if err != nil {
		return err
	}
	manifest, err := schema.Decode(bytes.NewReader(manifestContents))
	if err != nil {
		return fmt.Errorf("validate bumped %s: %w", bump.manifestPath, err)
	}
	generated, err := generatedLanguageManifestFiles(bump.manifestPath, manifest, newConnectorGoPackage(bump.manifestPath))
	if err != nil {
		return fmt.Errorf("generate language package manifests: %w", err)
	}
	for path, contents := range generated {
		if _, err := edits.contents(path); err != nil {
			return err
		}
		edits.set(path, contents)
	}
	return nil
}

func (edits *pendingFileEdits) contents(path string) ([]byte, error) {
	if contents, isLoaded := edits.updated[path]; isLoaded {
		return contents, nil
	}
	contents, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		edits.missing[path] = true
	} else if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	edits.original[path] = contents
	edits.updated[path] = contents
	return contents, nil
}

func (edits *pendingFileEdits) edit(path string, change func([]byte) ([]byte, error)) error {
	contents, err := edits.contents(path)
	if err != nil {
		return err
	}
	if edits.missing[path] {
		return fmt.Errorf("read %s: %w", path, fs.ErrNotExist)
	}
	updated, err := change(contents)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	edits.set(path, updated)
	return nil
}

// set records new contents for a path that contents has loaded.
func (edits *pendingFileEdits) set(path string, contents []byte) {
	edits.updated[path] = contents
}

// writeChangedFiles writes every changed file in path order and restores the written ones if a write fails.
func (edits *pendingFileEdits) writeChangedFiles() ([]string, error) {
	changed := edits.changedFiles()
	paths := make([]string, 0, len(changed))
	for path := range changed {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for index, path := range paths {
		if err := edits.fileWriter.replaceFile(path, changed[path]); err != nil {
			return nil, errors.Join(fmt.Errorf("write %s: %w", path, err), edits.restoreOriginals(paths[:index]))
		}
	}
	return paths, nil
}

func (edits *pendingFileEdits) restoreOriginals(paths []string) error {
	var restoreErrors []error
	for _, path := range paths {
		var err error
		if edits.missing[path] {
			err = os.Remove(path)
		} else {
			err = edits.fileWriter.replaceFile(path, edits.original[path])
		}
		if err != nil {
			restoreErrors = append(restoreErrors, fmt.Errorf("restore %s: %w", path, err))
		}
	}
	return errors.Join(restoreErrors...)
}

// replaceFile keeps an existing file's permissions and creates missing parent directories.
func (writer *atomicFileWriter) replaceFile(path string, contents []byte) error {
	mode := fs.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".bump-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	_, writeErr := temporary.Write(contents)
	closeErr := temporary.Close()
	err = errors.Join(writeErr, closeErr)
	if err == nil {
		err = os.Chmod(temporaryPath, mode)
	}
	if err == nil {
		err = writer.renameFile(temporaryPath, path)
	}
	if err != nil {
		if removeErr := os.Remove(temporaryPath); removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) {
			err = errors.Join(err, fmt.Errorf("remove %s: %w", temporaryPath, removeErr))
		}
		return err
	}
	return nil
}

func (edits *pendingFileEdits) changedFiles() map[string][]byte {
	changed := make(map[string][]byte)
	for path, contents := range edits.updated {
		if isCreated := edits.missing[path] && contents != nil; isCreated || !bytes.Equal(contents, edits.original[path]) {
			changed[path] = contents
		}
	}
	return changed
}

func nextConnectorVersion(baseline, level string) string {
	parts := connectorVersionParts(baseline)
	switch level {
	case "major":
		return fmt.Sprintf("v%d.0.0", parts[0]+1)
	case "minor":
		return fmt.Sprintf("v%d.%d.0", parts[0], parts[1]+1)
	}
	return fmt.Sprintf("v%d.%d.%d", parts[0], parts[1], parts[2]+1)
}

// replaceManifestVersions sets metadata.version for go and spec.languages.<language>.version for the others.
func replaceManifestVersions(contents []byte, versions map[string]string) ([]byte, error) {
	var document yaml.Node
	if err := yaml.Unmarshal(contents, &document); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	if len(document.Content) != 1 {
		return nil, errors.New("manifest is not one YAML document")
	}
	lines := strings.SplitAfter(string(contents), "\n")
	for _, language := range connectorPackageLanguages {
		version, isRequested := versions[language]
		if !isRequested {
			continue
		}
		keyPath := []string{"metadata", "version"}
		if language != "go" {
			keyPath = []string{"spec", "languages", language, "version"}
		}
		node := yamlMappingValue(document.Content[0], keyPath)
		if node == nil || node.Kind != yaml.ScalarNode || node.Style != 0 {
			return nil, fmt.Errorf("manifest has no unquoted %s", strings.Join(keyPath, "."))
		}
		line := lines[node.Line-1]
		valueStart := node.Column - 1
		if !strings.HasPrefix(line[valueStart:], node.Value) {
			return nil, fmt.Errorf("manifest %s is not on line %d", strings.Join(keyPath, "."), node.Line)
		}
		lines[node.Line-1] = line[:valueStart] + version + line[valueStart+len(node.Value):]
	}
	return []byte(strings.Join(lines, "")), nil
}

func yamlMappingValue(node *yaml.Node, keyPath []string) *yaml.Node {
	for _, key := range keyPath {
		if node.Kind != yaml.MappingNode {
			return nil
		}
		var value *yaml.Node
		for index := 0; index+1 < len(node.Content); index += 2 {
			if node.Content[index].Value == key {
				value = node.Content[index+1]
			}
		}
		if value == nil {
			return nil
		}
		node = value
	}
	return node
}

// replacePyprojectVersion edits the [project] version line that validateLanguagePackages reads.
func replacePyprojectVersion(contents []byte, version string) ([]byte, error) {
	lines := strings.SplitAfter(string(contents), "\n")
	isInProject := false
	replacements := 0
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			isInProject = trimmed == "[project]"
		}
		if !isInProject {
			continue
		}
		body := strings.TrimSuffix(line, "\n")
		field := pythonProjectField.FindStringSubmatchIndex(body)
		if field == nil || body[field[2]:field[3]] != "version" {
			continue
		}
		lines[index] = body[:field[4]] + version + body[field[5]:] + line[len(body):]
		replacements++
	}
	if replacements != 1 {
		return nil, fmt.Errorf("[project] must declare one version, found %d", replacements)
	}
	return []byte(strings.Join(lines, "")), nil
}

// replaceUVLockPackageVersion edits the version of the editable or virtual root package, never a dependency.
func replaceUVLockPackageVersion(contents []byte, packageName, version string) ([]byte, error) {
	lines := strings.SplitAfter(string(contents), "\n")
	var blocks []uvLockPackageBlock
	var current *uvLockPackageBlock
	for index, line := range lines {
		body := strings.TrimSuffix(line, "\n")
		if strings.HasPrefix(body, "[") {
			current = nil
			if body == "[[package]]" {
				blocks = append(blocks, uvLockPackageBlock{versionLine: -1})
				current = &blocks[len(blocks)-1]
			}
			continue
		}
		if current == nil {
			continue
		}
		if name := uvLockPackageName.FindStringSubmatch(body); name != nil {
			current.name = name[1]
		} else if uvLockPackageVersion.MatchString(body) {
			current.versionLine = index
		} else if uvLockRootPackageSource.MatchString(body) {
			current.isRootSource = true
		}
	}
	var rootBlocks []uvLockPackageBlock
	for _, block := range blocks {
		if block.isRootSource && canonicalPythonDistributionName(block.name) == canonicalPythonDistributionName(packageName) {
			rootBlocks = append(rootBlocks, block)
		}
	}
	if len(rootBlocks) != 1 || rootBlocks[0].versionLine < 0 {
		return nil, fmt.Errorf("uv.lock must lock root package %s once with an editable or virtual \".\" source and a version, found %d", packageName, len(rootBlocks))
	}
	index := rootBlocks[0].versionLine
	body := strings.TrimSuffix(lines[index], "\n")
	field := uvLockPackageVersion.FindStringSubmatchIndex(body)
	lines[index] = body[:field[2]] + version + body[field[3]:] + lines[index][len(body):]
	return []byte(strings.Join(lines, "")), nil
}

func canonicalPythonDistributionName(name string) string {
	return strings.ToLower(pythonNameSeparators.ReplaceAllString(name, "-"))
}

// replaceJSONStringValues sets the string value at each key path in place, keeping the rest of the document's bytes.
func replaceJSONStringValues(contents []byte, value string, keyPaths ...[]string) ([]byte, error) {
	type valueSpan struct{ start, end int }
	spans := make([]valueSpan, 0, len(keyPaths))
	for _, keyPath := range keyPaths {
		locator := jsonStringValueLocator{decoder: json.NewDecoder(bytes.NewReader(contents)), contents: contents, keyPath: keyPath}
		if err := locator.readValue(nil); err != nil {
			return nil, fmt.Errorf("parse JSON: %w", err)
		}
		if locator.matches != 1 {
			return nil, fmt.Errorf("JSON must contain one string at %q, found %d", strings.Join(keyPath, "."), locator.matches)
		}
		spans = append(spans, valueSpan{start: locator.start, end: locator.end})
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].start > spans[j].start })
	for _, span := range spans {
		contents = slices.Concat(contents[:span.start], []byte(value), contents[span.end:])
	}
	return contents, nil
}

func (locator *jsonStringValueLocator) readValue(path []string) error {
	token, err := locator.decoder.Token()
	if err != nil {
		return err
	}
	switch token {
	case json.Delim('{'):
		for locator.decoder.More() {
			keyToken, err := locator.decoder.Token()
			if err != nil {
				return err
			}
			key, isString := keyToken.(string)
			if !isString {
				return fmt.Errorf("object key %v is not a string", keyToken)
			}
			if err := locator.readValue(append(slices.Clone(path), key)); err != nil {
				return err
			}
		}
		_, err = locator.decoder.Token()
		return err
	case json.Delim('['):
		for locator.decoder.More() {
			if err := locator.readValue(append(slices.Clone(path), "[]")); err != nil {
				return err
			}
		}
		_, err = locator.decoder.Token()
		return err
	}
	value, isString := token.(string)
	if !isString || !slices.Equal(path, locator.keyPath) {
		return nil
	}
	end := int(locator.decoder.InputOffset())
	start := end - len(value) - 2
	if start < 0 || string(locator.contents[start:end]) != `"`+value+`"` {
		return fmt.Errorf("string at %q contains escapes", strings.Join(path, "."))
	}
	locator.start, locator.end = start+1, end-1
	locator.matches++
	return nil
}

// connectorREADMEPaths lists a connector's README.md files and the nested connectors whose files it skips.
func connectorREADMEPaths(connectorPath string) ([]string, []string, error) {
	var readmePaths, nestedConnectorDirectories []string
	err := filepath.WalkDir(connectorPath, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == "node_modules" {
				return filepath.SkipDir
			}
			if path == connectorPath {
				return nil
			}
			if _, statErr := os.Stat(filepath.Join(path, "connector.yaml")); statErr == nil {
				relative, relErr := filepath.Rel(connectorPath, path)
				if relErr != nil {
					return relErr
				}
				nestedConnectorDirectories = append(nestedConnectorDirectories, filepath.ToSlash(relative))
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Name() == "README.md" {
			readmePaths = append(readmePaths, path)
		}
		return nil
	})
	if err != nil {
		return nil, nil, fmt.Errorf("find README files of %s: %w", connectorPath, err)
	}
	return readmePaths, nestedConnectorDirectories, nil
}

// findREADMEVersionPins finds a README's pins of one package; nested connector module paths are not pins.
func findREADMEVersionPins(path string, contents []byte, directory string, nestedConnectorDirectories []string, language, packageName string) []readmeVersionPin {
	// Each pattern ends with the version group and then a prerelease or build suffix group.
	const goVersion = `(v\d+\.\d+\.\d+)` + readmeVersionSuffix
	const packageVersion = `(\d+\.\d+\.\d+)` + readmeVersionSuffix
	var patterns []*regexp.Regexp
	switch language {
	case "go":
		patterns = []*regexp.Regexp{
			regexp.MustCompile(regexp.QuoteMeta(directory) + `((?:/[A-Za-z0-9._-]+)*?)[@/]` + goVersion),
			regexp.MustCompile("--version " + goVersion),
			regexp.MustCompile("Connector `" + goVersion + "`"),
			regexp.MustCompile("Before `" + goVersion + "` is published"),
		}
	case "python":
		patterns = []*regexp.Regexp{regexp.MustCompile(`(?:^|[^A-Za-z0-9._/-])` + regexp.QuoteMeta(packageName) + `==` + packageVersion)}
	case "typescript":
		patterns = []*regexp.Regexp{regexp.MustCompile(`(?:^|[^A-Za-z0-9._/@-])` + regexp.QuoteMeta(packageName) + `@` + packageVersion)}
	}
	var pins []readmeVersionPin
	for patternIndex, pattern := range patterns {
		for _, match := range pattern.FindAllSubmatchIndex(contents, -1) {
			versionGroup := 2
			if language == "go" && patternIndex == 0 {
				subpath := strings.TrimPrefix(string(contents[match[2]:match[3]]), "/")
				if slices.ContainsFunc(nestedConnectorDirectories, func(nested string) bool {
					return subpath == nested || strings.HasPrefix(subpath, nested+"/")
				}) {
					continue
				}
				versionGroup = 4
			}
			// A release candidate or build tag names another release, so it is not a pin.
			if match[versionGroup+2] != match[versionGroup+3] {
				continue
			}
			pins = append(pins, readmeVersionPin{
				path: path, text: string(contents[match[0]:match[1]]),
				version: string(contents[match[versionGroup]:match[versionGroup+1]]),
				start:   match[versionGroup], end: match[versionGroup+1],
			})
		}
	}
	return pins
}
