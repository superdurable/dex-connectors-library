// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package connector_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	connector "github.com/superdurable/dex-connectors-library/sdk/go"
)

func TestQueryDefinitionRejectsInvalidBranches(t *testing.T) {
	valid := queryDefinition(testQueryRef)
	for _, test := range []struct {
		name   string
		change func(*connector.QueryDefinition)
		want   string
	}{
		{name: "empty", change: func(definition *connector.QueryDefinition) { definition.Branches = nil }, want: "at least one"},
		{name: "duplicate", change: func(definition *connector.QueryDefinition) {
			definition.Branches = append(definition.Branches, definition.Branches[0])
		}, want: "duplicated"},
		{name: "invalid", change: func(definition *connector.QueryDefinition) { definition.Branches[0].ID = "Bad" }, want: "lower camel"},
		{name: "missing description", change: func(definition *connector.QueryDefinition) { definition.Branches[0].Description = "" }, want: "description"},
		{name: "missing defect", change: func(definition *connector.QueryDefinition) { definition.DefectBranch = "missing" }, want: "defect branch"},
	} {
		t.Run(test.name, func(t *testing.T) {
			definition := valid
			definition.Branches = append([]connector.BranchDefinition(nil), valid.Branches...)
			test.change(&definition)
			require.ErrorContains(t, definition.Validate(), test.want)
		})
	}
}

func TestMutationDefinitionRequiresDeclaredUncertainBranch(t *testing.T) {
	definition := mutationDefinition(testMutationRef)
	definition.UncertainBranch = "missing"
	require.ErrorContains(t, definition.Validate(), "uncertain branch")
}
