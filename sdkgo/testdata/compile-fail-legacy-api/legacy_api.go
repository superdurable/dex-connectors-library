// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package legacyapi

import "github.com/superdurable/dex-connectors-library/sdkgo"

var _ sdkgo.TriggerEventFilter[string]
var _ sdkgo.FlowInputBuilder[string, string]
var _ sdkgo.Requirement
var _ sdkgo.PersistenceRequirements

var _ = sdkgo.QueryStepConfig[string, string, string]{
	BuildOperationInput: func(input string) (string, error) {
		return input, nil
	},
}
