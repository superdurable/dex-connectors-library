// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/mod/modfile"
)

type connectorTestMatrix struct {
	Include []connectorTestShard `json:"include"`
}

type connectorTestShard struct {
	Index int `json:"shard_index"`
	Count int `json:"shard_count"`
}

type ciJobSelection struct {
	Common           bool
	SDKPython        bool
	SDKTypeScript    bool
	SDKGoIntegration bool
	React            bool
	Site             bool
	// ConnectorsMCP tests the connectorsmcp module against the repository's sdkgo.
	ConnectorsMCP bool
}

// changedPathImpact is what one changed path requires a changed-scope CI run to test.
type changedPathImpact struct {
	shouldTestAllConnectors bool
	ciJobs                  ciJobSelection
}

// repositoryToolingTestOnlyPaths run only in common or release workflows; unlisted new files fan out.
var repositoryToolingTestOnlyPaths = map[string]bool{
	".githooks/commit-msg":                              true,
	".github/PULL_REQUEST_TEMPLATE.md":                  true,
	"script/agent_rules_test.py":                        true,
	"script/branch_off_main_policy.py":                  true,
	"script/branch_off_main_policy_test.py":             true,
	"script/ci_workflow_test.py":                        true,
	"script/connector_conformance_test.py":              true,
	"script/dex_compatibility_test.py":                  true,
	"script/python_latest_sdk_test.py":                  true,
	"script/python_timestamp_parity_test.py":            true,
	"script/studio_bundle_theme_check_test.py":          true,
	"script/verify_go_subdir_migration.py":              true,
	"script/verify_go_subdir_migration_test.py":         true,
	"script/release/component_release_test.py":          true,
	"script/release/connectors_mcp_release.py":          true,
	"script/release/connectors_mcp_release_test.py":     true,
	"script/release/github_release_inventory_test.py":   true,
	"script/release/language_publish.py":                true,
	"script/release/language_publish_test.py":           true,
	"script/release/npm_publish.py":                     true,
	"script/release/npm_publish_test.py":                true,
	"script/release/retitle_connector_releases.py":      true,
	"script/release/retitle_connector_releases_test.py": true,
}

func testMatrixCommand(args []string) error {
	flags := flag.NewFlagSet("test-matrix", flag.ContinueOnError)
	catalogPath := flags.String("catalog", "", "connector catalog source")
	scope := flags.String("scope", "all", "all or changed connectors")
	baseRevision := flags.String("base", "", "base Git revision for changed scope")
	headRevision := flags.String("head", "HEAD", "head Git revision for changed scope")
	maximumShards := flags.Int("max-shards", maximumGitHubActionsMatrixJobs, "maximum number of test shards")
	githubOutputPath := flags.String("github-output", "", "GitHub Actions output path")
	shardIndex := flags.Int("shard-index", -1, "list directories assigned to this zero-based shard")
	shardCount := flags.Int("shard-count", 0, "total shard count when listing directories")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *catalogPath == "" || (*scope != "all" && *scope != "changed") {
		return errors.New("usage: connectorctl test-matrix --catalog PATH --scope <all|changed> [--base REF --head REF] [--max-shards N | --shard-index N --shard-count N] [--github-output PATH]")
	}
	if *scope == "changed" && *baseRevision == "" {
		return errors.New("changed connector scope requires --base")
	}
	if *shardIndex < -1 || *shardCount < 0 || (*shardIndex >= 0) != (*shardCount > 0) || *shardIndex >= *shardCount {
		return errors.New("shard-index and shard-count must name one valid shard")
	}
	if *maximumShards < 1 || *maximumShards > maximumGitHubActionsMatrixJobs {
		return fmt.Errorf("max-shards must be between 1 and %d", maximumGitHubActionsMatrixJobs)
	}
	catalogSource, err := loadConnectorCatalogSource(*catalogPath)
	if err != nil {
		return err
	}
	directories, err := selectConnectorTestDirectories(
		*catalogPath,
		catalogSource.Directories,
		*scope,
		*baseRevision,
		*headRevision,
	)
	if err != nil {
		return err
	}
	var changedPaths []string
	if *scope == "changed" {
		changedPaths, err = changedRepositoryPaths(*catalogPath, *baseRevision, *headRevision)
		if err != nil {
			return err
		}
	}
	if *shardIndex >= 0 {
		for _, directory := range directoriesInConnectorTestShard(directories, *shardIndex, *shardCount) {
			if _, err := fmt.Fprintln(os.Stdout, directory); err != nil {
				return err
			}
		}
		return nil
	}
	matrix := buildConnectorTestMatrix(directories, *maximumShards)
	encoded, err := json.Marshal(matrix)
	if err != nil {
		return fmt.Errorf("encode connector test matrix: %w", err)
	}
	if *githubOutputPath == "" {
		_, err = fmt.Fprintln(os.Stdout, string(encoded))
		return err
	}
	ciJobs := selectCIJobsForPaths(*scope, changedPaths)
	contents := fmt.Sprintf("matrix=%s\ncount=%d\njobs=%d\ncommon=%t\nsdkpython=%t\nsdkts=%t\nsdkgo_integration=%t\nreact=%t\nsite=%t\nconnectors_mcp=%t\n", encoded, len(directories), len(matrix.Include), ciJobs.Common, ciJobs.SDKPython, ciJobs.SDKTypeScript, ciJobs.SDKGoIntegration, ciJobs.React, ciJobs.Site, ciJobs.ConnectorsMCP)
	if err := os.WriteFile(*githubOutputPath, []byte(contents), 0o644); err != nil {
		return fmt.Errorf("write GitHub output: %w", err)
	}
	return nil
}

func selectConnectorTestDirectories(
	catalogPath string,
	catalogDirectories []string,
	scope string,
	baseRevision string,
	headRevision string,
) ([]string, error) {
	if scope == "all" {
		return append([]string(nil), catalogDirectories...), nil
	}
	absoluteCatalogPath, err := filepath.Abs(catalogPath)
	if err != nil {
		return nil, fmt.Errorf("resolve connector catalog path: %w", err)
	}
	repositoryRoot := filepath.Dir(absoluteCatalogPath)
	changedPaths, err := changedRepositoryPaths(catalogPath, baseRevision, headRevision)
	if err != nil {
		return nil, err
	}
	mergeBase, err := gitOutput(repositoryRoot, "merge-base", baseRevision, headRevision)
	if err != nil {
		return nil, err
	}
	relativeCatalogPath, err := filepath.Rel(repositoryRoot, absoluteCatalogPath)
	if err != nil {
		return nil, fmt.Errorf("resolve connector catalog path: %w", err)
	}
	relativeCatalogPath = filepath.ToSlash(relativeCatalogPath)
	previousDirectories, err := connectorCatalogDirectoriesAtRevision(repositoryRoot, strings.TrimSpace(mergeBase), relativeCatalogPath)
	if err != nil {
		return nil, err
	}
	selected := selectConnectorDirectoriesForPaths(catalogDirectories, previousDirectories, changedPaths, relativeCatalogPath)
	return includeConnectorDependents(repositoryRoot, catalogDirectories, selected)
}

func changedRepositoryPaths(catalogPath string, baseRevision string, headRevision string) ([]string, error) {
	absoluteCatalogPath, err := filepath.Abs(catalogPath)
	if err != nil {
		return nil, fmt.Errorf("resolve connector catalog path: %w", err)
	}
	repositoryRoot := filepath.Dir(absoluteCatalogPath)
	mergeBase, err := gitOutput(repositoryRoot, "merge-base", baseRevision, headRevision)
	if err != nil {
		return nil, err
	}
	changedOutput, err := gitOutput(repositoryRoot, "diff", "--name-only", "--no-renames", "-z", strings.TrimSpace(mergeBase), headRevision)
	if err != nil {
		return nil, err
	}
	return splitNullTerminated(changedOutput), nil
}

func includeConnectorDependents(repositoryRoot string, catalogDirectories []string, selected []string) ([]string, error) {
	if len(selected) == 0 || len(selected) == len(catalogDirectories) {
		return selected, nil
	}
	selectedSet := make(map[string]bool, len(selected))
	for _, directory := range selected {
		selectedSet[directory] = true
	}
	for {
		added := false
		for _, directory := range catalogDirectories {
			if selectedSet[directory] {
				continue
			}
			contents, err := os.ReadFile(filepath.Join(repositoryRoot, directory, "go", "go.mod"))
			if err != nil {
				return nil, fmt.Errorf("read connector module %s: %w", directory, err)
			}
			module, err := modfile.Parse(directory+"/go/go.mod", contents, nil)
			if err != nil {
				return nil, fmt.Errorf("parse connector module %s: %w", directory, err)
			}
			for _, requirement := range module.Require {
				for dependency := range selectedSet {
					if requirement.Mod.Path == "github.com/superdurable/dex-connectors-library/"+dependency+"/go" {
						selectedSet[directory] = true
						added = true
						break
					}
				}
				if selectedSet[directory] {
					break
				}
			}
		}
		if !added {
			break
		}
	}
	result := make([]string, 0, len(selectedSet))
	for _, directory := range catalogDirectories {
		if selectedSet[directory] {
			result = append(result, directory)
		}
	}
	return result, nil
}

func selectCIJobsForPaths(scope string, changedPaths []string) ciJobSelection {
	if scope == "all" {
		return selectAllCIJobs()
	}
	jobs := ciJobSelection{}
	for _, path := range changedPaths {
		jobs = jobs.combinedWith(changedPathTestImpact(filepath.ToSlash(path)).ciJobs)
	}
	return jobs
}

func selectAllCIJobs() ciJobSelection {
	return ciJobSelection{Common: true, SDKPython: true, SDKTypeScript: true, SDKGoIntegration: true, React: true, Site: true, ConnectorsMCP: true}
}

func (selection ciJobSelection) combinedWith(other ciJobSelection) ciJobSelection {
	return ciJobSelection{
		Common:           selection.Common || other.Common,
		SDKPython:        selection.SDKPython || other.SDKPython,
		SDKTypeScript:    selection.SDKTypeScript || other.SDKTypeScript,
		SDKGoIntegration: selection.SDKGoIntegration || other.SDKGoIntegration,
		React:            selection.React || other.React,
		Site:             selection.Site || other.Site,
		ConnectorsMCP:    selection.ConnectorsMCP || other.ConnectorsMCP,
	}
}

func selectConnectorDirectoriesForPaths(
	directories []string,
	previousDirectories []string,
	changedPaths []string,
	catalogPath string,
) []string {
	selected := make(map[string]bool, len(directories))
	previous := make(map[string]bool, len(previousDirectories))
	for _, directory := range previousDirectories {
		previous[directory] = true
	}
	shouldSelectAll := false
	for _, changedPath := range changedPaths {
		changedPath = filepath.ToSlash(changedPath)
		if strings.HasPrefix(changedPath, "conformance/connectors/") {
			connectorPath := "connectors/" + strings.TrimPrefix(changedPath, "conformance/connectors/")
			if directory := longestConnectorDirectoryPrefix(directories, connectorPath); directory != "" {
				selected[directory] = true
			}
			continue
		}
		if changedPath == catalogPath {
			for _, directory := range directories {
				if !previous[directory] {
					selected[directory] = true
				}
			}
			continue
		}
		if directory := longestConnectorDirectoryPrefix(directories, changedPath); directory != "" {
			selected[directory] = true
			continue
		}
		if changedPathTestImpact(changedPath).shouldTestAllConnectors {
			shouldSelectAll = true
		}
	}
	if shouldSelectAll {
		return append([]string(nil), directories...)
	}
	result := make([]string, 0, len(selected))
	for _, directory := range directories {
		if selected[directory] {
			result = append(result, directory)
		}
	}
	return result
}

func longestConnectorDirectoryPrefix(directories []string, changedPath string) string {
	matched := ""
	for _, directory := range directories {
		if (changedPath == directory || strings.HasPrefix(changedPath, directory+"/")) && len(directory) > len(matched) {
			matched = directory
		}
	}
	return matched
}

// changedPathTestImpact tests everything for unlisted paths, such as component_release.py, which connector checks run.
func changedPathTestImpact(changedPath string) changedPathImpact {
	switch {
	case strings.HasPrefix(changedPath, "sdkgo/"):
		// The server links sdkgo, so an SDK change is tested against the server before release.
		return changedPathImpact{shouldTestAllConnectors: true, ciJobs: ciJobSelection{
			Common: true, SDKPython: true, SDKTypeScript: true, SDKGoIntegration: true, ConnectorsMCP: true,
		}}
	case strings.HasPrefix(changedPath, connectorsMCPServerDirectory+"/"):
		return changedPathImpact{ciJobs: ciJobSelection{ConnectorsMCP: true}}
	case strings.HasPrefix(changedPath, "sdkpython/"):
		return changedPathImpact{shouldTestAllConnectors: true, ciJobs: ciJobSelection{SDKPython: true}}
	case strings.HasPrefix(changedPath, "sdkts/"):
		return changedPathImpact{shouldTestAllConnectors: true, ciJobs: ciJobSelection{SDKTypeScript: true}}
	case strings.HasPrefix(changedPath, "sdk/react/"):
		return changedPathImpact{shouldTestAllConnectors: true, ciJobs: ciJobSelection{React: true}}
	case strings.HasPrefix(changedPath, "site/") || changedPath == "catalog.yaml":
		return changedPathImpact{ciJobs: ciJobSelection{Site: true}}
	case strings.HasPrefix(changedPath, "connectors/stripe/typescript/"):
		return changedPathImpact{ciJobs: ciJobSelection{Common: true}}
	case strings.HasPrefix(changedPath, "docs/") || strings.HasPrefix(changedPath, ".cursor/") || strings.HasPrefix(changedPath, ".codex/"):
		return changedPathImpact{ciJobs: ciJobSelection{Common: true}}
	case changedPath == "README.md" || changedPath == "AGENTS.md" || changedPath == "CLAUDE.md":
		return changedPathImpact{ciJobs: ciJobSelection{Common: true}}
	case repositoryToolingTestOnlyPaths[changedPath]:
		return changedPathImpact{ciJobs: ciJobSelection{Common: true}}
	case strings.HasPrefix(changedPath, "connectors/") || strings.HasPrefix(changedPath, "conformance/connectors/") || strings.HasPrefix(changedPath, "examples/"):
		return changedPathImpact{}
	case changedPath == ".gitignore" || changedPath == "CODE_OF_CONDUCT.md" || changedPath == "CONTRIBUTING.md":
		return changedPathImpact{}
	default:
		return changedPathImpact{shouldTestAllConnectors: true, ciJobs: selectAllCIJobs()}
	}
}

func connectorCatalogDirectoriesAtRevision(repositoryRoot string, revision string, catalogPath string) ([]string, error) {
	command := exec.Command("git", "show", revision+":"+catalogPath)
	command.Dir = repositoryRoot
	contents, err := command.Output()
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) && exitError.ExitCode() == 128 {
			return nil, nil
		}
		return nil, fmt.Errorf("read connector catalog at %s: %w", revision, err)
	}
	catalogSource, err := decodeConnectorCatalogSource(contents)
	if err != nil {
		return nil, fmt.Errorf("read connector catalog at %s: %w", revision, err)
	}
	return catalogSource.Directories, nil
}

func gitOutput(repositoryRoot string, arguments ...string) (string, error) {
	command := exec.Command("git", arguments...)
	command.Dir = repositoryRoot
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(arguments, " "), err)
	}
	return string(output), nil
}

func splitNullTerminated(contents string) []string {
	contents = strings.TrimSuffix(contents, "\x00")
	if contents == "" {
		return nil
	}
	return strings.Split(contents, "\x00")
}

func buildConnectorTestMatrix(directories []string, maximumShards int) connectorTestMatrix {
	if len(directories) == 0 {
		return connectorTestMatrix{Include: []connectorTestShard{}}
	}
	shardCount := min(len(directories), maximumShards)
	activeShards := make(map[int]bool, shardCount)
	for _, directory := range directories {
		activeShards[connectorTestShardIndex(directory, shardCount)] = true
	}
	indices := make([]int, 0, len(activeShards))
	for index := range activeShards {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	matrix := connectorTestMatrix{Include: make([]connectorTestShard, 0, len(indices))}
	for _, index := range indices {
		matrix.Include = append(matrix.Include, connectorTestShard{Index: index, Count: shardCount})
	}
	return matrix
}

func directoriesInConnectorTestShard(directories []string, shardIndex int, shardCount int) []string {
	selected := make([]string, 0, len(directories))
	for _, directory := range directories {
		if connectorTestShardIndex(directory, shardCount) == shardIndex {
			selected = append(selected, directory)
		}
	}
	return selected
}

func connectorTestShardIndex(directory string, shardCount int) int {
	digest := sha256.Sum256([]byte(directory))
	return int(binary.BigEndian.Uint64(digest[:8]) % uint64(shardCount))
}
