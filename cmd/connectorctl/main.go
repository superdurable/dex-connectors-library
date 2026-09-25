// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/internal/codegen"
	"github.com/superdurable/dex-connectors-library/schema"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: connectorctl <validate|catalog|generate|release-workflow|ui-artifact|release-artifact> [path ...]")
	}
	switch args[0] {
	case "validate":
		if len(args) < 2 {
			return errors.New("usage: connectorctl validate <manifest ...>")
		}
		for _, path := range args[1:] {
			manifest, err := load(path)
			if err != nil {
				return err
			}
			fmt.Printf("valid %s\n", manifest.Metadata.Name)
		}
		return nil
	case "catalog":
		root := "connectors"
		if len(args) > 1 {
			root = args[1]
		}
		manifests, err := catalog(root)
		if err != nil {
			return err
		}
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(manifests)
	case "generate":
		return generate(args[1:])
	case "release-workflow":
		return releaseWorkflow(args[1:])
	case "ui-artifact":
		return uiArtifact(args[1:])
	case "release-artifact":
		return releaseArtifact(args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

type connectorCatalogEntry struct {
	Slug         string
	ConnectorID  string
	ManifestPath string
	ModulePath   string
}

func releaseWorkflow(args []string) error {
	check := false
	if len(args) > 0 && args[0] == "--check" {
		check = true
		args = args[1:]
	}
	if len(args) != 2 {
		return errors.New("usage: connectorctl release-workflow [--check] <connectors-root> <output>")
	}
	entries, err := connectorReleaseCatalog(args[0])
	if err != nil {
		return err
	}
	generated := generateReleaseWorkflow(entries)
	if check {
		current, readErr := os.ReadFile(args[1])
		if readErr != nil {
			return fmt.Errorf("release workflow is missing: %s", args[1])
		}
		if !bytes.Equal(current, generated) {
			return fmt.Errorf("release workflow is stale: %s", args[1])
		}
		return nil
	}
	if err := os.WriteFile(args[1], generated, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", args[1], err)
	}
	return nil
}

func connectorReleaseCatalog(root string) ([]connectorCatalogEntry, error) {
	var entries []connectorCatalogEntry
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Name() != "connector.yaml" {
			return nil
		}
		manifest, loadErr := load(path)
		if loadErr != nil {
			return loadErr
		}
		directory := filepath.Dir(path)
		relative, relativeErr := filepath.Rel(root, directory)
		if relativeErr != nil {
			return relativeErr
		}
		slug := filepath.ToSlash(relative)
		if slug == "." || strings.HasPrefix(slug, "../") {
			return fmt.Errorf("connector manifest is outside catalog: %s", path)
		}
		module, moduleErr := readModulePath(filepath.Join(directory, "go.mod"))
		if moduleErr != nil {
			return moduleErr
		}
		if !strings.HasSuffix(module, "/connectors/"+slug) {
			return fmt.Errorf("connector module %s must end in /connectors/%s", module, slug)
		}
		entries = append(entries, connectorCatalogEntry{Slug: slug, ConnectorID: manifest.Metadata.Name, ManifestPath: filepath.ToSlash(path), ModulePath: module})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan connector release catalog: %w", err)
	}
	sort.Slice(entries, func(left, right int) bool { return entries[left].Slug < entries[right].Slug })
	if len(entries) == 0 {
		return nil, fmt.Errorf("connector catalog is empty: %s", root)
	}
	for index := 1; index < len(entries); index++ {
		if entries[index-1].Slug == entries[index].Slug {
			return nil, fmt.Errorf("duplicate connector slug: %s", entries[index].Slug)
		}
	}
	connectorIDs := map[string]bool{}
	for _, entry := range entries {
		if connectorIDs[entry.ConnectorID] {
			return nil, fmt.Errorf("duplicate connector ID: %s", entry.ConnectorID)
		}
		connectorIDs[entry.ConnectorID] = true
	}
	return entries, nil
}

func readModulePath(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read connector module %s: %w", path, err)
	}
	for _, line := range strings.Split(string(content), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "module ") {
			module := strings.TrimSpace(strings.TrimPrefix(trimmed, "module "))
			if module != "" {
				return module, nil
			}
		}
	}
	return "", fmt.Errorf("connector module does not declare a module path: %s", path)
}

func generateReleaseWorkflow(entries []connectorCatalogEntry) []byte {
	var options strings.Builder
	for _, entry := range entries {
		fmt.Fprintf(&options, "          - %s\n", entry.Slug)
	}
	workflow := `# Code generated by connectorctl release-workflow. DO NOT EDIT.
name: Release Connector

on:
  workflow_dispatch:
    inputs:
      connector:
        description: Connector module to release
        required: true
        type: choice
        options:
` + options.String() + `      bump:
        description: Semantic version component to increment
        required: true
        default: minor
        type: choice
        options:
          - minor
          - major
          - patch

permissions:
  contents: write
  pull-requests: read

concurrency:
  group: release-connector-${{ inputs.connector }}
  cancel-in-progress: false

jobs:
  release:
    if: github.ref == 'refs/heads/main'
    runs-on: ubuntu-latest
    env:
      COMPONENT_PATH: connectors/${{ inputs.connector }}
      MANIFEST_PATH: connectors/${{ inputs.connector }}/connector.yaml
      TAG_PREFIX: connectors/${{ inputs.connector }}/
    steps:
      - uses: actions/checkout@v6
        with:
          fetch-depth: 0
      - uses: actions/setup-go@v6
        with:
          go-version-file: go.mod
          cache-dependency-path: |
            go.sum
            sdk/go/go.sum
            connectors/${{ inputs.connector }}/go.sum
      - uses: actions/setup-node@v6
        with:
          node-version: 24
      - name: Check generated release dropdown and connector code
        env:
          GOWORK: "off"
        run: |
          go run ./cmd/connectorctl release-workflow --check connectors .github/workflows/release-connector.yml
          go run ./cmd/connectorctl generate --check "${MANIFEST_PATH}"
      - name: Validate published SDK dependency
        run: |
          python3 script/release/component_release.py validate-connector \
            --component-path "${COMPONENT_PATH}" \
            --sdk-module github.com/superdurable/dex-connectors-library/sdk/go
      - name: Test standalone connector module
        working-directory: ${{ env.COMPONENT_PATH }}
        env:
          GOWORK: "off"
        run: |
          go test -race ./...
          go vet ./...
      - name: Build and test Connector Studio UI
        run: |
          if [ -f "${COMPONENT_PATH}/ui/package-lock.json" ]; then
            npm ci --prefix sdk/react
            npm run build --prefix sdk/react
            npm ci --prefix "${COMPONENT_PATH}/ui"
            npm test --prefix "${COMPONENT_PATH}/ui"
            npm run build --prefix "${COMPONENT_PATH}/ui"
            go run ./cmd/connectorctl ui-artifact \
              --manifest "${MANIFEST_PATH}" \
              --ui-root "${COMPONENT_PATH}/ui/dist" \
              --output /tmp/connector-ui.tgz \
              --digest-output /tmp/connector-ui.tgz.sha256
          fi
      - name: Plan connector release
        id: plan
        run: |
          python3 script/release/component_release.py plan \
            --component-path "${COMPONENT_PATH}" \
            --tag-prefix "${TAG_PREFIX}" \
            --bump "${{ inputs.bump }}" \
            --ref "${GITHUB_REF}" \
            --json-output /tmp/connector-release-plan.json \
            --github-output "${GITHUB_OUTPUT}"
      - name: Build path-scoped release notes
        env:
          GH_TOKEN: ${{ github.token }}
        run: |
          python3 script/release/component_release.py notes \
            --plan /tmp/connector-release-plan.json \
            --repository "${GITHUB_REPOSITORY}" \
            --output /tmp/connector-release-notes.md
      - name: Build versioned connector artifact
        env:
          GOWORK: "off"
          RELEASE_TAG: ${{ steps.plan.outputs.tag }}
          RELEASE_VERSION: ${{ steps.plan.outputs.version }}
          MODULE_PATH: ${{ steps.plan.outputs.module_path }}
        run: |
          UI_ARGS=()
          if [ -f /tmp/connector-ui.tgz ]; then
            UI_ARGS+=(--ui-artifact /tmp/connector-ui.tgz --ui-digest /tmp/connector-ui.tgz.sha256)
          fi
          go run ./cmd/connectorctl release-artifact \
            --manifest "${MANIFEST_PATH}" \
            --module-path "${MODULE_PATH}" \
            --version "${RELEASE_VERSION}" \
            --tag "${RELEASE_TAG}" \
            --source-sha "${GITHUB_SHA}" \
            --output /tmp/connector-release.json \
            --digest-output /tmp/connector-release.json.sha256 \
            "${UI_ARGS[@]}"
      - name: Create connector release
        env:
          GH_TOKEN: ${{ github.token }}
          RELEASE_TAG: ${{ steps.plan.outputs.tag }}
          RELEASE_VERSION: ${{ steps.plan.outputs.version }}
        run: |
          ASSETS=(/tmp/connector-release.json /tmp/connector-release.json.sha256)
          if [ -f /tmp/connector-ui.tgz ]; then
            ASSETS+=(/tmp/connector-ui.tgz /tmp/connector-ui.tgz.sha256)
          fi
          gh release create "${RELEASE_TAG}" \
            --target "${GITHUB_SHA}" \
            --title "Connector ${{ inputs.connector }} ${RELEASE_VERSION}" \
            --notes-file /tmp/connector-release-notes.md \
            "${ASSETS[@]}"
      - name: Verify published connector module
        env:
          MODULE_PATH: ${{ steps.plan.outputs.module_path }}
          RELEASE_VERSION: ${{ steps.plan.outputs.version }}
        run: |
          deadline=$((SECONDS + 120))
          until GOWORK=off GOPROXY=direct GONOSUMDB=github.com/superdurable/dex-connectors-library \
            go mod download "${MODULE_PATH}@${RELEASE_VERSION}"; do
            if (( SECONDS >= deadline )); then
              echo "Published connector module did not become downloadable before the deadline" >&2
              exit 1
            fi
            sleep 2
          done

  reject-non-main:
    if: github.ref != 'refs/heads/main'
    runs-on: ubuntu-latest
    steps:
      - run: |
          echo "Connector releases can run only from main" >&2
          exit 1
`
	return []byte(workflow)
}

type connectorReleaseArtifact struct {
	ConnectorID    string              `json:"connectorId"`
	Manifest       schema.Manifest     `json:"manifest"`
	ModulePath     string              `json:"modulePath"`
	Version        string              `json:"version"`
	Tag            string              `json:"tag"`
	SourceSHA      string              `json:"sourceSha"`
	ManifestSHA256 string              `json:"manifestSha256"`
	UI             *connectorUIRelease `json:"ui,omitempty"`
}

type connectorUIRelease struct {
	Artifact      string   `json:"artifact"`
	SHA256        string   `json:"sha256"`
	Entrypoint    string   `json:"entrypoint"`
	HostAPIRange  string   `json:"hostApiRange"`
	Capabilities  []string `json:"backendCapabilities"`
	MockScenarios []string `json:"mockScenarios"`
	Icon          string   `json:"icon"`
}

func releaseArtifact(args []string) error {
	flags := flag.NewFlagSet("release-artifact", flag.ContinueOnError)
	manifestPath := flags.String("manifest", "", "connector manifest path")
	modulePath := flags.String("module-path", "", "connector Go module path")
	version := flags.String("version", "", "release version")
	tag := flags.String("tag", "", "full component tag")
	sourceSHA := flags.String("source-sha", "", "source commit SHA")
	output := flags.String("output", "", "artifact output path")
	digestOutput := flags.String("digest-output", "", "artifact digest output path")
	uiArtifactPath := flags.String("ui-artifact", "", "Connector Studio UI tarball")
	uiDigestPath := flags.String("ui-digest", "", "Connector Studio UI digest")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *manifestPath == "" || *modulePath == "" || *version == "" || *tag == "" || *sourceSHA == "" || *output == "" || *digestOutput == "" {
		return errors.New("usage: connectorctl release-artifact --manifest PATH --module-path PATH --version VERSION --tag TAG --source-sha SHA --output PATH --digest-output PATH")
	}
	manifestBytes, err := os.ReadFile(*manifestPath)
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}
	manifest, err := load(*manifestPath)
	if err != nil {
		return err
	}
	manifestDigest := fmt.Sprintf("%x", sha256.Sum256(manifestBytes))
	artifact := connectorReleaseArtifact{
		ConnectorID: manifest.Metadata.Name, Manifest: manifest, ModulePath: *modulePath,
		Version: *version, Tag: *tag, SourceSHA: *sourceSHA, ManifestSHA256: manifestDigest,
	}
	if (*uiArtifactPath == "") != (*uiDigestPath == "") {
		return errors.New("ui-artifact and ui-digest must be provided together")
	}
	if manifest.Spec.Studio != nil {
		if *uiArtifactPath == "" {
			return errors.New("manifest declares Studio UI but no UI artifact was provided")
		}
		digest, digestErr := readDigest(*uiDigestPath, filepath.Base(*uiArtifactPath))
		if digestErr != nil {
			return digestErr
		}
		uiBytes, readErr := os.ReadFile(*uiArtifactPath)
		if readErr != nil {
			return fmt.Errorf("read UI artifact: %w", readErr)
		}
		if actual := fmt.Sprintf("%x", sha256.Sum256(uiBytes)); actual != digest {
			return errors.New("UI artifact digest does not match artifact")
		}
		setup := manifest.Spec.Studio.Setup
		artifact.UI = &connectorUIRelease{
			Artifact: filepath.Base(*uiArtifactPath), SHA256: digest, Entrypoint: setup.Entrypoint,
			HostAPIRange: setup.HostAPIRange, Capabilities: setup.BackendCapabilities,
			MockScenarios: setup.MockScenarios, Icon: setup.Icon,
		}
	} else if *uiArtifactPath != "" {
		return errors.New("UI artifact requires spec.studio metadata")
	}
	encoded, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		return fmt.Errorf("encode release artifact: %w", err)
	}
	encoded = append(encoded, '\n')
	if err := os.WriteFile(*output, encoded, 0o644); err != nil {
		return fmt.Errorf("write release artifact: %w", err)
	}
	artifactDigest := fmt.Sprintf("%x  %s\n", sha256.Sum256(encoded), filepath.Base(*output))
	if err := os.WriteFile(*digestOutput, []byte(artifactDigest), 0o644); err != nil {
		return fmt.Errorf("write release artifact digest: %w", err)
	}
	return nil
}

const (
	maximumUIFiles = 128
	maximumUIBytes = 8 << 20
)

var (
	activeSVGAttribute   = regexp.MustCompile(`(?i)\bon[a-z]+\s*=`)
	externalSVGReference = regexp.MustCompile(`(?i)(?:href|xlink:href)\s*=\s*["']\s*(?:https?:|//|data:)`)
)

func uiArtifact(args []string) error {
	flags := flag.NewFlagSet("ui-artifact", flag.ContinueOnError)
	manifestPath := flags.String("manifest", "", "connector manifest path")
	uiRoot := flags.String("ui-root", "", "built Connector Studio UI directory")
	output := flags.String("output", "", "UI tarball output path")
	digestOutput := flags.String("digest-output", "", "UI tarball digest output path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *manifestPath == "" || *uiRoot == "" || *output == "" || *digestOutput == "" {
		return errors.New("usage: connectorctl ui-artifact --manifest PATH --ui-root DIRECTORY --output PATH --digest-output PATH")
	}
	manifest, err := load(*manifestPath)
	if err != nil {
		return err
	}
	if manifest.Spec.Studio == nil {
		return errors.New("manifest does not declare spec.studio")
	}
	files, err := studioUIFiles(*uiRoot)
	if err != nil {
		return err
	}
	required := map[string]bool{
		manifest.Spec.Studio.Setup.Entrypoint: false,
		manifest.Spec.Studio.Setup.Icon:       false,
	}
	for _, file := range files {
		if _, ok := required[file]; ok {
			required[file] = true
		}
	}
	for path, present := range required {
		if !present {
			return fmt.Errorf("Studio UI asset is missing: %s", path)
		}
	}
	archive, err := os.Create(*output)
	if err != nil {
		return fmt.Errorf("create UI artifact: %w", err)
	}
	failed := true
	defer func() {
		_ = archive.Close()
		if failed {
			_ = os.Remove(*output)
		}
	}()
	digest := sha256.New()
	gzipWriter, err := gzip.NewWriterLevel(io.MultiWriter(archive, digest), gzip.BestCompression)
	if err != nil {
		return fmt.Errorf("create deterministic gzip: %w", err)
	}
	gzipWriter.Header.ModTime = time.Unix(0, 0)
	gzipWriter.Header.OS = 255
	tarWriter := tar.NewWriter(gzipWriter)
	for _, relative := range files {
		content, readErr := os.ReadFile(filepath.Join(*uiRoot, filepath.FromSlash(relative)))
		if readErr != nil {
			return fmt.Errorf("read Studio UI asset %s: %w", relative, readErr)
		}
		if strings.HasSuffix(strings.ToLower(relative), ".svg") {
			lower := strings.ToLower(string(content))
			for _, prohibited := range []string{"<script", "<foreignobject", "<iframe", "<object", "<embed", "javascript:"} {
				if strings.Contains(lower, prohibited) {
					return fmt.Errorf("Studio UI SVG contains prohibited active content: %s", relative)
				}
			}
			if activeSVGAttribute.Match(content) || externalSVGReference.Match(content) {
				return fmt.Errorf("Studio UI SVG contains prohibited active content: %s", relative)
			}
		}
		header := &tar.Header{
			Name: relative, Mode: 0o644, Size: int64(len(content)),
			ModTime: time.Unix(0, 0), AccessTime: time.Unix(0, 0), ChangeTime: time.Unix(0, 0),
			Typeflag: tar.TypeReg, Format: tar.FormatPAX,
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			return fmt.Errorf("write Studio UI header: %w", err)
		}
		if _, err := tarWriter.Write(content); err != nil {
			return fmt.Errorf("write Studio UI asset: %w", err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		return fmt.Errorf("close Studio UI tar: %w", err)
	}
	if err := gzipWriter.Close(); err != nil {
		return fmt.Errorf("close Studio UI gzip: %w", err)
	}
	if err := archive.Close(); err != nil {
		return fmt.Errorf("close Studio UI artifact: %w", err)
	}
	digestLine := fmt.Sprintf("%x  %s\n", digest.Sum(nil), filepath.Base(*output))
	if err := os.WriteFile(*digestOutput, []byte(digestLine), 0o644); err != nil {
		return fmt.Errorf("write Studio UI digest: %w", err)
	}
	failed = false
	return nil
}

func studioUIFiles(root string) ([]string, error) {
	var files []string
	var total int64
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || (!entry.IsDir() && !info.Mode().IsRegular()) {
			return fmt.Errorf("Studio UI contains unsupported file: %s", path)
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if strings.HasPrefix(relative, "../") || filepath.IsAbs(relative) {
			return fmt.Errorf("Studio UI asset escapes root: %s", path)
		}
		files = append(files, relative)
		total += info.Size()
		if len(files) > maximumUIFiles || total > maximumUIBytes {
			return fmt.Errorf("Studio UI exceeds %d files or %d bytes", maximumUIFiles, maximumUIBytes)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan Studio UI: %w", err)
	}
	if len(files) == 0 {
		return nil, errors.New("Studio UI directory is empty")
	}
	sort.Strings(files)
	return files, nil
}

func readDigest(path string, expectedFile string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read digest: %w", err)
	}
	fields := strings.Fields(string(content))
	if len(fields) != 2 || fields[1] != expectedFile || len(fields[0]) != sha256.Size*2 {
		return "", errors.New("invalid UI artifact digest")
	}
	for _, character := range fields[0] {
		if !strings.ContainsRune("0123456789abcdef", character) {
			return "", errors.New("invalid UI artifact digest")
		}
	}
	return fields[0], nil
}

func generate(args []string) error {
	check := false
	if len(args) > 0 && args[0] == "--check" {
		check = true
		args = args[1:]
	}
	if len(args) != 1 {
		return errors.New("usage: connectorctl generate [--check] <manifest>")
	}
	manifest, err := load(args[0])
	if err != nil {
		return err
	}
	generated, err := codegen.Generate(manifest)
	if err != nil {
		return err
	}
	output := filepath.Join(filepath.Dir(args[0]), codegen.OutputFile)
	if check {
		current, readErr := os.ReadFile(output)
		if readErr != nil {
			return fmt.Errorf("generated connector is missing: %s", output)
		}
		if !bytes.Equal(current, generated) {
			return fmt.Errorf("generated connector is stale: %s", output)
		}
		return nil
	}
	if err := os.WriteFile(output, generated, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", output, err)
	}
	return nil
}

func load(path string) (schema.Manifest, error) {
	file, err := os.Open(path)
	if err != nil {
		return schema.Manifest{}, fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()
	manifest, err := schema.Decode(file)
	if err != nil {
		return schema.Manifest{}, fmt.Errorf("validate %s: %w", path, err)
	}
	return manifest, nil
}

func catalog(root string) ([]schema.Manifest, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && entry.Name() == "connector.yaml" {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan catalog: %w", err)
	}
	sort.Strings(paths)
	manifests := make([]schema.Manifest, 0, len(paths))
	for _, path := range paths {
		manifest, loadErr := load(path)
		if loadErr != nil {
			return nil, loadErr
		}
		manifests = append(manifests, manifest)
	}
	return manifests, nil
}
