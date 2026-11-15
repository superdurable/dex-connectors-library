// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// ConnectionName is the static connection the example's Trigger binding reads.
const ConnectionName = "changelog-main"

// itemChangedBindingDefinition is the generated-style static metadata of the example binding.
var itemChangedBindingDefinition = sdkgo.TriggerBindingDefinition{
	Definition: sdkgo.TriggerDefinition{
		Trigger:     sdkgo.TriggerRef{ConnectorID: "changelog", TriggerName: "itemChanged"},
		Description: "Receive every change appended to the change log after the binding first starts.",
	},
	ConnectionName: ConnectionName,
	BindingName:    "syncItem",
}

// itemSyncFlow handles one changed item; the poll Trigger starts one Flow per change.
type itemSyncFlow struct {
	dex.FlowDefaults
}

// GetSteps registers the single Step.
func (*itemSyncFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{dex.DefineStartStep(recordItemChange{})}
}

// GetPersistenceSchema declares no Attributes.
func (*itemSyncFlow) GetPersistenceSchema() dex.PersistenceSchema { return dex.PersistenceSchema{} }

// dex:group group-id:sync group-label:"Sync"
// dex:explanation text:"Complete with the changed item that the poll Trigger delivered."
type recordItemChange struct {
	dex.StepDefaultsNoWaitFor[ItemChanged]
}

// Execute completes the Flow with the delivered change.
func (recordItemChange) Execute(_ dex.Context, input ItemChanged) (*dex.StepDecision, error) {
	return dex.GracefulComplete(input), nil
}
