// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"

	"github.com/superdurable/dex-connectors-library/internal/codegen/gomodel"
)

func conformanceOperationsCommand(manifestPath string, output io.Writer) error {
	manifest, err := load(manifestPath)
	if err != nil {
		return err
	}
	type conformanceOperation struct {
		Kind                string   `json:"kind"`
		Branches            []string `json:"branches"`
		OptionalInputFields []string `json:"optionalInputFields"`
	}
	operations := make(map[string]conformanceOperation, len(manifest.Spec.Operations))
	for _, operation := range manifest.Spec.Operations {
		entry := conformanceOperation{Kind: operation.Kind}
		entry.OptionalInputFields, err = conformanceOptionalInputFields(manifestPath, operation.InputType)
		if err != nil {
			return err
		}
		for _, branch := range operation.Branches {
			entry.Branches = append(entry.Branches, branch.ID)
		}
		operations[operation.Name] = entry
	}
	return json.NewEncoder(output).Encode(operations)
}

func conformanceOptionalInputFields(manifestPath, inputType string) ([]string, error) {
	graph, err := gomodel.Load(filepath.Join(filepath.Dir(manifestPath), "go"), []string{inputType})
	if err != nil {
		return nil, err
	}
	for _, model := range graph.Models {
		if model.Name != inputType {
			continue
		}
		optional := []string{}
		for _, field := range model.Fields {
			if field.Optional {
				optional = append(optional, field.Name)
			}
		}
		return optional, nil
	}
	return nil, fmt.Errorf("input type %s missing from Go model graph", inputType)
}
