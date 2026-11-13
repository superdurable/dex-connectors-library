// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/superdurable/dex-connectors-library/schema"
)

// errLanguagePackageFileMissing marks a declared language whose package metadata file does not exist.
var errLanguagePackageFileMissing = errors.New("declared language package file is missing")

var pythonProjectField = regexp.MustCompile(`^\s*(name|version)\s*=\s*["']([^"']+)["']\s*(?:#.*)?$`)

func languageCapabilitiesCommand(manifestPath, languageName string, output io.Writer) error {
	manifest, err := load(manifestPath)
	if err != nil {
		return err
	}
	var language *schema.LanguagePackage
	if manifest.Spec.Languages != nil {
		switch languageName {
		case "python":
			language = manifest.Spec.Languages.Python
		case "typescript":
			language = manifest.Spec.Languages.TypeScript
		default:
			return fmt.Errorf("unsupported language: %s", languageName)
		}
	}
	if language == nil {
		return fmt.Errorf("%s does not declare spec.languages.%s", manifestPath, languageName)
	}
	return json.NewEncoder(output).Encode(struct {
		Operations []string `json:"operations"`
		Triggers   []string `json:"triggers"`
	}{Operations: language.Operations, Triggers: language.Triggers})
}

func validateLanguagePackages(manifestPath string, manifest schema.Manifest) error {
	if manifest.Spec.Languages == nil || (manifest.Spec.Languages.Python == nil && manifest.Spec.Languages.TypeScript == nil) {
		return nil
	}
	connectorRelativeDirectory, err := connectorDirectoryInCatalog(manifestPath)
	if err != nil {
		return err
	}
	connectorDirectory := filepath.Dir(manifestPath)
	if python := manifest.Spec.Languages.Python; python != nil {
		packageName := languagePackageName(connectorRelativeDirectory, "python", python.Package)
		pyprojectPath := filepath.Join(connectorDirectory, "python", "pyproject.toml")
		contents, err := os.ReadFile(pyprojectPath)
		if err != nil {
			return fmt.Errorf("spec.languages.python requires %s: %w", pyprojectPath, languagePackageFileError(err))
		}
		projectFields := make(map[string]string)
		inProject := false
		for _, line := range strings.Split(string(contents), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "[") {
				inProject = trimmed == "[project]"
			}
			if inProject {
				if fields := pythonProjectField.FindStringSubmatch(line); fields != nil {
					projectFields[fields[1]] = fields[2]
				}
			}
		}
		if projectFields["name"] != packageName || projectFields["version"] != strings.TrimPrefix(python.Version, "v") {
			return fmt.Errorf("%s [project] name and version must match spec.languages.python (%s %s)", pyprojectPath, packageName, python.Version)
		}
	}
	if typeScript := manifest.Spec.Languages.TypeScript; typeScript != nil {
		packageName := languagePackageName(connectorRelativeDirectory, "typescript", typeScript.Package)
		packagePath := filepath.Join(connectorDirectory, "typescript", "package.json")
		contents, err := os.ReadFile(packagePath)
		if err != nil {
			return fmt.Errorf("spec.languages.typescript requires %s: %w", packagePath, languagePackageFileError(err))
		}
		var declaration struct {
			Name    string                      `json:"name"`
			Version string                      `json:"version"`
			Files   *[]string                   `json:"files"`
			Exports *map[string]json.RawMessage `json:"exports"`
		}
		if err := json.Unmarshal(contents, &declaration); err != nil {
			return fmt.Errorf("parse %s: %w", packagePath, err)
		}
		if declaration.Name != packageName || declaration.Version != strings.TrimPrefix(typeScript.Version, "v") {
			return fmt.Errorf("%s name and version must match spec.languages.typescript (%s %s)", packagePath, packageName, typeScript.Version)
		}
		if declaration.Files != nil && !slices.Contains(*declaration.Files, "dex-connector.json") && !slices.Contains(*declaration.Files, "./dex-connector.json") {
			return fmt.Errorf("%s files must include dex-connector.json", packagePath)
		}
		// An exports map hides every unlisted file, so an agent host could not import the manifest.
		if declaration.Exports != nil && string((*declaration.Exports)["./dex-connector.json"]) != `"./dex-connector.json"` {
			return fmt.Errorf(`%s exports must map "./dex-connector.json" to "./dex-connector.json"`, packagePath)
		}
	}
	return nil
}

// languagePackageFileError adds errLanguagePackageFileMissing to a read error that reports a missing file.
func languagePackageFileError(err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: %w", errLanguagePackageFileMissing, err)
	}
	return err
}

func connectorDirectoryInCatalog(manifestPath string) (string, error) {
	connectorAbsoluteDirectory, err := filepath.Abs(filepath.Dir(manifestPath))
	if err != nil {
		return "", err
	}
	repositoryRoot := connectorAbsoluteDirectory
	for {
		catalogPath := filepath.Join(repositoryRoot, "catalog.yaml")
		_, statErr := os.Stat(catalogPath)
		if statErr == nil {
			break
		}
		if !errors.Is(statErr, fs.ErrNotExist) {
			return "", fmt.Errorf("find the repository catalog of %s: %w", manifestPath, statErr)
		}
		parent := filepath.Dir(repositoryRoot)
		if parent == repositoryRoot {
			return "", fmt.Errorf("language packages require a repository catalog: %s: %w", manifestPath, fs.ErrNotExist)
		}
		repositoryRoot = parent
	}
	connectorRelativeDirectory, err := filepath.Rel(repositoryRoot, connectorAbsoluteDirectory)
	if err != nil || !strings.HasPrefix(filepath.ToSlash(connectorRelativeDirectory), "connectors/") {
		return "", fmt.Errorf("language packages require a manifest below connectors/: %s", manifestPath)
	}
	return filepath.ToSlash(connectorRelativeDirectory), nil
}

func languagePackageName(directory, language, override string) string {
	if override != "" {
		return override
	}
	slug := strings.ReplaceAll(strings.TrimPrefix(directory, "connectors/"), "/", "-")
	if language == "python" {
		return "dex-connector-" + slug
	}
	return "@superdurable/connector-" + slug
}
