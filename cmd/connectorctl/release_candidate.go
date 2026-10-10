// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// connectorReleaseCandidateVersionPattern captures the release version a candidate precedes and its candidate number.
var connectorReleaseCandidateVersionPattern = regexp.MustCompile(`^(v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*))-rc\.([1-9][0-9]*)$`)

var gitCommitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

// connectorReleaseCandidate is a validated request to publish one connector's release candidate from a commit that
// is not on main.
type connectorReleaseCandidate struct {
	Directory    string
	ManifestPath string
	ModulePath   string
	Version      string
	Tag          string
	SourceSHA    string
	DisplayName  string
	// IsTagPublished reports that the tag already names the candidate commit, so publishing repeats safely.
	IsTagPublished bool
}

func releaseCandidateCommand(args []string) error {
	flags := flag.NewFlagSet("release-candidate", flag.ContinueOnError)
	catalogPath := flags.String("catalog", "", "connector catalog source of the main checkout")
	candidateRoot := flags.String("candidate-root", "", "checkout of the candidate commit")
	directory := flags.String("directory", "", "registered connector directory of the candidate")
	commit := flags.String("commit", "", "candidate commit SHA")
	version := flags.String("version", "", "release candidate version, vX.Y.Z-rc.N")
	githubOutputPath := flags.String("github-output", "", "GitHub Actions output path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *catalogPath == "" || *candidateRoot == "" || *directory == "" || *commit == "" || *version == "" {
		return errors.New("usage: connectorctl release-candidate --catalog PATH --candidate-root PATH --directory DIRECTORY --commit SHA --version vX.Y.Z-rc.N [--github-output PATH]")
	}
	candidate, err := planConnectorReleaseCandidate(*catalogPath, *candidateRoot, *directory, *commit, *version)
	if err != nil {
		return err
	}
	outputs := fmt.Sprintf("directory=%s\nmanifest_path=%s\nmodule_path=%s\nversion=%s\ntag=%s\nsource_sha=%s\ndisplay_name=%s\ntag_published=%t\n",
		candidate.Directory, candidate.ManifestPath, candidate.ModulePath, candidate.Version, candidate.Tag,
		candidate.SourceSHA, candidate.DisplayName, candidate.IsTagPublished)
	if *githubOutputPath == "" {
		_, err = fmt.Print(outputs)
		return err
	}
	githubOutput, err := os.OpenFile(*githubOutputPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open GitHub Actions output: %w", err)
	}
	if _, err = githubOutput.WriteString(outputs); err != nil {
		_ = githubOutput.Close() // the write error is the one reported
		return fmt.Errorf("write GitHub Actions output: %w", err)
	}
	return githubOutput.Close()
}

// planConnectorReleaseCandidate validates a candidate against main, whose checkout holds catalogPath: the commit
// changes only the connector's own module (a new connector may also register itself and add its company logo), its
// manifest declares the release the candidate precedes, that release is after the latest one reachable from main,
// and the tag is free or already names the commit.
func planConnectorReleaseCandidate(catalogPath, candidateRoot, directory, commit, version string) (connectorReleaseCandidate, error) {
	match := connectorReleaseCandidateVersionPattern.FindStringSubmatch(version)
	if match == nil {
		return connectorReleaseCandidate{}, fmt.Errorf("release candidate version %s is not vX.Y.Z-rc.N", version)
	}
	releaseVersion := match[1]
	if !gitCommitPattern.MatchString(commit) {
		return connectorReleaseCandidate{}, fmt.Errorf("candidate commit %s is not a full commit SHA", commit)
	}
	if err := validateConnectorDirectory(directory); err != nil {
		return connectorReleaseCandidate{}, err
	}
	repositoryRoot := filepath.Dir(catalogPath)
	checkedOut, err := gitRevisionCommit(candidateRoot, "HEAD")
	if err != nil {
		return connectorReleaseCandidate{}, err
	}
	if checkedOut != commit {
		return connectorReleaseCandidate{}, fmt.Errorf("candidate checkout is at %s, not %s", checkedOut, commit)
	}
	mainSource, err := loadConnectorCatalogSource(catalogPath)
	if err != nil {
		return connectorReleaseCandidate{}, err
	}
	if err := validateReleaseCandidateChanges(repositoryRoot, directory, commit, !slices.Contains(mainSource.Directories, directory)); err != nil {
		return connectorReleaseCandidate{}, err
	}
	entries, err := loadConnectorDirectoryEntries(filepath.Join(candidateRoot, "catalog.yaml"))
	if err != nil {
		return connectorReleaseCandidate{}, fmt.Errorf("candidate catalog: %w", err)
	}
	var entry *connectorDirectoryEntry
	for index := range entries {
		if entries[index].Directory == directory {
			entry = &entries[index]
		}
	}
	if entry == nil {
		return connectorReleaseCandidate{}, fmt.Errorf("candidate does not register %s in catalog.yaml", directory)
	}
	if entry.Manifest.Metadata.Version != releaseVersion {
		return connectorReleaseCandidate{}, fmt.Errorf("%s declares %s, but %s is a candidate of %s",
			entry.ManifestPath, entry.Manifest.Metadata.Version, version, releaseVersion)
	}
	latestReleases, err := latestReachableConnectorReleases(repositoryRoot)
	if err != nil {
		return connectorReleaseCandidate{}, err
	}
	isPending, err := validateConnectorVersionTransition(latestReleases[directory].Version, releaseVersion)
	if err != nil {
		return connectorReleaseCandidate{}, fmt.Errorf("%s: %w", directory, err)
	}
	if !isPending {
		return connectorReleaseCandidate{}, fmt.Errorf("%s %s is already released", directory, releaseVersion)
	}
	tag := directory + "/" + version
	isTagPublished, err := isTagAtCommit(repositoryRoot, tag, commit)
	if err != nil {
		return connectorReleaseCandidate{}, err
	}
	return connectorReleaseCandidate{
		Directory: directory, ManifestPath: entry.ManifestPath, ModulePath: entry.ModulePath, Version: version, Tag: tag,
		SourceSHA: commit, DisplayName: entry.Manifest.Metadata.DisplayName, IsTagPublished: isTagPublished,
	}, nil
}

// validateReleaseCandidateChanges requires every path the commit changes since it left main to belong to the
// connector, so a candidate cannot change the SDK, the tooling or another connector.
func validateReleaseCandidateChanges(repositoryRoot, directory, commit string, isNewConnector bool) error {
	mergeBase, err := gitOutput(repositoryRoot, "merge-base", commit, "HEAD")
	if err != nil {
		return err
	}
	changed, err := gitOutput(repositoryRoot, "diff", "--name-only", "-z", strings.TrimSpace(mergeBase), commit)
	if err != nil {
		return err
	}
	company, err := connectorCompanyDirectory(directory)
	if err != nil {
		return err
	}
	companyLogo := "connectors/" + company + "/logo.svg"
	changesConnector := false
	for _, path := range splitNullTerminated(changed) {
		switch {
		case strings.HasPrefix(path, directory+"/"):
			changesConnector = true
		case isNewConnector && path == "catalog.yaml":
		case path == companyLogo && !gitPathExists(repositoryRoot, "HEAD", companyLogo):
		default:
			return fmt.Errorf("candidate changes %s outside %s", strconv.Quote(path), directory)
		}
	}
	if !changesConnector {
		return fmt.Errorf("candidate does not change %s", directory)
	}
	return nil
}

func isTagAtCommit(repositoryRoot, tag, commit string) (bool, error) {
	if !gitPathExists(repositoryRoot, "refs/tags/"+tag, "") {
		return false, nil
	}
	tagged, err := gitRevisionCommit(repositoryRoot, "refs/tags/"+tag)
	if err != nil {
		return false, err
	}
	if tagged != commit {
		return false, fmt.Errorf("release candidate tag %s already names %s", tag, tagged)
	}
	return true, nil
}

// gitPathExists reports whether revision exists, or names path when path is not empty.
func gitPathExists(repositoryRoot, revision, path string) bool {
	object := revision
	if path != "" {
		object = revision + ":" + path
	}
	command := exec.Command("git", "cat-file", "-e", object)
	command.Dir = repositoryRoot
	return command.Run() == nil
}
