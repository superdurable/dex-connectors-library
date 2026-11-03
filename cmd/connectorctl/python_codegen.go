// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"

	"github.com/superdurable/dex-connectors-library/internal/codegen"
	"github.com/superdurable/dex-connectors-library/internal/codegen/gomodel"
	"github.com/superdurable/dex-connectors-library/schema"
)

//go:embed python_codegen.py
var pythonCodegenSource string

type pythonCodegenInput struct {
	Manifest             schema.Manifest     `json:"manifest"`
	Models               []gomodel.Model     `json:"models"`
	PackageName          string              `json:"packageName"`
	MockPackageName      string              `json:"mockPackageName"`
	ModelAliases         map[string]string   `json:"modelAliases"`
	SharedRequiredFields map[string][]string `json:"sharedRequiredFields"`
}

var pythonModelAliasPattern = regexp.MustCompile(`(?m)^# connectorctl: model-alias ([A-Za-z_][A-Za-z_0-9]*)=([A-Za-z_][A-Za-z_0-9]*)$`)
var pythonPublishedModelPattern = regexp.MustCompile(`(?m)^class ([A-Za-z][A-Za-z_0-9]*):$`)

func generatedPythonFiles(manifestPath string, manifest schema.Manifest) (map[string][]byte, error) {
	files := make(map[string][]byte)
	if manifest.Spec.Languages == nil || manifest.Spec.Languages.Python == nil {
		return files, nil
	}
	connectorDirectory, err := connectorDirectoryInCatalog(manifestPath)
	if err != nil {
		return nil, err
	}
	packageName := pythonImportPackageName(connectorDirectory)
	packageDirectory := filepath.Join(filepath.Dir(manifestPath), "python", "src", "dex_connectors", packageName)
	if _, err := os.Stat(filepath.Join(packageDirectory, "__init__.py")); os.IsNotExist(err) {
		return files, nil
	} else if err != nil {
		return nil, err
	}
	rootNames := make([]string, 0, len(manifest.Spec.Languages.Python.Operations)*2)
	for _, operationName := range manifest.Spec.Languages.Python.Operations {
		for _, operation := range manifest.Spec.Operations {
			if operation.Name == operationName {
				rootNames = append(rootNames, operation.InputType, operation.OutputType)
				break
			}
		}
	}
	publishedNames, err := publishedPythonModelNames(filepath.Join(packageDirectory, "_generated", "models.py"))
	if err != nil {
		return nil, err
	}
	models, err := gomodel.Load(filepath.Join(filepath.Dir(manifestPath), "go"), rootNames, publishedNames...)
	if err != nil {
		return nil, err
	}
	publicModels, err := os.ReadFile(filepath.Join(packageDirectory, "models.py"))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read Python model facade: %w", err)
	}
	modelAliases := make(map[string]string)
	for _, match := range pythonModelAliasPattern.FindAllStringSubmatch(string(publicModels), -1) {
		modelAliases[match[1]] = match[2]
	}
	mockFacades, err := filepath.Glob(filepath.Join(packageDirectory, "*mock", "__init__.py"))
	if err != nil {
		return nil, fmt.Errorf("find Python mock facade: %w", err)
	}
	if len(mockFacades) > 1 {
		return nil, fmt.Errorf("multiple Python mock facades in %s", packageDirectory)
	}
	mockPackageName := manifest.Spec.Codegen.Go.Package + "mock"
	if len(mockFacades) == 1 {
		mockPackageName = filepath.Base(filepath.Dir(mockFacades[0]))
	}
	generated, err := renderPythonCodegen(pythonCodegenInput{Manifest: manifest, Models: models.Models, PackageName: packageName, MockPackageName: mockPackageName, ModelAliases: modelAliases})
	if err != nil {
		return nil, err
	}
	manualFiles := map[string]string{
		"connection.py": filepath.Join(packageDirectory, "connection.py"),
		"steps.py":      filepath.Join(packageDirectory, "steps.py"),
	}
	if len(mockFacades) == 1 {
		manualFiles["mock.py"] = mockFacades[0]
	}
	for name, source := range generated {
		if facadePath, exists := manualFiles[name]; exists {
			isManual, err := isHandwrittenPythonFacade(facadePath)
			if err != nil {
				return nil, err
			}
			if isManual {
				continue
			}
		}
		files[filepath.Join(packageDirectory, "_generated", name)] = []byte(source)
	}
	if len(mockFacades) == 0 {
		if _, hasMock := generated["mock.py"]; hasMock {
			files[filepath.Join(packageDirectory, mockPackageName, "__init__.py")] = []byte(
				"# Copyright (c) 2026 Super Durable\n# SPDX-License-Identifier: MIT\n" +
					"# ruff: noqa: F403\n\n" +
					"from .._generated.mock import *\nfrom .._generated.mock import __all__ as __all__\n",
			)
		}
	}
	return files, nil
}

func publishedPythonModelNames(path string) ([]string, error) {
	contents, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read existing Python models: %w", err)
	}
	var names []string
	for _, match := range pythonPublishedModelPattern.FindAllSubmatch(contents, -1) {
		names = append(names, string(match[1]))
	}
	return names, nil
}

func renderPythonCodegen(input pythonCodegenInput) (map[string]string, error) {
	input.SharedRequiredFields = make(map[string][]string)
	for _, method := range input.Manifest.Spec.Auth.AuthMethods() {
		input.SharedRequiredFields[method.ID] = codegen.SharedRequiredCredentialFields(method)
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("encode Python generation input: %w", err)
	}
	command := exec.Command("python3", "-c", pythonCodegenSource)
	command.Stdin = bytes.NewReader(payload)
	var output bytes.Buffer
	var diagnostics bytes.Buffer
	command.Stdout = &output
	command.Stderr = &diagnostics
	if err := command.Run(); err != nil {
		return nil, fmt.Errorf("generate Python connector: %w: %s", err, diagnostics.String())
	}
	var generated map[string]string
	if err := json.Unmarshal(output.Bytes(), &generated); err != nil {
		return nil, fmt.Errorf("decode Python generation output: %w", err)
	}
	return generated, nil
}

func isHandwrittenPythonFacade(path string) (bool, error) {
	source, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read Python facade %s: %w", path, err)
	}
	return bytes.Contains(source, []byte("# connectorctl: handwritten facade")), nil
}

func stalePythonFiles(manifestPath string, generated map[string][]byte) ([]string, error) {
	if len(generated) == 0 {
		return nil, nil
	}
	connectorDirectory, err := connectorDirectoryInCatalog(manifestPath)
	if err != nil {
		return nil, err
	}
	packageDirectory := filepath.Join(filepath.Dir(manifestPath), "python", "src", "dex_connectors", pythonImportPackageName(connectorDirectory), "_generated")
	entries, err := os.ReadDir(packageDirectory)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read generated Python module: %w", err)
	}
	var stale []string
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".py" {
			continue
		}
		path := filepath.Join(packageDirectory, entry.Name())
		if _, expected := generated[path]; !expected {
			stale = append(stale, path)
		}
	}
	return stale, nil
}
