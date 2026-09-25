// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package customeronboarding demonstrates Connector Step factories and explicit recovery.
package customeronboarding

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	httpconnector "github.com/superdurable/dex-connectors-library/connectors/http"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	readCustomerProfileStepType  = "ReadCustomerProfile"
	grantCustomerCreditsStepType = "GrantCustomerCredits"
	reconcileCreditGrantStepType = "ReconcileCreditGrant"
)

var (
	CustomerOnboardingState = dex.DefineAttribute[State]("CustomerOnboardingState")
	CreditGrantResult       = dex.DefineAttribute[sdkgo.MutationResult[httpconnector.Response]]("CreditGrantResult")
)

type Input struct {
	CustomerID      string `json:"customerId"`
	Credits         int    `json:"credits"`
	SimulateUnknown bool   `json:"simulateUnknown,omitempty"`
}

type Profile struct {
	CustomerID string `json:"customerId"`
	Name       string `json:"name"`
	Headline   string `json:"headline"`
}

type Output struct {
	CustomerID string         `json:"customerId"`
	CallID     sdkgo.CallID   `json:"callId"`
	Branch     sdkgo.BranchID `json:"branch"`
}

type State struct {
	Input   Input   `json:"input"`
	Profile Profile `json:"profile"`
}

type profileResult = httpconnector.QueryResult
type grantResult = httpconnector.MutationResult
type reconcileResult = httpconnector.QueryResult

type CustomerOnboardingConnectorFlow struct {
	dex.FlowDefaults
	connection httpconnector.Connection
}

func NewCustomerOnboardingConnectorFlow(connection httpconnector.Connection) *CustomerOnboardingConnectorFlow {
	return &CustomerOnboardingConnectorFlow{connection: connection}
}

func (flow *CustomerOnboardingConnectorFlow) GetSteps() []dex.StepDef {
	readCustomerProfile := httpconnector.NewQueryStep(httpconnector.QueryStepConfig[Input]{
		StepType: readCustomerProfileStepType,
		Annotations: sdkgo.StepAnnotations{
			GroupID: "onboarding", GroupLabel: "Onboarding",
			Explanation: "Read the customer profile from the configured provider.",
		},
		Connection: flow.connection, BuildOperationInput: buildProfileQuery,
		Succeeded: sdkgo.GoTo(ProfileReadSucceededStep{}),
		Failed:    sdkgo.GoTo(ProfileReadFailedStep{}),
		Defect:    sdkgo.GoTo(ProfileReadFailedStep{}),
	})
	return []dex.StepDef{
		dex.DefineStartStep(InitializeCustomerOnboardingStep{}),
		dex.DefineStep(readCustomerProfile),
		dex.DefineStep(httpconnector.NewMutationStep(httpconnector.MutationStepConfig[State]{
			StepType: grantCustomerCreditsStepType,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "onboarding", GroupLabel: "Onboarding",
				Explanation: "Grant credits through an idempotent provider mutation.",
			},
			Connection: flow.connection, BuildOperationInput: buildGrantMutation,
			Succeeded:       sdkgo.GoTo(CreditGrantSucceededStep{}),
			Rejected:        sdkgo.GoTo(CreditGrantFailedStep{}),
			Uncertain:       sdkgo.GoTo(sdkgo.StepRef[grantResult](reconcileCreditGrantStepType)),
			Defect:          sdkgo.GoTo(CreditGrantFailedStep{}),
			ResultAttribute: &CreditGrantResult,
			StepOptionsOverride: &dex.StepOptions{
				ExecuteFailure: dex.ProceedToOnExecuteFailure(GrantExecuteFailedStep{}, nil),
			},
		})),
		dex.DefineStep(httpconnector.NewQueryStep(httpconnector.QueryStepConfig[grantResult]{
			StepType: reconcileCreditGrantStepType,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "recovery", GroupLabel: "Recovery",
				Explanation: "Reconcile an uncertain credit grant without repeating the mutation.",
			},
			Connection: flow.connection, BuildOperationInput: buildReconciliationQuery,
			Succeeded: sdkgo.GoTo(CreditGrantReconciledStep{}),
			Failed:    sdkgo.GoTo(CreditGrantReconcileFailedStep{}),
			Defect:    sdkgo.GoTo(CreditGrantReconcileFailedStep{}),
		})),
		dex.DefineStep(ProfileReadSucceededStep{}),
		dex.DefineStep(ProfileReadFailedStep{}),
		dex.DefineStep(CreditGrantSucceededStep{}),
		dex.DefineStep(CreditGrantFailedStep{}),
		dex.DefineStep(CreditGrantReconciledStep{}),
		dex.DefineStep(CreditGrantReconcileFailedStep{}),
		dex.DefineStep(GrantExecuteFailedStep{}),
	}
}

func (*CustomerOnboardingConnectorFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{CustomerOnboardingState, CreditGrantResult}}
}

func (flow *CustomerOnboardingConnectorFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:CreditGrantResult value-type:json editable:false description:"Credit grant connector result"
func (*CustomerOnboardingConnectorFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	result, err := optionalCreditGrantResult(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"CreditGrantResult": result}}, nil
}

// dex:field attribute-key:CreditGrantResult value-type:json editable:false description:"Credit grant connector result"
func (*CustomerOnboardingConnectorFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	result, err := optionalCreditGrantResult(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"CreditGrantResult": result}}, nil
}

func optionalCreditGrantResult(ctx dex.Context) (sdkgo.MutationResult[httpconnector.Response], error) {
	result, err := CreditGrantResult.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return sdkgo.MutationResult[httpconnector.Response]{}, nil
	}
	return result, err
}

func buildProfileQuery(input Input) (httpconnector.Request, error) {
	if input.CustomerID == "" || input.Credits <= 0 {
		return httpconnector.Request{}, fmt.Errorf("customer ID and positive credits are required")
	}
	return httpconnector.Request{Method: http.MethodGet, Path: "/profiles/" + input.CustomerID}, nil
}

func buildGrantMutation(state State) (httpconnector.Request, error) {
	path := "/credits"
	if state.Input.SimulateUnknown {
		path = "/credits-unknown"
	}
	return httpconnector.Request{
		Method: http.MethodPost, Path: path,
		Body: map[string]any{"customerId": state.Profile.CustomerID, "credits": state.Input.Credits},
	}, nil
}

func buildReconciliationQuery(result grantResult) (httpconnector.Request, error) {
	if result.Receipt.CallID == "" {
		return httpconnector.Request{}, fmt.Errorf("credit grant receipt is missing")
	}
	return httpconnector.Request{
		Method: http.MethodGet, Path: "/mutations/" + string(result.Receipt.CallID),
	}, nil
}

// dex:group group-id:onboarding group-label:"Onboarding"
// dex:explanation text:"Persist the onboarding request before invoking Connector Steps."
type InitializeCustomerOnboardingStep struct {
	dex.StepDefaultsNoWaitFor[Input]
}

func (InitializeCustomerOnboardingStep) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	if err := CustomerOnboardingState.Set(ctx, State{Input: input}); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[Input](readCustomerProfileStepType), input), nil
}

// dex:group group-id:onboarding group-label:"Onboarding"
// dex:explanation text:"Store the customer profile and prepare the credit grant."
type ProfileReadSucceededStep struct {
	dex.StepDefaultsNoWaitFor[profileResult]
}

func (ProfileReadSucceededStep) Execute(ctx dex.Context, result profileResult) (*dex.StepDecision, error) {
	state, err := CustomerOnboardingState.Get(ctx)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(result.Value.Body, &state.Profile); err != nil {
		return nil, fmt.Errorf("decode profile: %w", err)
	}
	if err := CustomerOnboardingState.Set(ctx, state); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[State](grantCustomerCreditsStepType), state), nil
}

// dex:group group-id:failure group-label:"Failure"
// dex:explanation text:"Close after a profile query failure or local input defect."
type ProfileReadFailedStep struct {
	dex.StepDefaultsNoWaitFor[profileResult]
}

func (ProfileReadFailedStep) Execute(_ dex.Context, result profileResult) (*dex.StepDecision, error) {
	return dex.ForceFail(resultFailure("profile read", result.Failure)), nil
}

// dex:group group-id:onboarding group-label:"Onboarding"
// dex:explanation text:"Complete after the provider confirms the credit grant."
type CreditGrantSucceededStep struct {
	dex.StepDefaultsNoWaitFor[grantResult]
}

func (CreditGrantSucceededStep) Execute(ctx dex.Context, result grantResult) (*dex.StepDecision, error) {
	if err := validateCommittedCreditGrant(ctx, result.Receipt.CallID); err != nil {
		return nil, err
	}
	state, err := CustomerOnboardingState.Get(ctx)
	if err != nil {
		return nil, err
	}
	return dex.GracefulComplete(Output{
		CustomerID: state.Input.CustomerID,
		CallID:     result.Receipt.CallID,
		Branch:     result.Branch,
	}), nil
}

// dex:group group-id:failure group-label:"Failure"
// dex:explanation text:"Close after a confirmed credit grant rejection or local defect."
type CreditGrantFailedStep struct {
	dex.StepDefaultsNoWaitFor[grantResult]
}

func (CreditGrantFailedStep) Execute(_ dex.Context, result grantResult) (*dex.StepDecision, error) {
	return dex.ForceFail(resultFailure("credit grant", result.Failure)), nil
}

// dex:group group-id:recovery group-label:"Recovery"
// dex:explanation text:"Complete after reconciliation confirms the original mutation."
type CreditGrantReconciledStep struct {
	dex.StepDefaultsNoWaitFor[reconcileResult]
}

func (CreditGrantReconciledStep) Execute(ctx dex.Context, result reconcileResult) (*dex.StepDecision, error) {
	var status struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(result.Value.Body, &status); err != nil || status.Status != "succeeded" {
		return dex.ForceFail("credit grant reconciliation returned an invalid status"), nil
	}
	grant, err := CreditGrantResult.Get(ctx)
	if err != nil {
		return nil, err
	}
	if err := validateCommittedCreditGrant(ctx, grant.Receipt.CallID); err != nil {
		return nil, err
	}
	state, err := CustomerOnboardingState.Get(ctx)
	if err != nil {
		return nil, err
	}
	return dex.GracefulComplete(Output{
		CustomerID: state.Input.CustomerID,
		CallID:     grant.Receipt.CallID,
		Branch:     httpconnector.MutationBranchSucceeded,
	}), nil
}

// dex:group group-id:failure group-label:"Failure"
// dex:explanation text:"Close after reconciliation cannot confirm the original mutation."
type CreditGrantReconcileFailedStep struct {
	dex.StepDefaultsNoWaitFor[reconcileResult]
}

func (CreditGrantReconcileFailedStep) Execute(_ dex.Context, result reconcileResult) (*dex.StepDecision, error) {
	return dex.ForceFail(resultFailure("credit grant reconciliation", result.Failure)), nil
}

// dex:group group-id:failure group-label:"Failure"
// dex:explanation text:"Close after the credit grant Step exhausts its Dex retry policy."
type GrantExecuteFailedStep struct {
	dex.StepDefaultsNoWaitFor[State]
}

func (GrantExecuteFailedStep) Execute(_ dex.Context, _ State) (*dex.StepDecision, error) {
	return dex.ForceFail("credit grant connector Step exhausted retries"), nil
}

func resultFailure(operation string, failure *sdkgo.Failure) string {
	if failure == nil {
		return operation + " failed without a classified failure"
	}
	return fmt.Sprintf("%s failed: %s", operation, failure.Kind)
}

func validateCommittedCreditGrant(ctx dex.Context, callID sdkgo.CallID) error {
	result, err := CreditGrantResult.Get(ctx)
	if err != nil {
		return fmt.Errorf("load committed credit grant Result: %w", err)
	}
	if result.Receipt.CallID != callID {
		return fmt.Errorf("committed credit grant Result does not match transition input")
	}
	return nil
}

var _ dex.Flow = (*CustomerOnboardingConnectorFlow)(nil)
