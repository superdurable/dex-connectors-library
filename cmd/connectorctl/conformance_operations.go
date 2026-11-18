// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/superdurable/dex-connectors-library/schema"
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
		// IsSafeToRepeat marks a safe Mutation, whose declared uncertain and invalidResponse branches may be unreachable.
		IsSafeToRepeat bool `json:"isSafeToRepeat,omitempty"`
		// RetiredBranches lists declared branches the connector no longer selects; they need no replay coverage.
		RetiredBranches []string `json:"retiredBranches,omitempty"`
	}
	operations := make(map[string]conformanceOperation, len(manifest.Spec.Operations))
	goPackage := newConnectorGoPackage(manifestPath)
	for _, operation := range manifest.Spec.Operations {
		entry := conformanceOperation{Kind: operation.Kind}
		entry.IsSafeToRepeat = operation.Kind == "mutation" && operation.ResolvedRepeatOnUnknown() == schema.RepeatOnUnknownSafe
		entry.OptionalInputFields, err = conformanceOptionalInputFields(goPackage, operation.InputType)
		if err != nil {
			return err
		}
		for _, branch := range operation.Branches {
			entry.Branches = append(entry.Branches, branch.ID)
			if branch.Retired {
				entry.RetiredBranches = append(entry.RetiredBranches, branch.ID)
			}
		}
		operations[operation.Name] = entry
	}
	return json.NewEncoder(output).Encode(operations)
}

func conformanceOptionalInputFields(goPackage *connectorGoPackage, inputType string) ([]string, error) {
	graph, err := goPackage.graph([]string{inputType})
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
