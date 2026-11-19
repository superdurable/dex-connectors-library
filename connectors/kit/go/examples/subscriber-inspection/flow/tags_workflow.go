// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package subscriberinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/kit/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// TagsFlowType is the tag-directory example Flow identity.
const TagsFlowType = "KitTagsInspection"

const listTagsStepType = "CallListTags"

var tagsPageAttribute = dex.DefineAttribute[kit.TagsPage]("kit-tags-page")

// TagsFlow lists one page of Kit tags.
type TagsFlow struct {
	dex.FlowDefaults
	connection kit.Connection
}

// NewTagsFlow binds the Kit connection at registration time.
func NewTagsFlow(connection kit.Connection) *TagsFlow { return &TagsFlow{connection: connection} }

func (*TagsFlow) GetFlowType() string { return TagsFlowType }

func (*TagsFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{tagsPageAttribute}}
}

func (flow *TagsFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startTags{}),
		dex.DefineStep(kit.NewListTagsStep(kit.ListTagsStepConfig[kit.ListTagsInput]{
			StepType: listTagsStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Kit", Explanation: "List one page of tags."},
			Connection:  flow.connection, MapToOperationInput: MapToListTagsInput,
			Listed: sdkgo.GoTo(completeTags{}),
		})),
		dex.DefineStep(completeTags{}),
	}
}

func (flow *TagsFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:kit-tags-page value-type:json editable:false description:"Tag page"
// GetDexSummary shows the tag page in Dex Web.
func (*TagsFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readTagsPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"kit-tags-page": page}}, nil
}

// dex:field attribute-key:kit-tags-page value-type:json editable:false description:"Tag page"
// GetDexDisplay shows the tag page in Dex Web.
func (*TagsFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readTagsPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"kit-tags-page": page}}, nil
}

func readTagsPage(ctx dex.Context) (kit.TagsPage, error) {
	page, err := tagsPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return kit.TagsPage{}, nil
	}
	return page, err
}

// MapToListTagsInput passes the typed page request to Kit.
func MapToListTagsInput(input kit.ListTagsInput) kit.ListTagsInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the requested tag page to the provider Step."
type startTags struct{ dex.StepDefaults }

func (startTags) GetStepType() string { return "StartListTags" }
func (startTags) WaitFor(dex.Context, kit.ListTagsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startTags) Execute(_ dex.Context, input kit.ListTagsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[kit.ListTagsInput](listTagsStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with one typed tag page."
type completeTags struct {
	dex.StepDefaultsNoWaitFor[kit.ListTagsResult]
}

func (completeTags) GetStepType() string { return "CompleteListTags" }
func (completeTags) Execute(ctx dex.Context, result kit.ListTagsResult) (*dex.StepDecision, error) {
	if err := tagsPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*TagsFlow)(nil)
