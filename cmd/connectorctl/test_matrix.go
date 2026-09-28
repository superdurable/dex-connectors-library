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
)

type connectorTestMatrix struct {
	Include []connectorTestShard `json:"include"`
}

type connectorTestShard struct {
	Index int `json:"shard_index"`
	Count int `json:"shard_count"`
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
	contents := fmt.Sprintf("matrix=%s\ncount=%d\njobs=%d\n", encoded, len(directories), len(matrix.Include))
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
	mergeBase, err := gitOutput(repositoryRoot, "merge-base", baseRevision, headRevision)
	if err != nil {
		return nil, err
	}
	changedOutput, err := gitOutput(repositoryRoot, "diff", "--name-only", "--no-renames", "-z", strings.TrimSpace(mergeBase), headRevision)
	if err != nil {
		return nil, err
	}
	changedPaths := splitNullTerminated(changedOutput)
	relativeCatalogPath, err := filepath.Rel(repositoryRoot, absoluteCatalogPath)
	if err != nil {
		return nil, fmt.Errorf("resolve connector catalog path: %w", err)
	}
	relativeCatalogPath = filepath.ToSlash(relativeCatalogPath)
	previousDirectories, err := connectorCatalogDirectoriesAtRevision(repositoryRoot, strings.TrimSpace(mergeBase), relativeCatalogPath)
	if err != nil {
		return nil, err
	}
	return selectConnectorDirectoriesForPaths(catalogDirectories, previousDirectories, changedPaths, relativeCatalogPath), nil
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
		if isConnectorTestNeutralPath(changedPath) || strings.HasPrefix(changedPath, "connectors/") {
			continue
		}
		shouldSelectAll = true
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

func isConnectorTestNeutralPath(changedPath string) bool {
	for _, prefix := range []string{"docs/", "site/", ".cursor/", ".codex/", "examples/"} {
		if strings.HasPrefix(changedPath, prefix) {
			return true
		}
	}
	switch changedPath {
	case "README.md", "AGENTS.md", "CLAUDE.md", ".gitignore", "CODE_OF_CONDUCT.md", "CONTRIBUTING.md":
		return true
	default:
		return false
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
