// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/superdurable/dex-connectors-library/internal/codegen"
	"github.com/superdurable/dex-connectors-library/schema"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"
)

const connectorSDKModulePath = "github.com/superdurable/dex-connectors-library/sdkgo"

// generatedAgentToolFiles returns the Go agent tools file of a connector that has adopted agent tools.
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

// staleAgentToolFiles lists an agent tools file that a connector which has not adopted agent tools must not keep.
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

// hasAdoptedGoAgentTools reports whether the Go module offers agent tools: the manifest exposes an
// operation and go/go.mod requires a Connector Go SDK that defines them. Until the module raises its
// SDK requirement, exposure reaches only the Python and TypeScript packages, and the generated code
// never references SDK API the module cannot compile against.
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

func requiredConnectorSDKVersion(goModulePath string) (string, error) {
	contents, err := os.ReadFile(goModulePath)
	if err != nil {
		return "", fmt.Errorf("read connector module %s: %w", goModulePath, err)
	}
	module, err := modfile.Parse(goModulePath, contents, nil)
	if err != nil {
		return "", fmt.Errorf("parse connector module %s: %w", goModulePath, err)
	}
	for _, requirement := range module.Require {
		if requirement.Mod.Path == connectorSDKModulePath {
			if !semver.IsValid(requirement.Mod.Version) {
				return "", fmt.Errorf("connector module %s requires %s at invalid version %q", goModulePath, connectorSDKModulePath, requirement.Mod.Version)
			}
			return requirement.Mod.Version, nil
		}
	}
	return "", fmt.Errorf("connector module %s exposes operations to agents but does not require %s", goModulePath, connectorSDKModulePath)
}

// agentToolOperations derives every exposed operation's agent surface from the manifest and the Go models.
// It does not depend on the SDK requirement, so parity tests cover connectors that have not adopted Go agent tools.
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
	// The language package roots come first, so an exposed operation they list gets their exact schema.
	selectedOperations := languageSelectedOperations(manifest)
	for _, operation := range exposedOperations {
		selectedOperations[operation.Name] = true
	}
	inputModels, err := loadOperationInputModels(goDirectory, manifest, selectedOperations)
	if err != nil {
		return nil, err
	}
	operations := make([]codegen.AgentToolOperation, 0, len(exposedOperations))
	for _, operation := range exposedOperations {
		inputSchema, err := inputModels.JSONSchema(operation.InputType)
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
