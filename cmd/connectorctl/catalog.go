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

	"github.com/superdurable/dex-connectors-library/schema"
	"gopkg.in/yaml.v3"
)

const (
	connectorDirectoryListAPIVersion = "connectors.dex.dev/directory-list/v1alpha1"
	connectorCatalogAPIVersion       = "connectors.dex.dev/catalog/v1alpha1"
)

var connectorVersionPattern = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

type connectorDirectoryList struct {
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
	Company     string `yaml:"company"`
	ID          string `yaml:"id"`
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Version     string `yaml:"version"`
	Directory   string `yaml:"directory"`
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

func catalogCommand(args []string) error {
	flags := flag.NewFlagSet("catalog", flag.ContinueOnError)
	check := flags.Bool("check", false, "validate without writing a catalog")
	registryPath := flags.String("registry", "", "connector directory registry")
	outputPath := flags.String("output", "", "generated catalog path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *registryPath == "" || (*check && *outputPath != "") || (!*check && *outputPath == "") {
		return errors.New("usage: connectorctl catalog --registry PATH (--check | --output PATH)")
	}
	entries, err := loadConnectorDirectoryEntries(*registryPath)
	if err != nil {
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
	registryPath := flags.String("registry", "", "connector directory registry")
	includePublished := flags.Bool("include-published", false, "include versions with reachable tags for recovery checks")
	githubOutputPath := flags.String("github-output", "", "GitHub Actions output path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *registryPath == "" {
		return errors.New("usage: connectorctl release-matrix --registry PATH [--include-published] [--github-output PATH]")
	}
	entries, err := loadConnectorDirectoryEntries(*registryPath)
	if err != nil {
		return err
	}
	repositoryRoot := filepath.Dir(*registryPath)
	matrix := connectorReleaseMatrix{Include: make([]connectorReleaseMatrixItem, 0, len(entries))}
	for _, entry := range entries {
		latestVersion, latestErr := latestReachableConnectorVersion(repositoryRoot, entry.Directory+"/")
		if latestErr != nil {
			return latestErr
		}
		isPending, transitionErr := validateConnectorVersionTransition(latestVersion, entry.Manifest.Metadata.Version)
		if transitionErr != nil {
			return fmt.Errorf("%s: %w", entry.Directory, transitionErr)
		}
		if !isPending && !*includePublished {
			continue
		}
		matrix.Include = append(matrix.Include, connectorReleaseMatrixItem{
			Directory: entry.Directory, ManifestPath: entry.ManifestPath, TagPrefix: entry.Directory + "/",
			Version: entry.Manifest.Metadata.Version, DisplayName: entry.Manifest.Metadata.DisplayName,
		})
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

func loadConnectorDirectoryEntries(registryPath string) ([]connectorDirectoryEntry, error) {
	registryContents, err := os.ReadFile(registryPath)
	if err != nil {
		return nil, fmt.Errorf("open connector directory registry: %w", err)
	}
	decoder := yaml.NewDecoder(strings.NewReader(string(registryContents)))
	decoder.KnownFields(true)
	var registry connectorDirectoryList
	if err := decoder.Decode(&registry); err != nil {
		return nil, fmt.Errorf("decode connector directory registry: %w", err)
	}
	var extraDocument any
	if err := decoder.Decode(&extraDocument); err != io.EOF {
		if err == nil {
			return nil, errors.New("connector directory registry must contain one YAML document")
		}
		return nil, fmt.Errorf("decode connector directory registry: %w", err)
	}
	if registry.APIVersion != connectorDirectoryListAPIVersion {
		return nil, fmt.Errorf("connector directory registry apiVersion must be %s", connectorDirectoryListAPIVersion)
	}
	if registry.Kind != "ConnectorDirectoryList" {
		return nil, errors.New("connector directory registry kind must be ConnectorDirectoryList")
	}
	if len(registry.Directories) == 0 {
		return nil, errors.New("connector directory registry is empty")
	}
	if !sort.StringsAreSorted(registry.Directories) {
		return nil, errors.New("connector directories must be sorted")
	}
	repositoryRoot, err := filepath.Abs(filepath.Dir(registryPath))
	if err != nil {
		return nil, fmt.Errorf("resolve repository root: %w", err)
	}
	registeredDirectories := make(map[string]bool, len(registry.Directories))
	for _, directory := range registry.Directories {
		if err := validateConnectorDirectory(directory); err != nil {
			return nil, err
		}
		if registeredDirectories[directory] {
			return nil, fmt.Errorf("duplicate connector directory: %s", directory)
		}
		registeredDirectories[directory] = true
	}
	connectorIDs := make(map[string]bool, len(registry.Directories))
	entries := make([]connectorDirectoryEntry, 0, len(registry.Directories))
	for _, directory := range registry.Directories {
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
	return entries, nil
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
		catalog.Connectors = append(catalog.Connectors, connectorCatalogItem{
			Company: metadata.Company, ID: metadata.Name, Name: metadata.DisplayName,
			Description: metadata.Description, Version: metadata.Version, Directory: entry.Directory,
		})
	}
	encoded, err := yaml.Marshal(catalog)
	if err != nil {
		return nil, fmt.Errorf("encode connector catalog: %w", err)
	}
	return encoded, nil
}

func latestReachableConnectorVersion(repositoryRoot, tagPrefix string) (string, error) {
	command := exec.Command("git", "for-each-ref", "--merged=HEAD", "--format=%(refname:short)", "refs/tags/"+tagPrefix+"*")
	command.Dir = repositoryRoot
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("list connector release tags: %w", err)
	}
	latest := ""
	for _, tag := range strings.Fields(string(output)) {
		version := strings.TrimPrefix(tag, tagPrefix)
		if !connectorVersionPattern.MatchString(version) {
			continue
		}
		if latest == "" || compareConnectorVersions(version, latest) > 0 {
			latest = version
		}
	}
	return latest, nil
}

func validateConnectorVersionTransition(baseline, target string) (bool, error) {
	if !connectorVersionPattern.MatchString(target) {
		return false, fmt.Errorf("invalid target connector version: %s", target)
	}
	if baseline == "" {
		if target != "v0.1.0" {
			return false, fmt.Errorf("first connector release must be v0.1.0, got %s", target)
		}
		return true, nil
	}
	comparison := compareConnectorVersions(target, baseline)
	if comparison == 0 {
		return false, nil
	}
	if comparison < 0 {
		return false, fmt.Errorf("connector version %s is behind latest release %s", target, baseline)
	}
	baselineParts := connectorVersionParts(baseline)
	allowed := map[string]bool{
		fmt.Sprintf("v%d.%d.%d", baselineParts[0], baselineParts[1], baselineParts[2]+1): true,
		fmt.Sprintf("v%d.%d.0", baselineParts[0], baselineParts[1]+1):                    true,
		fmt.Sprintf("v%d.0.0", baselineParts[0]+1):                                       true,
	}
	if !allowed[target] {
		return false, fmt.Errorf("connector version %s must be the next patch, minor, or major after %s", target, baseline)
	}
	return true, nil
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
