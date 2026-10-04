// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/superdurable/dex-connectors-library/schema"
	"gopkg.in/yaml.v3"
)

const (
	connectorCatalogSourceAPIVersion = "connectors.dex.dev/catalog-source/v1alpha1"
	connectorCatalogAPIVersion       = "connectors.dex.dev/catalog/v1alpha1"
	maximumGitHubActionsMatrixJobs   = 256
)

var connectorVersionPattern = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

type connectorCatalogSource struct {
	APIVersion  string   `yaml:"apiVersion"`
	Kind        string   `yaml:"kind"`
	Directories []string `yaml:"directories"`
}

type connectorCatalog struct {
	APIVersion string                 `yaml:"apiVersion"`
	Kind       string                 `yaml:"kind"`
	Connectors []connectorCatalogItem `yaml:"connectors"`
}

type connectorCatalogItem struct {
	Company     string              `yaml:"company"`
	ID          string              `yaml:"id"`
	Name        string              `yaml:"name"`
	Description string              `yaml:"description"`
	Version     string              `yaml:"version"`
	Directory   string              `yaml:"directory"`
	UIUnits     []catalogCapability `yaml:"uiUnits"`
	Triggers    []catalogCapability `yaml:"triggers"`
	Operations  []catalogCapability `yaml:"operations"`
}

type catalogCapability struct {
	Name        string `yaml:"name"`
	Kind        string `yaml:"kind,omitempty"`
	Description string `yaml:"description"`
}

type connectorDirectoryEntry struct {
	Directory    string
	ManifestPath string
	ModulePath   string
	Manifest     schema.Manifest
}

type connectorReleaseMatrix struct {
	Include []connectorReleaseMatrixItem `json:"include"`
}

type connectorReleaseMatrixItem struct {
	Directory    string `json:"directory"`
	ManifestPath string `json:"manifest_path"`
	TagPrefix    string `json:"tag_prefix"`
	Version      string `json:"version"`
	DisplayName  string `json:"display_name"`
}

type reachableConnectorRelease struct {
	Version      string
	SourceCommit string
}

func catalogCommand(args []string) error {
	flags := flag.NewFlagSet("catalog", flag.ContinueOnError)
	check := flags.Bool("check", false, "validate without writing a catalog")
	catalogPath := flags.String("catalog", "", "connector catalog source")
	outputPath := flags.String("output", "", "generated catalog path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *catalogPath == "" || (*check && *outputPath != "") || (!*check && *outputPath == "") {
		return errors.New("usage: connectorctl catalog --catalog PATH (--check | --output PATH)")
	}
	entries, err := loadConnectorDirectoryEntries(*catalogPath)
	if err != nil {
		return err
	}
	if err := validateLockstepVersions(entries); err != nil {
		return err
	}
	if *check {
		return nil
	}
	generated, err := encodeConnectorCatalog(entries)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(*outputPath), 0o755); err != nil {
		return fmt.Errorf("create catalog output directory: %w", err)
	}
	if err := os.WriteFile(*outputPath, generated, 0o644); err != nil {
		return fmt.Errorf("write connector catalog: %w", err)
	}
	return nil
}

func releaseMatrixCommand(args []string) error {
	flags := flag.NewFlagSet("release-matrix", flag.ContinueOnError)
	catalogPath := flags.String("catalog", "", "connector catalog source")
	directoryFilter := flags.String("directory", "", "one registered connector directory")
	includePublished := flags.Bool("include-published", false, "include versions with reachable tags for recovery checks")
	includePublishedAt := flags.String("include-published-at", "", "include published versions whose tag resolves to this revision")
	githubOutputPath := flags.String("github-output", "", "GitHub Actions output path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *catalogPath == "" {
		return errors.New("usage: connectorctl release-matrix --catalog PATH [--directory DIRECTORY] [--include-published | --include-published-at REV] [--github-output PATH]")
	}
	if *includePublished && *includePublishedAt != "" {
		return errors.New("include-published and include-published-at cannot be combined")
	}
	entries, err := loadConnectorDirectoryEntries(*catalogPath)
	if err != nil {
		return err
	}
	if *directoryFilter != "" {
		var selected *connectorDirectoryEntry
		for index := range entries {
			if entries[index].Directory == *directoryFilter {
				selected = &entries[index]
				break
			}
		}
		if selected == nil {
			return fmt.Errorf("connector directory is not in the catalog: %s", *directoryFilter)
		}
		entries = []connectorDirectoryEntry{*selected}
	}
	repositoryRoot := filepath.Dir(*catalogPath)
	latestReleases, err := latestReachableConnectorReleases(repositoryRoot)
	if err != nil {
		return err
	}
	includedSourceCommit := ""
	if *includePublishedAt != "" {
		includedSourceCommit, err = gitRevisionCommit(repositoryRoot, *includePublishedAt)
		if err != nil {
			return err
		}
	}
	matrix := connectorReleaseMatrix{Include: make([]connectorReleaseMatrixItem, 0, len(entries))}
	for _, entry := range entries {
		latestRelease := latestReleases[entry.Directory]
		isPending, transitionErr := validateConnectorVersionTransition(latestRelease.Version, entry.Manifest.Metadata.Version)
		if transitionErr != nil {
			return fmt.Errorf("%s: %w", entry.Directory, transitionErr)
		}
		isPublishedAtIncludedSource := includedSourceCommit != "" && latestRelease.SourceCommit == includedSourceCommit
		if !isPending && !*includePublished && !isPublishedAtIncludedSource {
			continue
		}
		matrix.Include = append(matrix.Include, connectorReleaseMatrixItem{
			Directory: entry.Directory, ManifestPath: entry.ManifestPath, TagPrefix: entry.Directory + "/",
			Version: entry.Manifest.Metadata.Version, DisplayName: entry.Manifest.Metadata.DisplayName,
		})
	}
	if err := validateConnectorReleaseMatrixSize(matrix); err != nil {
		return err
	}
	encoded, err := json.Marshal(matrix)
	if err != nil {
		return fmt.Errorf("encode connector release matrix: %w", err)
	}
	if *githubOutputPath == "" {
		_, err = fmt.Fprintln(os.Stdout, string(encoded))
		return err
	}
	contents := fmt.Sprintf("matrix=%s\ncount=%d\n", encoded, len(matrix.Include))
	if err := os.WriteFile(*githubOutputPath, []byte(contents), 0o644); err != nil {
		return fmt.Errorf("write GitHub output: %w", err)
	}
	return nil
}

func validateConnectorReleaseMatrixSize(matrix connectorReleaseMatrix) error {
	if len(matrix.Include) > maximumGitHubActionsMatrixJobs {
		return fmt.Errorf("release matrix contains %d connectors; split the release into at most %d connectors", len(matrix.Include), maximumGitHubActionsMatrixJobs)
	}
	return nil
}

func loadConnectorDirectoryEntries(catalogPath string) ([]connectorDirectoryEntry, error) {
	catalogSource, err := loadConnectorCatalogSource(catalogPath)
	if err != nil {
		return nil, err
	}
	repositoryRoot, err := filepath.Abs(filepath.Dir(catalogPath))
	if err != nil {
		return nil, fmt.Errorf("resolve repository root: %w", err)
	}
	registeredDirectories := make(map[string]bool, len(catalogSource.Directories))
	for _, directory := range catalogSource.Directories {
		registeredDirectories[directory] = true
	}
	connectorIDs := make(map[string]bool, len(catalogSource.Directories))
	entries := make([]connectorDirectoryEntry, 0, len(catalogSource.Directories))
	for _, directory := range catalogSource.Directories {
		if err := rejectSymlinkPath(repositoryRoot, directory); err != nil {
			return nil, err
		}
		manifestPath := filepath.Join(repositoryRoot, filepath.FromSlash(directory), "connector.yaml")
		if err := requireRegularFile(manifestPath, "connector manifest"); err != nil {
			return nil, err
		}
		manifest, err := load(manifestPath)
		if err != nil {
			return nil, err
		}
		if connectorIDs[manifest.Metadata.Name] {
			return nil, fmt.Errorf("duplicate connector ID: %s", manifest.Metadata.Name)
		}
		connectorIDs[manifest.Metadata.Name] = true
		companyDir, companyErr := connectorCompanyDirectory(directory)
		if companyErr != nil {
			return nil, companyErr
		}
		if companyDirectorySlug(manifest.Metadata.Company) != companyDir {
			return nil, fmt.Errorf("%s company %s must match directory %s", directory, manifest.Metadata.Company, companyDir)
		}
		goModPath := filepath.Join(repositoryRoot, filepath.FromSlash(directory), "go.mod")
		if err := requireRegularFile(goModPath, "connector go.mod"); err != nil {
			return nil, err
		}
		modulePath, err := readModulePath(goModPath)
		if err != nil {
			return nil, err
		}
		if !strings.HasSuffix(modulePath, "/"+directory) {
			return nil, fmt.Errorf("connector module %s must end in /%s", modulePath, directory)
		}
		entries = append(entries, connectorDirectoryEntry{
			Directory: directory, ManifestPath: directory + "/connector.yaml", ModulePath: modulePath, Manifest: manifest,
		})
	}
	if err := validateDiscoveredConnectorDirectories(repositoryRoot, registeredDirectories); err != nil {
		return nil, err
	}
	if err := validateCompanyLogos(repositoryRoot); err != nil {
		return nil, err
	}
	return entries, nil
}

func loadConnectorCatalogSource(catalogPath string) (connectorCatalogSource, error) {
	catalogContents, err := os.ReadFile(catalogPath)
	if err != nil {
		return connectorCatalogSource{}, fmt.Errorf("open connector catalog source: %w", err)
	}
	return decodeConnectorCatalogSource(catalogContents)
}

func decodeConnectorCatalogSource(catalogContents []byte) (connectorCatalogSource, error) {
	decoder := yaml.NewDecoder(strings.NewReader(string(catalogContents)))
	decoder.KnownFields(true)
	var catalogSource connectorCatalogSource
	if err := decoder.Decode(&catalogSource); err != nil {
		return connectorCatalogSource{}, fmt.Errorf("decode connector catalog source: %w", err)
	}
	var extraDocument any
	if err := decoder.Decode(&extraDocument); err != io.EOF {
		if err == nil {
			return connectorCatalogSource{}, errors.New("connector catalog source must contain one YAML document")
		}
		return connectorCatalogSource{}, fmt.Errorf("decode connector catalog source: %w", err)
	}
	if catalogSource.APIVersion != connectorCatalogSourceAPIVersion {
		return connectorCatalogSource{}, fmt.Errorf("connector catalog source apiVersion must be %s", connectorCatalogSourceAPIVersion)
	}
	if catalogSource.Kind != "ConnectorCatalogSource" {
		return connectorCatalogSource{}, errors.New("connector catalog source kind must be ConnectorCatalogSource")
	}
	if len(catalogSource.Directories) == 0 {
		return connectorCatalogSource{}, errors.New("connector catalog source is empty")
	}
	if !sort.StringsAreSorted(catalogSource.Directories) {
		return connectorCatalogSource{}, errors.New("connector directories must be sorted")
	}
	registeredDirectories := make(map[string]bool, len(catalogSource.Directories))
	for _, directory := range catalogSource.Directories {
		if err := validateConnectorDirectory(directory); err != nil {
			return connectorCatalogSource{}, err
		}
		if registeredDirectories[directory] {
			return connectorCatalogSource{}, fmt.Errorf("duplicate connector directory: %s", directory)
		}
		registeredDirectories[directory] = true
	}
	return catalogSource, nil
}

func connectorCompanyDirectory(directory string) (string, error) {
	rest := strings.TrimPrefix(directory, "connectors/")
	companyDir, _, _ := strings.Cut(rest, "/")
	if companyDir == "" || companyDir == "." || companyDir == ".." {
		return "", fmt.Errorf("connector directory is missing a company: %s", directory)
	}
	return companyDir, nil
}

func companyDirectorySlug(company string) string {
	var slug strings.Builder
	for _, character := range strings.ToLower(company) {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			slug.WriteRune(character)
		}
	}
	return slug.String()
}

func validateCompanyLogos(repositoryRoot string) error {
	connectorsRoot := filepath.Join(repositoryRoot, "connectors")
	entries, err := os.ReadDir(connectorsRoot)
	if err != nil {
		return fmt.Errorf("read company directories: %w", err)
	}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("company directory cannot be a symlink: connectors/%s", entry.Name())
		}
		if !entry.IsDir() {
			continue
		}
		logoPath := filepath.Join(connectorsRoot, entry.Name(), "logo.svg")
		if err := requireRegularFile(logoPath, "company logo"); err != nil {
			return fmt.Errorf("connectors/%s: %w", entry.Name(), err)
		}
	}
	return nil
}

func validateConnectorDirectory(directory string) error {
	if directory == "" || path.IsAbs(directory) || path.Clean(directory) != directory || strings.Contains(directory, `\`) {
		return fmt.Errorf("connector directory must be a normalized repository-relative POSIX path: %s", directory)
	}
	if !strings.HasPrefix(directory, "connectors/") || strings.TrimPrefix(directory, "connectors/") == "" {
		return fmt.Errorf("connector directory must be below connectors/: %s", directory)
	}
	for _, segment := range strings.Split(directory, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return fmt.Errorf("connector directory contains an unsafe segment: %s", directory)
		}
	}
	return nil
}

func rejectSymlinkPath(repositoryRoot, directory string) error {
	current := repositoryRoot
	for _, segment := range strings.Split(directory, "/") {
		current = filepath.Join(current, segment)
		info, err := os.Lstat(current)
		if err != nil {
			return fmt.Errorf("inspect connector directory %s: %w", directory, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("connector directory cannot contain symlinks: %s", directory)
		}
	}
	return nil
}

func requireRegularFile(filePath, description string) error {
	info, err := os.Lstat(filePath)
	if err != nil {
		return fmt.Errorf("%s is missing: %s", description, filePath)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s must be a regular file: %s", description, filePath)
	}
	return nil
}

func readModulePath(filePath string) (string, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("read connector module %s: %w", filePath, err)
	}
	for _, line := range strings.Split(string(content), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "module ") {
			modulePath := strings.TrimSpace(strings.TrimPrefix(trimmed, "module "))
			if modulePath != "" {
				return modulePath, nil
			}
		}
	}
	return "", fmt.Errorf("connector module does not declare a module path: %s", filePath)
}

func validateDiscoveredConnectorDirectories(repositoryRoot string, registeredDirectories map[string]bool) error {
	connectorsRoot := filepath.Join(repositoryRoot, "connectors")
	return filepath.WalkDir(connectorsRoot, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Name() != "connector.yaml" {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("connector manifest cannot be a symlink: %s", filePath)
		}
		directory, err := filepath.Rel(repositoryRoot, filepath.Dir(filePath))
		if err != nil {
			return err
		}
		directory = filepath.ToSlash(directory)
		if !registeredDirectories[directory] {
			return fmt.Errorf("connector manifest is not registered: %s", directory)
		}
		return nil
	})
}

func encodeConnectorCatalog(entries []connectorDirectoryEntry) ([]byte, error) {
	catalog := connectorCatalog{
		APIVersion: connectorCatalogAPIVersion,
		Kind:       "ConnectorCatalog",
		Connectors: make([]connectorCatalogItem, 0, len(entries)),
	}
	for _, entry := range entries {
		metadata := entry.Manifest.Metadata
		uiUnits := []catalogCapability{}
		if entry.Manifest.Spec.Studio != nil {
			uiUnits = make([]catalogCapability, 0, len(entry.Manifest.Spec.Studio.Units))
			for _, unit := range entry.Manifest.Spec.Studio.Units {
				uiUnits = append(uiUnits, catalogCapability{Name: unit.ID, Description: unit.Description})
			}
		}
		triggers := make([]catalogCapability, 0, len(entry.Manifest.Spec.Triggers))
		for _, trigger := range entry.Manifest.Spec.Triggers {
			triggers = append(triggers, catalogCapability{Name: trigger.Name, Description: trigger.Description})
		}
		operations := make([]catalogCapability, 0, len(entry.Manifest.Spec.Operations))
		for _, operation := range entry.Manifest.Spec.Operations {
			operations = append(operations, catalogCapability{
				Name: operation.Name, Kind: operation.Kind, Description: operation.Description,
			})
		}
		catalog.Connectors = append(catalog.Connectors, connectorCatalogItem{
			Company: metadata.Company, ID: metadata.Name, Name: metadata.DisplayName,
			Description: metadata.Description, Version: metadata.Version, Directory: entry.Directory,
			UIUnits: uiUnits, Triggers: triggers, Operations: operations,
		})
	}
	encoded, err := yaml.Marshal(catalog)
	if err != nil {
		return nil, fmt.Errorf("encode connector catalog: %w", err)
	}
	return encoded, nil
}

func latestReachableConnectorReleases(repositoryRoot string) (map[string]reachableConnectorRelease, error) {
	command := exec.Command(
		"git", "for-each-ref", "--merged=HEAD",
		"--format=%(refname:short)%09%(objectname)%09%(*objectname)",
		"refs/tags/connectors/",
	)
	command.Dir = repositoryRoot
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("list connector release tags: %w", err)
	}
	latest := make(map[string]reachableConnectorRelease)
	for _, line := range strings.Split(strings.TrimSuffix(string(output), "\n"), "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) != 3 {
			continue
		}
		tag := fields[0]
		separator := strings.LastIndex(tag, "/")
		if separator < 0 {
			continue
		}
		directory := tag[:separator]
		version := tag[separator+1:]
		if !connectorVersionPattern.MatchString(version) {
			continue
		}
		current := latest[directory]
		if current.Version == "" || compareConnectorVersions(version, current.Version) > 0 {
			sourceCommit := fields[1]
			if fields[2] != "" {
				sourceCommit = fields[2]
			}
			latest[directory] = reachableConnectorRelease{Version: version, SourceCommit: sourceCommit}
		}
	}
	return latest, nil
}

func gitRevisionCommit(repositoryRoot string, revision string) (string, error) {
	command := exec.Command("git", "rev-parse", "--verify", revision+"^{commit}")
	command.Dir = repositoryRoot
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("resolve Git revision %s: %w", revision, err)
	}
	return strings.TrimSpace(string(output)), nil
}

// validateConnectorVersionTransition reports whether target is a release after baseline, the latest released
// version. Connectors release in lockstep, so a connector skips the versions it was not released at, and a new
// connector starts at the current lockstep version.
func validateConnectorVersionTransition(baseline, target string) (bool, error) {
	if !connectorVersionPattern.MatchString(target) {
		return false, fmt.Errorf("invalid target connector version: %s", target)
	}
	if baseline == "" {
		return true, nil
	}
	comparison := compareConnectorVersions(target, baseline)
	if comparison < 0 {
		return false, fmt.Errorf("connector version %s is behind latest release %s", target, baseline)
	}
	return comparison > 0, nil
}

func compareConnectorVersions(left, right string) int {
	leftParts := connectorVersionParts(left)
	rightParts := connectorVersionParts(right)
	for index := range leftParts {
		if leftParts[index] < rightParts[index] {
			return -1
		}
		if leftParts[index] > rightParts[index] {
			return 1
		}
	}
	return 0
}

func connectorVersionParts(version string) [3]int {
	match := connectorVersionPattern.FindStringSubmatch(version)
	var parts [3]int
	for index := range parts {
		part, err := strconv.Atoi(match[index+1])
		if err != nil {
			panic(fmt.Sprintf("validated connector version contains a non-numeric part: %s", version))
		}
		parts[index] = part
	}
	return parts
}

// validateLockstepVersions requires every connector to declare one release version. The Connector SDK and every
// connector release together under that version, so an application never mixes releases built for different SDKs.
func validateLockstepVersions(entries []connectorDirectoryEntry) error {
	if len(entries) == 0 {
		return nil
	}
	version := entries[0].Manifest.Metadata.Version
	for _, entry := range entries[1:] {
		if entry.Manifest.Metadata.Version != version {
			return fmt.Errorf("connector %s declares %s, but every connector declares the same release version (%s declares %s)",
				entry.Directory, entry.Manifest.Metadata.Version, entries[0].Directory, version)
		}
	}
	return nil
}
