// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contactread

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/keap/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// TagFlowType identifies the applied tag listing example Flow.
const TagFlowType = "KeapContactTags"

const listContactTagsStepType = "CallListContactTags"

var contactTagsPageAttribute = dex.DefineAttribute[keap.ContactTagsPage]("keap-contact-tags-page")

// TagFlow lists a contact's applied tags.
type TagFlow struct {
	dex.FlowDefaults
	connection keap.Connection
}

// NewTagFlow binds the static Keap connection at registration.
func NewTagFlow(connection keap.Connection) *TagFlow { return &TagFlow{connection: connection} }

func (*TagFlow) GetFlowType() string { return TagFlowType }

func (*TagFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{contactTagsPageAttribute}}
}

func (flow *TagFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startTagList{}),
		dex.DefineStep(keap.NewListContactTagsStep(keap.ListContactTagsStepConfig[keap.ListContactTagsInput]{
			StepType: listContactTagsStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Keap", Explanation: "List one contact's applied tags."},
			Connection:  flow.connection, MapToOperationInput: mapContactTagListInput,
			Listed: sdkgo.GoTo(completeTagList{}),
		})),
		dex.DefineStep(completeTagList{}),
	}
}

func (flow *TagFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:keap-contact-tags-page value-type:json editable:false description:"Applied tags"
// GetDexSummary shows the tag page in Dex Web.
func (*TagFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readContactTagsPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"keap-contact-tags-page": page}}, nil
}

// dex:field attribute-key:keap-contact-tags-page value-type:json editable:false description:"Applied tags"
// GetDexDisplay shows the tag page in Dex Web.
func (*TagFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readContactTagsPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"keap-contact-tags-page": page}}, nil
}

func readContactTagsPage(ctx dex.Context) (keap.ContactTagsPage, error) {
	page, err := contactTagsPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return keap.ContactTagsPage{}, nil
	}
	return page, err
}

func mapContactTagListInput(input keap.ListContactTagsInput) keap.ListContactTagsInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the selected contact and page to the provider Step."
type startTagList struct{ dex.StepDefaults }

func (startTagList) GetStepType() string { return "StartContactTags" }
func (startTagList) WaitFor(dex.Context, keap.ListContactTagsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startTagList) Execute(_ dex.Context, input keap.ListContactTagsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[keap.ListContactTagsInput](listContactTagsStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Store the returned tag page and complete the Flow."
type completeTagList struct {
	dex.StepDefaultsNoWaitFor[keap.ListContactTagsResult]
}

func (completeTagList) GetStepType() string { return "CompleteContactTags" }
func (completeTagList) Execute(ctx dex.Context, result keap.ListContactTagsResult) (*dex.StepDecision, error) {
	if err := contactTagsPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*TagFlow)(nil)
