// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package accounts

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/basecamp/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// PersonEnrollmentFlowType identifies the person enrollment example.
const PersonEnrollmentFlowType = "BasecampPersonEnrollment"

const personEnrollmentStepType = "CallEnrollPerson"

var enrolledPersonAttribute = dex.DefineAttribute[basecamp.EnrollmentResult]("basecamp-enrolled-person")

// PersonEnrollmentFlow enrolls or re-enrolls one Basecamp person.
type PersonEnrollmentFlow struct {
	dex.FlowDefaults
	connection basecamp.Connection
}

// NewPersonEnrollmentFlow binds the provider connection at registration.
func NewPersonEnrollmentFlow(connection basecamp.Connection) *PersonEnrollmentFlow {
	return &PersonEnrollmentFlow{connection: connection}
}

func (*PersonEnrollmentFlow) GetFlowType() string { return PersonEnrollmentFlowType }

func (*PersonEnrollmentFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{enrolledPersonAttribute}}
}

// GetRPCs registers read-only person views.
func (flow *PersonEnrollmentFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:basecamp-enrolled-person value-type:json editable:false description:"Enrolled person"
// GetDexSummary shows the enrolled person identity.
func (*PersonEnrollmentFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	enrollment, err := readEnrolledPerson(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"basecamp-enrolled-person": enrollment}}, nil
}

// dex:field attribute-key:basecamp-enrolled-person value-type:json editable:false description:"Enrolled person"
// GetDexDisplay shows the enrolled person identity.
func (*PersonEnrollmentFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	enrollment, err := readEnrolledPerson(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"basecamp-enrolled-person": enrollment}}, nil
}

func readEnrolledPerson(ctx dex.Context) (basecamp.EnrollmentResult, error) {
	enrollment, err := enrolledPersonAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return basecamp.EnrollmentResult{}, nil
	}
	return enrollment, err
}

func (flow *PersonEnrollmentFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startPersonEnrollment{}),
		dex.DefineStep(basecamp.NewEnrollPersonStep(basecamp.EnrollPersonStepConfig[basecamp.EnrollPersonInput]{
			StepType: personEnrollmentStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Basecamp", Explanation: "Enroll one person in the selected account."},
			Connection:          flow.connection,
			MapToOperationInput: func(input basecamp.EnrollPersonInput) basecamp.EnrollPersonInput { return input },
			Enrolled:            sdkgo.GoTo(completePersonEnrollment{}),
		})),
		dex.DefineStep(completePersonEnrollment{}),
	}
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the person fields to the creation Step."
type startPersonEnrollment struct{ dex.StepDefaults }

func (startPersonEnrollment) GetStepType() string { return "StartEnrollPerson" }
func (startPersonEnrollment) WaitFor(dex.Context, basecamp.EnrollPersonInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startPersonEnrollment) Execute(_ dex.Context, input basecamp.EnrollPersonInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[basecamp.EnrollPersonInput](personEnrollmentStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with the enrolled person identity."
type completePersonEnrollment struct {
	dex.StepDefaultsNoWaitFor[basecamp.EnrollPersonResult]
}

func (completePersonEnrollment) GetStepType() string { return "CompleteEnrollPerson" }
func (completePersonEnrollment) Execute(ctx dex.Context, result basecamp.EnrollPersonResult) (*dex.StepDecision, error) {
	if err := enrolledPersonAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*PersonEnrollmentFlow)(nil)
