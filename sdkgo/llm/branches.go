// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llm

import (
	"fmt"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	// GeneratedBranchID is the required branch for a normal finish with text.
	GeneratedBranchID sdkgo.BranchID = "generated"
	// TruncatedBranchID is the optional branch for a finish at the output token limit.
	TruncatedBranchID sdkgo.BranchID = "truncated"
	// BlockedBranchID is the optional branch for a content-policy stop or a refusal.
	BlockedBranchID sdkgo.BranchID = "blocked"
	// ProviderRejectedBranchID is the optional branch for a conclusive provider rejection.
	ProviderRejectedBranchID sdkgo.BranchID = "providerRejected"
	// InvalidResponseBranchID is the optional branch for a malformed, oversized, or unusable response.
	InvalidResponseBranchID sdkgo.BranchID = "invalidResponse"
	// DefectBranchID is the optional standard branch for invalid local input or configuration.
	DefectBranchID sdkgo.BranchID = sdkgo.DefectBranchID
)

// TextGenerationBranchDefinitions returns the six generateText branches in
// manifest order with provider-neutral descriptions. Only generated is
// required; every other branch is optional, so leaving it unwired fails the
// Flow when it is selected.
//
// Connectors declare the same IDs and optionality in connector.yaml, usually
// with provider-specific descriptions; NewTextGenerationQuery rejects a
// definition with any other branch set. The result is a new slice on every call.
func TextGenerationBranchDefinitions() []sdkgo.BranchDefinition {
	return []sdkgo.BranchDefinition{
		{ID: GeneratedBranchID, Description: "The model finished normally and returned text."},
		{ID: TruncatedBranchID, Description: "The model stopped at the output token limit and returned any partial text.", Optional: true},
		{ID: BlockedBranchID, Description: "The provider stopped the response for a content policy, or the model refused.", Optional: true},
		{ID: ProviderRejectedBranchID, Description: "The provider conclusively rejected the request, such as invalid credentials, an unknown model, or exhausted quota.", Optional: true},
		{ID: InvalidResponseBranchID, Description: "The provider returned a malformed, oversized, or unusable response, including structured output that does not match its schema.", Optional: true},
		{ID: DefectBranchID, Description: "Local input, connection configuration, or connector definition is invalid.", Optional: true},
	}
}

// validateTextGenerationBranches requires exactly the generateText branch set and optionality.
func validateTextGenerationBranches(definition sdkgo.QueryDefinition) error {
	expected := TextGenerationBranchDefinitions()
	if len(definition.Branches) != len(expected) {
		return fmt.Errorf("generateText declares %d branches; it must declare exactly %d", len(definition.Branches), len(expected))
	}
	declared := make(map[sdkgo.BranchID]bool, len(definition.Branches))
	for _, branch := range definition.Branches {
		declared[branch.ID] = branch.Optional
	}
	for _, branch := range expected {
		isOptional, found := declared[branch.ID]
		if !found {
			return fmt.Errorf("generateText must declare the %q branch", branch.ID)
		}
		if isOptional != branch.Optional {
			return fmt.Errorf("generateText branch %q must have optional: %t", branch.ID, branch.Optional)
		}
	}
	return nil
}
