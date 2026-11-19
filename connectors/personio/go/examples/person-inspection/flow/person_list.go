// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package personinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/personio/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// PersonListFlowType is the stable identity of the list example.
const PersonListFlowType = "PersonioPersonList"

const personListStepType = "CallListPersons"

var personPageAttribute = dex.DefineAttribute[personio.PersonPage]("personio-person-page")

// PersonListFlow reads one page of Personio persons.
type PersonListFlow struct {
	dex.FlowDefaults
	connection personio.Connection
}

// NewPersonListFlow binds the provider connection at registration.
func NewPersonListFlow(connection personio.Connection) *PersonListFlow {
	return &PersonListFlow{connection: connection}
}

// GetFlowType returns the stable example identity.
func (*PersonListFlow) GetFlowType() string { return PersonListFlowType }

// GetPersistenceSchema registers the person page attribute.
func (*PersonListFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{personPageAttribute}}
}

// GetSteps returns the start, provider read, and completion Steps.
func (flow *PersonListFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startPersonList{}),
		dex.DefineStep(personio.NewListPersonsStep(personio.ListPersonsStepConfig[personio.ListPersonsInput]{
			StepType: personListStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Personio", Explanation: "List one page of selected persons."},
			MapToOperationInput: func(input personio.ListPersonsInput) personio.ListPersonsInput { return input },
			Listed:              sdkgo.GoTo(completePersonList{}),
		})),
		dex.DefineStep(completePersonList{}),
	}
}

// GetRPCs exposes summary and display views in Dex Web.
func (flow *PersonListFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:personio-person-page value-type:json editable:false description:"Person page"
// GetDexSummary reads the persisted page.
func (*PersonListFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readPersonPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"personio-person-page": page}}, nil
}

// dex:field attribute-key:personio-person-page value-type:json editable:false description:"Person page"
// GetDexDisplay reads the persisted page.
func (*PersonListFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readPersonPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"personio-person-page": page}}, nil
}

func readPersonPage(ctx dex.Context) (personio.PersonPage, error) {
	page, err := personPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return personio.PersonPage{}, nil
	}
	return page, err
}

// dex:group group-id:person-list-request group-label:"Person list request"
// dex:explanation text:"Pass the typed filters to the Personio list Step."
type startPersonList struct{ dex.StepDefaults }

func (startPersonList) GetStepType() string { return "StartListPersons" }
func (startPersonList) WaitFor(dex.Context, personio.ListPersonsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startPersonList) Execute(_ dex.Context, input personio.ListPersonsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[personio.ListPersonsInput](personListStepType), input), nil
}

// dex:group group-id:person-list-result group-label:"Person list result"
// dex:explanation text:"Persist the selected person page and complete the Flow."
type completePersonList struct {
	dex.StepDefaultsNoWaitFor[personio.ListPersonsResult]
}

func (completePersonList) GetStepType() string { return "CompleteListPersons" }
func (completePersonList) Execute(ctx dex.Context, result personio.ListPersonsResult) (*dex.StepDecision, error) {
	if err := personPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*PersonListFlow)(nil)
