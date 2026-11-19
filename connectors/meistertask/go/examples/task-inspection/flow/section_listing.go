// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package taskinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/meistertask/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const sectionListingStepType = "CallListSections"

var sectionListingAttribute = dex.DefineAttribute[meistertask.SectionsPage]("meistertask-section-list")

// SectionListingFlow reads one page of sections for a Dex application.
type SectionListingFlow struct {
	dex.FlowDefaults
	connection meistertask.Connection
}

// NewSectionListingFlow binds the same static MeisterTask connection used by section inspection.
func NewSectionListingFlow(connection meistertask.Connection) *SectionListingFlow {
	return &SectionListingFlow{connection: connection}
}

// GetFlowType identifies the section listing Flow.
func (*SectionListingFlow) GetFlowType() string { return "MeisterTaskSectionListing" }

// GetPersistenceSchema registers its durable provider result.
func (*SectionListingFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{sectionListingAttribute}}
}

// GetSteps wires the section listing Query through an operation-specific factory.
func (flow *SectionListingFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startSectionListing{}),
		dex.DefineStep(meistertask.NewListSectionsStep(meistertask.ListSectionsStepConfig[meistertask.ListSectionsInput]{
			StepType: sectionListingStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "MeisterTask", Explanation: "List sections visible to the connection."},
			Connection:  flow.connection, MapToOperationInput: listSectionsInput,
			Listed: sdkgo.GoTo(completeSectionListing{}),
		})),
		dex.DefineStep(completeSectionListing{}),
	}
}

// GetRPCs exposes the section page in Dex Web.
func (flow *SectionListingFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:meistertask-section-list value-type:json editable:false description:"Section page"
// GetDexSummary returns the last section page.
func (*SectionListingFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	sections, err := readSectionListing(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"meistertask-section-list": sections}}, nil
}

// dex:field attribute-key:meistertask-section-list value-type:json editable:false description:"Section page"
// GetDexDisplay returns the last section page.
func (*SectionListingFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	sections, err := readSectionListing(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"meistertask-section-list": sections}}, nil
}

func readSectionListing(ctx dex.Context) (meistertask.SectionsPage, error) {
	sections, err := sectionListingAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return meistertask.SectionsPage{}, nil
	}
	return sections, err
}

func listSectionsInput(input meistertask.ListSectionsInput) meistertask.ListSectionsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass page filters to the MeisterTask list operation."
type startSectionListing struct{ dex.StepDefaults }

func (startSectionListing) GetStepType() string { return "StartSectionListing" }
func (startSectionListing) WaitFor(dex.Context, meistertask.ListSectionsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startSectionListing) Execute(_ dex.Context, input meistertask.ListSectionsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[meistertask.ListSectionsInput](sectionListingStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Save the returned section page and complete the Flow."
type completeSectionListing struct {
	dex.StepDefaultsNoWaitFor[meistertask.ListSectionsResult]
}

func (completeSectionListing) GetStepType() string { return "CompleteSectionListing" }
func (completeSectionListing) Execute(ctx dex.Context, result meistertask.ListSectionsResult) (*dex.StepDecision, error) {
	if err := sectionListingAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*SectionListingFlow)(nil)
