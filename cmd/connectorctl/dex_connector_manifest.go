// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/superdurable/dex-connectors-library/internal/codegen/gomodel"
	"github.com/superdurable/dex-connectors-library/schema"
)

type dexConnectorPackageManifest struct {
	ConnectorID string                      `json:"connectorId"`
	ReleaseTag  string                      `json:"releaseTag"`
	Package     dexConnectorPackageIdentity `json:"package"`
	Factories   []dexConnectorFactory       `json:"factories"`
}

type dexConnectorPackageIdentity struct {
	Ecosystem string `json:"ecosystem"`
	Name      string `json:"name"`
	Version   string `json:"version"`
}

type dexConnectorFactory struct {
	Name        string               `json:"name"`
	Kind        string               `json:"kind"`
	OperationID string               `json:"operationId,omitempty"`
	TriggerName string               `json:"triggerName,omitempty"`
	InputType   string               `json:"inputType,omitempty"`
	Description string               `json:"description,omitempty"`
	Agent       *dexConnectorAgent   `json:"agent,omitempty"`
	Fields      dexConnectorFields   `json:"fields"`
	Branches    []dexConnectorBranch `json:"branches,omitempty"`
	InputSchema map[string]any       `json:"inputSchema,omitempty"`
}

type dexConnectorAgent struct {
	Exposed  bool              `json:"exposed"`
	Approval string            `json:"approval,omitempty"`
	Retry    string            `json:"retry,omitempty"`
	Branches map[string]string `json:"branches,omitempty"`
}

type dexConnectorFields struct {
	StepType        string `json:"stepType,omitempty"`
	Annotations     string `json:"annotations,omitempty"`
	ConnectionName  string `json:"connectionName"`
	BindingName     string `json:"bindingName,omitempty"`
	ConfigurationUI string `json:"configurationUI,omitempty"`
	ResultAttribute string `json:"resultAttribute,omitempty"`
}

type dexConnectorBranch struct {
	ID       string `json:"id"`
	Field    string `json:"field"`
	Optional bool   `json:"optional"`
}

func generatedLanguageManifestFiles(manifestPath string, manifest schema.Manifest, goPackage *connectorGoPackage) (map[string][]byte, error) {
	files := make(map[string][]byte)
	if manifest.Spec.Languages == nil || (manifest.Spec.Languages.Python == nil && manifest.Spec.Languages.TypeScript == nil) {
		return files, nil
	}
	connectorDirectory, err := connectorDirectoryInCatalog(manifestPath)
	if err != nil {
		return nil, err
	}
	inputModels, err := languageOperationInputModels(manifest, goPackage)
	if err != nil {
		return nil, err
	}
	for _, language := range []struct {
		name               string
		packageDeclaration *schema.LanguagePackage
	}{
		{name: "python", packageDeclaration: manifest.Spec.Languages.Python},
		{name: "typescript", packageDeclaration: manifest.Spec.Languages.TypeScript},
	} {
		if language.packageDeclaration == nil {
			continue
		}
		packageManifest := dexConnectorPackageManifest{
			ConnectorID: manifest.Metadata.Name,
			ReleaseTag:  connectorReleaseTag(connectorDirectory, manifest.Metadata.Version),
			Package: dexConnectorPackageIdentity{
				Ecosystem: map[string]string{"python": "pypi", "typescript": "npm"}[language.name],
				Name:      languagePackageName(connectorDirectory, language.name, language.packageDeclaration.Package),
				Version:   strings.TrimPrefix(language.packageDeclaration.Version, "v"),
			},
			Factories: make([]dexConnectorFactory, 0, len(language.packageDeclaration.Operations)+len(language.packageDeclaration.Triggers)),
		}
		selectedOperations := make(map[string]bool, len(language.packageDeclaration.Operations))
		for _, name := range language.packageDeclaration.Operations {
			selectedOperations[name] = true
		}
		for _, operation := range manifest.Spec.Operations {
			if !selectedOperations[operation.Name] {
				continue
			}
			fields := dexConnectorFields{ConnectionName: "connectionName", ResultAttribute: "resultAttribute"}
			if language.name == "python" {
				fields.StepType = "step_type"
				fields.Annotations = "annotations"
				fields.ConnectionName = "connection_name"
				fields.ResultAttribute = "result_attribute"
			} else {
				fields.StepType = "stepType"
				fields.Annotations = "annotations"
			}
			factory := dexConnectorFactory{
				Kind: operation.Kind, OperationID: operation.Name, Description: modelFacingOperationDescription(operation),
				Fields: fields, Branches: make([]dexConnectorBranch, 0, len(operation.Branches)),
			}
			if inputModels != nil {
				if factory.InputSchema, err = inputModels.JSONSchema(operation.InputType); err != nil {
					return nil, fmt.Errorf("%s %s input schema: %w", language.name, operation.Name, err)
				}
				factory.InputType = operation.InputType
				factory.Agent = dexConnectorOperationAgent(operation)
			}
			if language.name == "python" {
				factory.Name = pythonStepFactoryName(operation.Name)
			} else {
				factory.Name = "new" + operation.GoName + "Step"
			}
			mappedFields := map[string]bool{
				fields.StepType: true, fields.Annotations: true,
				fields.ConnectionName: true, fields.ResultAttribute: true,
			}
			for _, branch := range operation.Branches {
				field := branch.ID
				if language.name == "python" {
					field = lowerCamelToSnake(branch.ID)
				}
				if mappedFields[field] {
					return nil, fmt.Errorf("%s %s factory maps more than one field to %q", language.name, operation.Name, field)
				}
				mappedFields[field] = true
				factory.Branches = append(factory.Branches, dexConnectorBranch{ID: branch.ID, Field: field, Optional: branch.Optional})
			}
			packageManifest.Factories = append(packageManifest.Factories, factory)
		}
		packageManifest.Factories = append(packageManifest.Factories, typeScriptTriggerBindingFactories(manifest, language.packageDeclaration.Triggers)...)
		contents, err := encodeLanguageManifest(packageManifest)
		if err != nil {
			return nil, fmt.Errorf("encode %s connector package manifest: %w", language.name, err)
		}
		path := filepath.Join(filepath.Dir(manifestPath), language.name)
		if language.name == "python" {
			importPackage := pythonImportPackageName(connectorDirectory)
			path = filepath.Join(path, "src", "dex_connectors", importPackage)
		}
		files[filepath.Join(path, "dex-connector.json")] = contents
	}
	return files, nil
}

// Python and TypeScript share one graph, so both packages publish the same schema for an operation.
// It returns nil for a fixture without authored Go models; the catalog test requires every schema.
func languageOperationInputModels(manifest schema.Manifest, goPackage *connectorGoPackage) (*gomodel.Graph, error) {
	hasAuthoredGoTypes, err := hasAuthoredGoSource(goPackage.goDirectory)
	if err != nil || !hasAuthoredGoTypes {
		return nil, err
	}
	return loadOperationInputModels(goPackage, manifest, languageSelectedOperations(manifest))
}

// languageSelectedOperations names the operations that any language package implements.
func languageSelectedOperations(manifest schema.Manifest) map[string]bool {
	selectedOperations := make(map[string]bool)
	if manifest.Spec.Languages == nil {
		return selectedOperations
	}
	for _, language := range []*schema.LanguagePackage{manifest.Spec.Languages.Python, manifest.Spec.Languages.TypeScript} {
		if language == nil {
			continue
		}
		for _, name := range language.Operations {
			selectedOperations[name] = true
		}
	}
	return selectedOperations
}

// loadOperationInputModels roots the graph at selected inputs in manifest order; the root set decides model names.
func loadOperationInputModels(goPackage *connectorGoPackage, manifest schema.Manifest, selectedOperations map[string]bool) (*gomodel.Graph, error) {
	var inputTypes []string
	for _, operation := range manifest.Spec.Operations {
		if selectedOperations[operation.Name] {
			inputTypes = append(inputTypes, operation.InputType)
		}
	}
	graph, err := goPackage.graph(inputTypes)
	if err != nil {
		return nil, err
	}
	return &graph, nil
}

func modelFacingOperationDescription(operation schema.Operation) string {
	if operation.Agent != nil && operation.Agent.Description != "" {
		return strings.TrimSpace(operation.Agent.Description)
	}
	return strings.TrimSpace(operation.Description)
}

func dexConnectorOperationAgent(operation schema.Operation) *dexConnectorAgent {
	if operation.Agent == nil {
		return nil
	}
	return &dexConnectorAgent{Exposed: *operation.Agent.Exposed, Approval: operation.Agent.Approval, Retry: operation.Agent.Retry, Branches: operation.Agent.Branches}
}

// Descriptions are prose for a model, so keep comparison operators readable in the file.
func encodeLanguageManifest(packageManifest dexConnectorPackageManifest) ([]byte, error) {
	var contents bytes.Buffer
	encoder := json.NewEncoder(&contents)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(packageManifest); err != nil {
		return nil, err
	}
	return contents.Bytes(), nil
}

func pythonImportPackageName(connectorDirectory string) string {
	return strings.NewReplacer("/", "_", "-", "_").Replace(strings.TrimPrefix(connectorDirectory, "connectors/"))
}

func staleLanguageManifestFiles(manifestPath string, generated map[string][]byte) ([]string, error) {
	connectorDirectory := filepath.Dir(manifestPath)
	pythonFiles, err := filepath.Glob(filepath.Join(connectorDirectory, "python", "src", "dex_connectors", "*", "dex-connector.json"))
	if err != nil {
		return nil, fmt.Errorf("find Python connector package manifests: %w", err)
	}
	paths := append(pythonFiles, filepath.Join(connectorDirectory, "typescript", "dex-connector.json"))
	stale := make([]string, 0)
	for _, path := range paths {
		if _, isGenerated := generated[path]; isGenerated {
			continue
		}
		if _, err := os.Lstat(path); err == nil {
			stale = append(stale, path)
		} else if !os.IsNotExist(err) {
			return nil, fmt.Errorf("inspect %s: %w", path, err)
		}
	}
	return stale, nil
}

// pythonStepFactoryName is the Python Step factory of an operation; Go agent tool names share its snake case.
func pythonStepFactoryName(operationID string) string {
	return "new_" + lowerCamelToSnake(operationID) + "_step"
}

func connectorReleaseTag(connectorDirectory, version string) string {
	return connectorDirectory + "/go/" + version
}

func lowerCamelToSnake(name string) string {
	var converted strings.Builder
	characters := []rune(name)
	for index, character := range characters {
		if character >= 'A' && character <= 'Z' {
			isPreviousLowerOrDigit := index > 0 && ((characters[index-1] >= 'a' && characters[index-1] <= 'z') || (characters[index-1] >= '0' && characters[index-1] <= '9'))
			isEndOfAcronym := index > 0 && index+1 < len(characters) && characters[index-1] >= 'A' && characters[index-1] <= 'Z' && characters[index+1] >= 'a' && characters[index+1] <= 'z'
			// A trailing lowercase s pluralizes the acronym, as in listStoryIDs.
			isAcronymPlural := index+1 < len(characters) && characters[index+1] == 's' && (index+2 == len(characters) || characters[index+2] < 'a' || characters[index+2] > 'z')
			isEndOfAcronym = isEndOfAcronym && !isAcronymPlural
			if isPreviousLowerOrDigit || isEndOfAcronym {
				converted.WriteByte('_')
			}
			converted.WriteRune(character + ('a' - 'A'))
		} else {
			converted.WriteRune(character)
		}
	}
	return converted.String()
}

// typeScriptTriggerBindingFactories describes the static Trigger binding factory of each declared Trigger, which
// Dex Web reads to configure a binding, as Go's DefineXTriggerBinding. Manifest validation keeps Python Trigger
// lists empty, so only TypeScript packages declare them.
func typeScriptTriggerBindingFactories(manifest schema.Manifest, declared []string) []dexConnectorFactory {
	selected := make(map[string]bool, len(declared))
	for _, name := range declared {
		selected[name] = true
	}
	factories := make([]dexConnectorFactory, 0, len(declared))
	for _, trigger := range manifest.Spec.Triggers {
		if !selected[trigger.Name] {
			continue
		}
		factories = append(factories, dexConnectorFactory{
			Name: "define" + trigger.GoName + "TriggerBinding", Kind: "trigger", TriggerName: trigger.Name,
			Fields: dexConnectorFields{ConnectionName: "connectionName", BindingName: "bindingName", ConfigurationUI: "configurationUI"},
		})
	}
	return factories
}
