// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sdkgo_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestQueryDefinitionRejectsInvalidBranches(t *testing.T) {
	valid := queryDefinition(testQueryRef)
	for _, test := range []struct {
		name   string
		change func(*sdkgo.QueryDefinition)
		want   string
	}{
		{name: "empty", change: func(definition *sdkgo.QueryDefinition) { definition.Branches = nil }, want: "at least one"},
		{name: "duplicate", change: func(definition *sdkgo.QueryDefinition) {
			definition.Branches = append(definition.Branches, definition.Branches[0])
		}, want: "duplicated"},
		{name: "invalid", change: func(definition *sdkgo.QueryDefinition) { definition.Branches[0].ID = "Bad" }, want: "lower camel"},
		{name: "missing description", change: func(definition *sdkgo.QueryDefinition) { definition.Branches[0].Description = "" }, want: "description"},
		{name: "missing defect", change: func(definition *sdkgo.QueryDefinition) {
			definition.Branches[len(definition.Branches)-1].ID = "broken"
		}, want: "defect branch"},
	} {
		t.Run(test.name, func(t *testing.T) {
			definition := valid
			definition.Branches = append([]sdkgo.BranchDefinition(nil), valid.Branches...)
			test.change(&definition)
			require.ErrorContains(t, definition.Validate(), test.want)
		})
	}
}

func TestMutationDefinitionAllowsOmittedUncertainBranch(t *testing.T) {
	definition := mutationDefinition(testMutationRef)
	definition.Branches = definition.Branches[:len(definition.Branches)-2]
	definition.Branches = append(definition.Branches, sdkgo.BranchDefinition{ID: sdkgo.DefectBranchID, Description: "defect"})
	require.NoError(t, definition.Validate())
}
