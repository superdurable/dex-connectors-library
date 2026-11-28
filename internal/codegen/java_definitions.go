// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package codegen

import (
	"encoding/json"
	"slices"
	"time"

	"github.com/superdurable/dex-connectors-library/schema"
)

// GenerateJavaDefinitions encodes manifest operation definitions for the Java SDK's Jackson constructors.
func GenerateJavaDefinitions(manifest schema.Manifest, operationNames []string) ([]byte, error) {
	definitions := make(map[string]any, len(operationNames))
	for _, operation := range manifest.Spec.Operations {
		if !slices.Contains(operationNames, operation.Name) {
			continue
		}
		durations, err := parseOperationDurations(operation)
		if err != nil {
			return nil, err
		}
		retry := map[string]any{
			"InitialInterval": time.Second, "BackoffCoefficient": 2,
			"MaximumInterval": 5 * time.Minute, "MaximumAttempts": 0, "TotalDuration": time.Hour,
		}
		if policy := operation.Execution.Retry; policy != nil {
			retry = map[string]any{
				"InitialInterval": durations.retryInitialInterval, "BackoffCoefficient": policy.BackoffCoefficient,
				"MaximumInterval": durations.retryMaximumInterval, "MaximumAttempts": policy.MaximumAttempts, "TotalDuration": durations.retryTotalDuration,
			}
		}
		durability := 1
		if operation.Execution.Durability == "async" {
			durability = 2
		}
		branches := make([]map[string]any, 0, len(operation.Branches))
		for _, branch := range operation.Branches {
			branches = append(branches, map[string]any{"id": branch.ID, "description": branch.Description, "optional": branch.Optional})
		}
		definition := map[string]any{
			"operation":    map[string]string{"connectorId": manifest.Metadata.Name, "operationId": operation.Name},
			"branches":     branches,
			"stepDefaults": map[string]any{"executeDurability": durability, "executeMethodTimeout": durations.executeMethodTimeout, "heartbeatTimeout": durations.heartbeatTimeout, "executeRetry": retry},
		}
		if operation.Kind == "mutation" {
			definition["repeatOnUnknown"] = operation.ResolvedRepeatOnUnknown()
			definition["maxSends"] = operation.ResolvedMaxSends()
		}
		definitions[operation.Name] = definition
	}
	content, err := json.MarshalIndent(definitions, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(content, '\n'), nil
}
