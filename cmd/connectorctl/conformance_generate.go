// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

//go:embed conformance_generated.go.txt
var conformanceGeneratedTemplate string

func generatedConformanceTestFiles(manifestPath string) (map[string][]byte, error) {
	connectorDirectory := filepath.Dir(manifestPath)
	adapterPath := filepath.Join(connectorDirectory, "go", "conformance_adapter_test.go")
	generatedPath := filepath.Join(connectorDirectory, "go", "zz_generated_conformance_test.go")
	if _, err := os.Stat(adapterPath); os.IsNotExist(err) {
		if _, staleError := os.Stat(generatedPath); staleError == nil {
			return nil, fmt.Errorf("generated conformance test has no adapter: %s", generatedPath)
		} else if !os.IsNotExist(staleError) {
			return nil, staleError
		}
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	syntax, err := parser.ParseFile(token.NewFileSet(), adapterPath, nil, parser.PackageClauseOnly)
	if err != nil {
		return nil, fmt.Errorf("parse conformance adapter: %w", err)
	}
	repositoryRoot := connectorDirectory
	for {
		if _, err := os.Stat(filepath.Join(repositoryRoot, "catalog.yaml")); err == nil {
			break
		}
		parent := filepath.Dir(repositoryRoot)
		if parent == repositoryRoot {
			return nil, fmt.Errorf("find catalog for %s", manifestPath)
		}
		repositoryRoot = parent
	}
	relativeConnector, err := filepath.Rel(filepath.Join(repositoryRoot, "connectors"), connectorDirectory)
	if err != nil {
		return nil, err
	}
	fixture := filepath.Join(repositoryRoot, "conformance", "connectors", relativeConnector, "scenarios.json")
	if _, err := os.Stat(fixture); err != nil {
		return nil, fmt.Errorf("conformance fixture %s: %w", fixture, err)
	}
	relativeFixture, err := filepath.Rel(filepath.Join(connectorDirectory, "go"), fixture)
	if err != nil {
		return nil, err
	}
	matcherFixture := filepath.Join(repositoryRoot, "conformance", "runners", "matcher_cases.json")
	relativeMatcherFixture, err := filepath.Rel(filepath.Join(connectorDirectory, "go"), matcherFixture)
	if err != nil {
		return nil, err
	}
	source := strings.ReplaceAll(conformanceGeneratedTemplate, "{{PACKAGE}}", syntax.Name.Name)
	source = strings.ReplaceAll(source, "{{FIXTURE}}", fmt.Sprintf("%q", filepath.ToSlash(relativeFixture)))
	source = strings.ReplaceAll(source, "{{MATCHER_FIXTURE}}", fmt.Sprintf("%q", filepath.ToSlash(relativeMatcherFixture)))
	formatted, err := format.Source([]byte(source))
	if err != nil {
		return nil, fmt.Errorf("format conformance test: %w", err)
	}
	return map[string][]byte{generatedPath: bytes.Clone(formatted)}, nil
}
