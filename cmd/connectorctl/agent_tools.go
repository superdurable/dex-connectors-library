// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"

	"github.com/superdurable/dex-connectors-library/internal/codegen"
	"github.com/superdurable/dex-connectors-library/internal/codegen/gomodel"
	"github.com/superdurable/dex-connectors-library/schema"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"
)

const connectorSDKModulePath = "github.com/superdurable/dex-connectors-library/sdkgo"

// generatedAgentToolFiles returns the agent tools file of a Go module that has adopted them.
func generatedAgentToolFiles(manifestPath string, manifest schema.Manifest) (map[string][]byte, error) {
	files := make(map[string][]byte)
	hasAdopted, err := hasAdoptedGoAgentTools(manifestPath, manifest)
	if err != nil || !hasAdopted {
		return files, err
	}
	operations, err := agentToolOperations(manifestPath, manifest)
	if err != nil {
		return nil, err
	}
	source, err := codegen.GenerateAgentTools(manifest, operations)
	if err != nil {
		return nil, err
	}
	files[agentToolsOutputPath(manifestPath)] = source
	return files, nil
}

// staleAgentToolFiles lists an agent tools file left in a Go module that has not adopted them.
func staleAgentToolFiles(manifestPath string, manifest schema.Manifest) ([]string, error) {
	hasAdopted, err := hasAdoptedGoAgentTools(manifestPath, manifest)
	if err != nil || hasAdopted {
		return nil, err
	}
	path := agentToolsOutputPath(manifestPath)
	if _, err := os.Lstat(path); err == nil {
		return []string{path}, nil
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("inspect %s: %w", path, err)
	}
	return nil, nil
}

// hasAdoptedGoAgentTools requires an exposed operation and a go.mod SDK release that defines agent tools.
func hasAdoptedGoAgentTools(manifestPath string, manifest schema.Manifest) (bool, error) {
	if len(codegen.ExposedAgentOperations(manifest)) == 0 {
		return false, nil
	}
	goModulePath := filepath.Join(filepath.Dir(manifestPath), "go", "go.mod")
	sdkVersion, err := requiredConnectorSDKVersion(goModulePath)
	if err != nil {
		return false, err
	}
	return semver.Compare(sdkVersion, codegen.AgentToolsMinimumSDKVersion) >= 0, nil
}

// requiredConnectorSDKVersion rejects a replace or non-release version, which the gate cannot trust to compile.
func requiredConnectorSDKVersion(goModulePath string) (string, error) {
	contents, err := os.ReadFile(goModulePath)
	if err != nil {
		return "", fmt.Errorf("read connector module %s: %w", goModulePath, err)
	}
	module, err := modfile.Parse(goModulePath, contents, nil)
	if err != nil {
		return "", fmt.Errorf("parse connector module %s: %w", goModulePath, err)
	}
	for _, replacement := range module.Replace {
		if replacement.Old.Path == connectorSDKModulePath {
			return "", fmt.Errorf("connector module %s replaces %s; require a released version instead", goModulePath, connectorSDKModulePath)
		}
	}
	for _, requirement := range module.Require {
		if requirement.Mod.Path != connectorSDKModulePath {
			continue
		}
		version := requirement.Mod.Version
		if !semver.IsValid(version) || semver.Prerelease(version) != "" || semver.Build(version) != "" {
			return "", fmt.Errorf("connector module %s requires %s at %q, which is not a released version", goModulePath, connectorSDKModulePath, version)
		}
		return version, nil
	}
	return "", fmt.Errorf("connector module %s exposes operations to agents but does not require %s", goModulePath, connectorSDKModulePath)
}

// agentToolOperations ignores the SDK requirement, so parity tests also cover modules that have not adopted.
func agentToolOperations(manifestPath string, manifest schema.Manifest) ([]codegen.AgentToolOperation, error) {
	exposedOperations := codegen.ExposedAgentOperations(manifest)
	if len(exposedOperations) == 0 {
		return nil, nil
	}
	goDirectory := filepath.Join(filepath.Dir(manifestPath), "go")
	hasAuthoredGoTypes, err := hasAuthoredGoSource(goDirectory)
	if err != nil {
		return nil, err
	}
	if !hasAuthoredGoTypes {
		return nil, fmt.Errorf("agent tools require authored Go operation types in %s", goDirectory)
	}
	schemas := agentToolInputSchemas{goDirectory: goDirectory, manifest: manifest, languageOperations: languageSelectedOperations(manifest)}
	operations := make([]codegen.AgentToolOperation, 0, len(exposedOperations))
	for _, operation := range exposedOperations {
		inputSchema, err := schemas.inputSchema(operation)
		if err != nil {
			return nil, fmt.Errorf("agent tool %s input schema: %w", operation.Name, err)
		}
		encodedSchema, err := encodeCompactJSON(inputSchema)
		if err != nil {
			return nil, fmt.Errorf("encode agent tool %s input schema: %w", operation.Name, err)
		}
		operations = append(operations, codegen.AgentToolOperation{
			Operation:   operation,
			ToolName:    agentToolName(manifest, operation),
			Description: modelFacingOperationDescription(operation),
			InputSchema: encodedSchema,
		})
	}
	return operations, nil
}

// agentToolInputSchemas loads graphs whose roots never differ from the language packages' roots.
type agentToolInputSchemas struct {
	goDirectory        string
	manifest           schema.Manifest
	languageOperations map[string]bool
	languageModels     *gomodel.Graph
}

// inputSchema reuses the language graph for listed operations, because an extra root can rename shared models.
func (schemas *agentToolInputSchemas) inputSchema(operation schema.Operation) (map[string]any, error) {
	if schemas.languageOperations[operation.Name] {
		if schemas.languageModels == nil {
			languageModels, err := loadOperationInputModels(schemas.goDirectory, schemas.manifest, schemas.languageOperations)
			if err != nil {
				return nil, err
			}
			schemas.languageModels = languageModels
		}
		return schemas.languageModels.JSONSchema(operation.InputType)
	}
	roots := maps.Clone(schemas.languageOperations)
	roots[operation.Name] = true
	models, err := loadOperationInputModels(schemas.goDirectory, schemas.manifest, roots)
	if err != nil {
		return nil, err
	}
	return models.JSONSchema(operation.InputType)
}

// agentToolName uses the snake case of pythonStepFactoryName, which sdkgo also requires.
func agentToolName(manifest schema.Manifest, operation schema.Operation) string {
	return manifest.Metadata.Name + "_" + lowerCamelToSnake(operation.Name)
}

// encodeCompactJSON keeps characters such as < readable, as the language package manifests do.
func encodeCompactJSON(value any) (json.RawMessage, error) {
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(encoded.Bytes(), []byte("\n")), nil
}

func agentToolsOutputPath(manifestPath string) string {
	return filepath.Join(filepath.Dir(manifestPath), "go", codegen.AgentToolsOutputFile)
}
