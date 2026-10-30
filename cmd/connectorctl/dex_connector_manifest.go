// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
	OperationID string               `json:"operationId"`
	Fields      dexConnectorFields   `json:"fields"`
	Branches    []dexConnectorBranch `json:"branches"`
}

type dexConnectorFields struct {
	StepType        string `json:"stepType"`
	Annotations     string `json:"annotations"`
	ConnectionName  string `json:"connectionName"`
	ResultAttribute string `json:"resultAttribute"`
}

type dexConnectorBranch struct {
	ID       string `json:"id"`
	Field    string `json:"field"`
	Optional bool   `json:"optional"`
}

func generatedLanguageManifestFiles(manifestPath string, manifest schema.Manifest) (map[string][]byte, error) {
	files := make(map[string][]byte)
	if manifest.Spec.Languages == nil || (manifest.Spec.Languages.Python == nil && manifest.Spec.Languages.TypeScript == nil) {
		return files, nil
	}
	connectorDirectory, err := connectorDirectoryInCatalog(manifestPath)
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
			Factories: make([]dexConnectorFactory, 0, len(language.packageDeclaration.Operations)),
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
				Kind: operation.Kind, OperationID: operation.Name, Fields: fields,
				Branches: make([]dexConnectorBranch, 0, len(operation.Branches)),
			}
			if language.name == "python" {
				factory.Name = "new_" + lowerCamelToSnake(operation.Name) + "_step"
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
		contents, err := json.MarshalIndent(packageManifest, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("encode %s connector package manifest: %w", language.name, err)
		}
		contents = append(contents, '\n')
		path := filepath.Join(filepath.Dir(manifestPath), language.name)
		if language.name == "python" {
			importPackage := pythonImportPackageName(connectorDirectory)
			path = filepath.Join(path, "src", "dex_connectors", importPackage)
		}
		files[filepath.Join(path, "dex-connector.json")] = contents
	}
	return files, nil
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
