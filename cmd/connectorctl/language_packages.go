// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/superdurable/dex-connectors-library/schema"
)

var pythonProjectField = regexp.MustCompile(`^\s*(name|version)\s*=\s*["']([^"']+)["']\s*(?:#.*)?$`)

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
			return fmt.Errorf("spec.languages.python requires %s: %w", pyprojectPath, err)
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
			return fmt.Errorf("spec.languages.typescript requires %s: %w", packagePath, err)
		}
		var declaration struct {
			Name    string    `json:"name"`
			Version string    `json:"version"`
			Files   *[]string `json:"files"`
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
	}
	return nil
}

func connectorDirectoryInCatalog(manifestPath string) (string, error) {
	connectorAbsoluteDirectory, err := filepath.Abs(filepath.Dir(manifestPath))
	if err != nil {
		return "", err
	}
	repositoryRoot := connectorAbsoluteDirectory
	for {
		if _, err := os.Stat(filepath.Join(repositoryRoot, "catalog.yaml")); err == nil {
			break
		}
		parent := filepath.Dir(repositoryRoot)
		if parent == repositoryRoot {
			return "", fmt.Errorf("language packages require a repository catalog: %s", manifestPath)
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
