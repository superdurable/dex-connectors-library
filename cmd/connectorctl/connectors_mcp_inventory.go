// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"
	"gopkg.in/yaml.v3"
)

const (
	connectorsMCPSDKModulePath = "github.com/superdurable/dex-connectors-library/sdkgo"
	// connectorsMCPMinimumSDKVersion is the first sdkgo release with the agent tool API.
	connectorsMCPMinimumSDKVersion = "v0.35.0"
	connectorsMCPAgentToolsFile    = "zz_generated_agent_tools.go"
	connectorsMCPServerDirectory   = "connectorsmcp"
)

var (
	connectorsMCPSeriesPattern      = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	connectorsMCPConnectorIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,62}$`)
	goPackageClausePattern          = regexp.MustCompile(`(?m)^package ([A-Za-z_][A-Za-z0-9_]*)\s*$`)
)

// connectorsMCPReleaseConfig is connectorsmcp/release.yaml.
type connectorsMCPReleaseConfig struct {
	Series              string   `yaml:"series"`
	WithdrawnConnectors []string `yaml:"withdrawnConnectors"`
}

// connectorsMCPInventory is the input from which the Connectors MCP release plans its connector set.
type connectorsMCPInventory struct {
	Series              string                   `json:"series"`
	WithdrawnConnectors []string                 `json:"withdrawnConnectors"`
	SourceCommit        string                   `json:"sourceCommit"`
	ServerSourceTree    string                   `json:"serverSourceTree"`
	Connectors          []connectorsMCPCandidate `json:"connectors"`
}

// connectorsMCPCandidate is a connector whose latest complete Go release offers generated agent tools.
type connectorsMCPCandidate struct {
	ConnectorID string   `json:"connectorId"`
	DisplayName string   `json:"displayName"`
	Directory   string   `json:"directory"`
	Module      string   `json:"module"`
	Version     string   `json:"version"`
	Tag         string   `json:"tag"`
	PackageName string   `json:"packageName"`
	Tools       []string `json:"tools"`
}

// connectorsMCPReleasedManifest decodes only the needed fields, so manifests at older tags still decode.
type connectorsMCPReleasedManifest struct {
	Metadata struct {
		Name        string `yaml:"name"`
		DisplayName string `yaml:"displayName"`
	} `yaml:"metadata"`
	Spec struct {
		Operations []struct {
			Name  string `yaml:"name"`
			Agent *struct {
				Exposed *bool `yaml:"exposed"`
			} `yaml:"agent"`
		} `yaml:"operations"`
	} `yaml:"spec"`
}

func connectorsMCPInventoryCommand(args []string) error {
	flags := flag.NewFlagSet("connectors-mcp-inventory", flag.ContinueOnError)
	catalogPath := flags.String("catalog", "", "connector catalog source")
	releaseInventoryPath := flags.String("release-inventory", "", "GitHub release inventory")
	releaseConfigPath := flags.String("release-config", "", "connectorsmcp/release.yaml")
	outputPath := flags.String("output", "", "inventory JSON output path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *catalogPath == "" || *releaseInventoryPath == "" || *releaseConfigPath == "" || *outputPath == "" {
		return errors.New("usage: connectorctl connectors-mcp-inventory --catalog PATH --release-inventory PATH --release-config PATH --output PATH")
	}
	inventory, err := buildConnectorsMCPInventory(*catalogPath, *releaseInventoryPath, *releaseConfigPath)
	if err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(inventory, "", "  ")
	if err != nil {
		return fmt.Errorf("encode Connectors MCP inventory: %w", err)
	}
	if err := os.WriteFile(*outputPath, append(encoded, '\n'), 0o644); err != nil {
		return fmt.Errorf("write Connectors MCP inventory: %w", err)
	}
	return nil
}

func buildConnectorsMCPInventory(catalogPath, releaseInventoryPath, releaseConfigPath string) (connectorsMCPInventory, error) {
	config, err := readConnectorsMCPReleaseConfig(releaseConfigPath)
	if err != nil {
		return connectorsMCPInventory{}, err
	}
	completedTags, err := readCompletedGoReleaseTags(releaseInventoryPath)
	if err != nil {
		return connectorsMCPInventory{}, err
	}
	if completedTags == nil {
		return connectorsMCPInventory{}, errors.New("the release inventory is unavailable; the Connectors MCP release needs complete Go releases")
	}
	catalogSource, err := loadConnectorCatalogSource(catalogPath)
	if err != nil {
		return connectorsMCPInventory{}, err
	}
	absoluteCatalogPath, err := filepath.Abs(catalogPath)
	if err != nil {
		return connectorsMCPInventory{}, fmt.Errorf("resolve connector catalog path: %w", err)
	}
	repositoryRoot := filepath.Dir(absoluteCatalogPath)
	latestTags, err := latestCompleteGoReleaseTags(repositoryRoot, catalogSource.Directories, completedTags)
	if err != nil {
		return connectorsMCPInventory{}, err
	}
	inventory := connectorsMCPInventory{
		Series:              config.Series,
		WithdrawnConnectors: config.WithdrawnConnectors,
		Connectors:          []connectorsMCPCandidate{},
	}
	if inventory.SourceCommit, err = gitTrimmedOutput(repositoryRoot, "rev-parse", "--verify", "HEAD^{commit}"); err != nil {
		return connectorsMCPInventory{}, err
	}
	if inventory.ServerSourceTree, err = gitTrimmedOutput(repositoryRoot, "rev-parse", "--verify", "HEAD:"+connectorsMCPServerDirectory); err != nil {
		return connectorsMCPInventory{}, err
	}
	connectorIDs := make(map[string]string)
	for _, directory := range catalogSource.Directories {
		tag, hasRelease := latestTags[directory]
		if !hasRelease {
			continue
		}
		candidate, isCandidate, err := readConnectorsMCPCandidate(repositoryRoot, directory, tag)
		if err != nil {
			return connectorsMCPInventory{}, err
		}
		if !isCandidate {
			continue
		}
		if previous, isDuplicate := connectorIDs[candidate.ConnectorID]; isDuplicate {
			return connectorsMCPInventory{}, fmt.Errorf("connector ID %s is released by both %s and %s", candidate.ConnectorID, previous, directory)
		}
		connectorIDs[candidate.ConnectorID] = directory
		inventory.Connectors = append(inventory.Connectors, candidate)
	}
	slices.SortFunc(inventory.Connectors, func(left, right connectorsMCPCandidate) int {
		return strings.Compare(left.ConnectorID, right.ConnectorID)
	})
	return inventory, nil
}

func readConnectorsMCPReleaseConfig(path string) (connectorsMCPReleaseConfig, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return connectorsMCPReleaseConfig{}, fmt.Errorf("read Connectors MCP release config: %w", err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(contents))
	decoder.KnownFields(true)
	var config connectorsMCPReleaseConfig
	if err := decoder.Decode(&config); err != nil {
		return connectorsMCPReleaseConfig{}, fmt.Errorf("decode Connectors MCP release config %s: %w", path, err)
	}
	if !connectorsMCPSeriesPattern.MatchString(config.Series) {
		return connectorsMCPReleaseConfig{}, fmt.Errorf("Connectors MCP release series %q must be vMAJOR.MINOR", config.Series)
	}
	if config.WithdrawnConnectors == nil {
		config.WithdrawnConnectors = []string{}
	}
	seen := make(map[string]bool, len(config.WithdrawnConnectors))
	for _, connectorID := range config.WithdrawnConnectors {
		if !connectorsMCPConnectorIDPattern.MatchString(connectorID) {
			return connectorsMCPReleaseConfig{}, fmt.Errorf("withdrawn connector ID %q is invalid", connectorID)
		}
		if seen[connectorID] {
			return connectorsMCPReleaseConfig{}, fmt.Errorf("withdrawn connector %s is listed twice", connectorID)
		}
		seen[connectorID] = true
	}
	slices.Sort(config.WithdrawnConnectors)
	return config, nil
}

// latestCompleteGoReleaseTags returns each directory's highest reachable Go tag with connector-release.complete.
func latestCompleteGoReleaseTags(repositoryRoot string, directories []string, completedTags map[string]bool) (map[string]string, error) {
	output, err := gitOutput(repositoryRoot, "for-each-ref", "--merged=HEAD", "--format=%(refname:short)", "refs/tags/connectors/")
	if err != nil {
		return nil, err
	}
	registered := make(map[string]bool, len(directories))
	for _, directory := range directories {
		registered[directory] = true
	}
	latest := make(map[string]string)
	for _, tag := range strings.Fields(output) {
		separator := strings.LastIndex(tag, "/go/")
		if separator < 0 {
			continue
		}
		directory, version := tag[:separator], tag[separator+len("/go/"):]
		if !registered[directory] || !connectorVersionPattern.MatchString(version) || !completedTags[tag] {
			continue
		}
		if current, hasCurrent := latest[directory]; !hasCurrent || compareConnectorVersions(version, strings.TrimPrefix(current, directory+"/go/")) > 0 {
			latest[directory] = tag
		}
	}
	return latest, nil
}

// readConnectorsMCPCandidate links a release that requires sdkgo v0.35.0+, has agent tools, and exposes an operation.
func readConnectorsMCPCandidate(repositoryRoot, directory, tag string) (connectorsMCPCandidate, bool, error) {
	goModPath := directory + "/go/go.mod"
	goModContents, err := gitOutput(repositoryRoot, "show", tag+":"+goModPath)
	if err != nil {
		return connectorsMCPCandidate{}, false, err
	}
	module, err := modfile.Parse(tag+":"+goModPath, []byte(goModContents), nil)
	if err != nil {
		return connectorsMCPCandidate{}, false, fmt.Errorf("parse %s at %s: %w", goModPath, tag, err)
	}
	if module.Module == nil {
		return connectorsMCPCandidate{}, false, fmt.Errorf("%s at %s has no module directive", goModPath, tag)
	}
	sdkVersion := ""
	for _, requirement := range module.Require {
		if requirement.Mod.Path == connectorsMCPSDKModulePath {
			sdkVersion = requirement.Mod.Version
		}
	}
	if !semver.IsValid(sdkVersion) || semver.Compare(sdkVersion, connectorsMCPMinimumSDKVersion) < 0 {
		return connectorsMCPCandidate{}, false, nil
	}
	agentToolsPath := directory + "/go/" + connectorsMCPAgentToolsFile
	if !gitObjectExists(repositoryRoot, tag+":"+agentToolsPath) {
		return connectorsMCPCandidate{}, false, nil
	}
	agentToolsSource, err := gitOutput(repositoryRoot, "show", tag+":"+agentToolsPath)
	if err != nil {
		return connectorsMCPCandidate{}, false, err
	}
	packageMatch := goPackageClausePattern.FindStringSubmatch(agentToolsSource)
	if packageMatch == nil {
		return connectorsMCPCandidate{}, false, fmt.Errorf("%s at %s has no package clause", agentToolsPath, tag)
	}
	manifestContents, err := gitOutput(repositoryRoot, "show", tag+":"+directory+"/connector.yaml")
	if err != nil {
		return connectorsMCPCandidate{}, false, err
	}
	var manifest connectorsMCPReleasedManifest
	if err := yaml.Unmarshal([]byte(manifestContents), &manifest); err != nil {
		return connectorsMCPCandidate{}, false, fmt.Errorf("decode %s/connector.yaml at %s: %w", directory, tag, err)
	}
	if !connectorsMCPConnectorIDPattern.MatchString(manifest.Metadata.Name) {
		return connectorsMCPCandidate{}, false, fmt.Errorf("%s/connector.yaml at %s has an invalid connector ID %q", directory, tag, manifest.Metadata.Name)
	}
	tools := []string{}
	for _, operation := range manifest.Spec.Operations {
		if operation.Agent != nil && operation.Agent.Exposed != nil && *operation.Agent.Exposed {
			tools = append(tools, manifest.Metadata.Name+"_"+lowerCamelToSnake(operation.Name))
		}
	}
	if len(tools) == 0 {
		// generate --check rejects this, so only older tooling could have released it.
		fmt.Fprintf(os.Stderr, "::warning::%s has %s but exposes no operation; it is not linked\n", tag, connectorsMCPAgentToolsFile)
		return connectorsMCPCandidate{}, false, nil
	}
	slices.Sort(tools)
	return connectorsMCPCandidate{
		ConnectorID: manifest.Metadata.Name,
		DisplayName: manifest.Metadata.DisplayName,
		Directory:   directory,
		Module:      module.Module.Mod.Path,
		Version:     strings.TrimPrefix(tag, directory+"/go/"),
		Tag:         tag,
		PackageName: packageMatch[1],
		Tools:       tools,
	}, true, nil
}

func gitObjectExists(repositoryRoot, object string) bool {
	command := exec.Command("git", "cat-file", "-e", object)
	command.Dir = repositoryRoot
	return command.Run() == nil
}

func gitTrimmedOutput(repositoryRoot string, arguments ...string) (string, error) {
	output, err := gitOutput(repositoryRoot, arguments...)
	return strings.TrimSpace(output), err
}
