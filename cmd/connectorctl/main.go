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
		return errors.New("usage: connectorctl <validate|catalog|release-matrix|generate|ui-artifact|release-artifact> [path ...]")
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
		return catalogCommand(args[1:])
	case "release-matrix":
		return releaseMatrixCommand(args[1:])
	case "generate":
		return generate(args[1:])
	case "ui-artifact":
		return uiArtifact(args[1:])
	case "release-artifact":
		return releaseArtifact(args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
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
	if manifest.Metadata.Version != *version {
		return fmt.Errorf("release version %s does not match manifest version %s", *version, manifest.Metadata.Version)
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
