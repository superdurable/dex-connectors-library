// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package opportunityinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/lever/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// InterviewListFlow lists interview schedule metadata for one Lever opportunity.
type InterviewListFlow struct {
	dex.FlowDefaults
	connection lever.Connection
}

// NewInterviewListFlow binds the provider connection at registration time.
func NewInterviewListFlow(connection lever.Connection) *InterviewListFlow {
	return &InterviewListFlow{connection: connection}
}
func (*InterviewListFlow) GetFlowType() string { return "LeverInterviewList" }

var interviewsAttribute = dex.DefineAttribute[lever.InterviewsPage]("lever-interviews")

func (*InterviewListFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{interviewsAttribute}}
}
func (flow *InterviewListFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startInterviewList{}),
		dex.DefineStep(lever.NewListInterviewsStep(lever.ListInterviewsStepConfig[lever.ListInterviewsInput]{
			StepType: "CallListInterviews", ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Lever", Explanation: "List interviews for the selected opportunity."},
			Connection:  flow.connection, MapToOperationInput: mapInterviewListInput,
			Listed: sdkgo.GoTo(completeInterviewList{}),
		})),
		dex.DefineStep(completeInterviewList{}),
	}
}
func (flow *InterviewListFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:lever-interviews value-type:json editable:false description:"Interview schedule metadata"
// GetDexSummary shows the interview page in Dex Web.
func (*InterviewListFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readInterviews(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"lever-interviews": value}}, nil
}

// dex:field attribute-key:lever-interviews value-type:json editable:false description:"Interview schedule metadata"
// GetDexDisplay shows the interview page in Dex Web.
func (*InterviewListFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readInterviews(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"lever-interviews": value}}, nil
}
func readInterviews(ctx dex.Context) (lever.InterviewsPage, error) {
	value, err := interviewsAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return lever.InterviewsPage{}, nil
	}
	return value, err
}
func mapInterviewListInput(input lever.ListInterviewsInput) lever.ListInterviewsInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the opportunity ID to the provider Step."
type startInterviewList struct{ dex.StepDefaults }

func (startInterviewList) GetStepType() string { return "StartListInterviews" }
func (startInterviewList) WaitFor(dex.Context, lever.ListInterviewsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startInterviewList) Execute(_ dex.Context, input lever.ListInterviewsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[lever.ListInterviewsInput]("CallListInterviews"), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Store the interview schedule metadata and complete the Flow."
type completeInterviewList struct {
	dex.StepDefaultsNoWaitFor[lever.ListInterviewsResult]
}

func (completeInterviewList) GetStepType() string { return "CompleteListInterviews" }
func (completeInterviewList) Execute(ctx dex.Context, result lever.ListInterviewsResult) (*dex.StepDecision, error) {
	if err := interviewsAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*InterviewListFlow)(nil)
