// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sdkgo

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/superdurable/dex/sdk-go/dex"
)

var branchIDPattern = regexp.MustCompile(`^[a-z][A-Za-z0-9]+$`)

func (definition QueryDefinition) Validate() error {
	if err := definition.Operation.Validate(); err != nil {
		return err
	}
	branches, err := validateBranches(definition.Branches)
	if err != nil {
		return err
	}
	if !branches[FailedBranchID] {
		return fmt.Errorf("failed branch is not declared")
	}
	if branches[UncertainBranchID] {
		return fmt.Errorf("query cannot declare the uncertain branch")
	}
	return validateDefinitionOptions(definition.StepDefaults)
}

func (definition MutationDefinition) Validate() error {
	if err := definition.Operation.Validate(); err != nil {
		return err
	}
	branches, err := validateBranches(definition.Branches)
	if err != nil {
		return err
	}
	if !branches[FailedBranchID] {
		return fmt.Errorf("failed branch is not declared")
	}
	return validateDefinitionOptions(definition.StepDefaults)
}

func (definition QueryDefinition) hasBranch(branch BranchID) bool {
	return hasBranch(definition.Branches, branch)
}

func (definition MutationDefinition) hasBranch(branch BranchID) bool {
	return hasBranch(definition.Branches, branch)
}

func hasBranch(branches []BranchDefinition, branch BranchID) bool {
	for _, candidate := range branches {
		if candidate.ID == branch {
			return true
		}
	}
	return false
}

func validateBranches(definitions []BranchDefinition) (map[BranchID]bool, error) {
	if len(definitions) == 0 {
		return nil, fmt.Errorf("at least one branch is required")
	}
	branches := make(map[BranchID]bool, len(definitions))
	for _, definition := range definitions {
		if !branchIDPattern.MatchString(string(definition.ID)) {
			return nil, fmt.Errorf("branch ID %q must be lower camel case", definition.ID)
		}
		if branches[definition.ID] {
			return nil, fmt.Errorf("branch ID %q is duplicated", definition.ID)
		}
		if strings.TrimSpace(definition.Description) == "" {
			return nil, fmt.Errorf("branch %q description is required", definition.ID)
		}
		branches[definition.ID] = true
	}
	return branches, nil
}

func validateDefinitionOptions(defaults StepDefaults) error {
	if defaults.ExecuteMethodTimeout < 0 || defaults.HeartbeatTimeout < 0 {
		return fmt.Errorf("Step timeouts cannot be negative")
	}
	if defaults.ExecuteDurability != dex.StepDurabilityDefault && defaults.ExecuteDurability != dex.StepDurabilitySync && defaults.ExecuteDurability != dex.StepDurabilityAsync {
		return fmt.Errorf("Step Execute durability is invalid")
	}
	if defaults.ExecuteRetry != nil {
		retry := defaults.ExecuteRetry
		if retry.InitialInterval < 0 || retry.MaximumInterval < 0 || retry.TotalDuration < 0 || retry.MaximumAttempts < 0 || retry.BackoffCoefficient < 0 {
			return fmt.Errorf("Step retry fields cannot be negative")
		}
	}
	return nil
}
