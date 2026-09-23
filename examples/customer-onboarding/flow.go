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
	connector "github.com/superdurable/dex-connectors-library/sdk/go"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	readCustomerProfileStepType  = "ReadCustomerProfile"
	grantCustomerCreditsStepType = "GrantCustomerCredits"
	reconcileCreditGrantStepType = "ReconcileCreditGrant"
)

var CreditGrantResult = dex.DefineAttribute[connector.MutationResult[httpconnector.Response]]("CreditGrantResult")

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
	CustomerID string             `json:"customerId"`
	CallID     connector.CallID   `json:"callId"`
	Branch     connector.BranchID `json:"branch"`
}

type profileStepOutput = connector.QueryStepOutput[Input, httpconnector.Response]
type grantStepOutput = connector.MutationStepOutput[profileStepOutput, httpconnector.Response]
type reconcileStepOutput = connector.QueryStepOutput[grantStepOutput, httpconnector.Response]

type CustomerOnboardingConnectorFlow struct {
	dex.FlowDefaults
	client             *httpconnector.Client
	providerConnection connector.ConnectionRef
}

func NewCustomerOnboardingConnectorFlow(client *httpconnector.Client, providerConnection connector.ConnectionRef) *CustomerOnboardingConnectorFlow {
	if client == nil {
		panic("customer onboarding connector Flow requires an HTTP connector")
	}
	if err := providerConnection.Validate(); err != nil {
		panic("customer onboarding connector Flow requires a valid provider connection")
	}
	return &CustomerOnboardingConnectorFlow{client: client, providerConnection: providerConnection}
}

func (flow *CustomerOnboardingConnectorFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(connector.MustNewQueryStep(connector.QueryStepConfig[Input, httpconnector.Request, httpconnector.Response]{
			StepType: readCustomerProfileStepType,
			Presentation: connector.StepPresentation{
				GroupID: "onboarding", GroupLabel: "Onboarding",
				Explanation: "Read the customer profile from the configured provider.",
			},
			Operation: flow.client.Query(), Connection: flow.providerConnection, BuildInput: buildProfileQuery,
			Branches: []connector.BranchTarget[profileStepOutput]{
				connector.GoToBranch(httpconnector.QueryBranchSucceeded, connector.StepRef[profileStepOutput](grantCustomerCreditsStepType)),
				connector.GoToBranch(httpconnector.QueryBranchFailed, ProfileReadFailedStep{}),
				connector.GoToBranch(httpconnector.QueryBranchDefect, ProfileReadFailedStep{}),
			},
		})),
		dex.DefineStep(connector.MustNewMutationStep(connector.MutationStepConfig[profileStepOutput, httpconnector.Request, httpconnector.Response]{
			StepType: grantCustomerCreditsStepType,
			Presentation: connector.StepPresentation{
				GroupID: "onboarding", GroupLabel: "Onboarding",
				Explanation: "Grant credits through an idempotent provider mutation.",
			},
			Operation: flow.client.Mutation(), Connection: flow.providerConnection, BuildInput: buildGrantMutation,
			Branches: []connector.BranchTarget[grantStepOutput]{
				connector.GoToBranch(httpconnector.MutationBranchSucceeded, CreditGrantSucceededStep{}),
				connector.GoToBranch(httpconnector.MutationBranchRejected, CreditGrantFailedStep{}),
				connector.GoToBranch(httpconnector.MutationBranchUncertain, connector.StepRef[grantStepOutput](reconcileCreditGrantStepType)),
				connector.GoToBranch(httpconnector.MutationBranchDefect, CreditGrantFailedStep{}),
			},
			ResultAttribute: &CreditGrantResult,
			StepOptionsOverride: &dex.StepOptions{
				ExecuteFailure: dex.ProceedToOnExecuteFailure(GrantExecuteFailedStep{}, nil),
			},
		})),
		dex.DefineStep(connector.MustNewQueryStep(connector.QueryStepConfig[grantStepOutput, httpconnector.Request, httpconnector.Response]{
			StepType: reconcileCreditGrantStepType,
			Presentation: connector.StepPresentation{
				GroupID: "recovery", GroupLabel: "Recovery",
				Explanation: "Reconcile an uncertain credit grant without repeating the mutation.",
			},
			Operation: flow.client.Query(), Connection: flow.providerConnection, BuildInput: buildReconciliationQuery,
			Branches: []connector.BranchTarget[reconcileStepOutput]{
				connector.GoToBranch(httpconnector.QueryBranchSucceeded, CreditGrantReconciledStep{}),
				connector.GoToBranch(httpconnector.QueryBranchFailed, CreditGrantReconcileFailedStep{}),
				connector.GoToBranch(httpconnector.QueryBranchDefect, CreditGrantReconcileFailedStep{}),
			},
		})),
		dex.DefineStep(ProfileReadFailedStep{}),
		dex.DefineStep(CreditGrantSucceededStep{}),
		dex.DefineStep(CreditGrantFailedStep{}),
		dex.DefineStep(CreditGrantReconciledStep{}),
		dex.DefineStep(CreditGrantReconcileFailedStep{}),
		dex.DefineStep(GrantExecuteFailedStep{}),
	}
}

func (*CustomerOnboardingConnectorFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{CreditGrantResult}}
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

func optionalCreditGrantResult(ctx dex.Context) (connector.MutationResult[httpconnector.Response], error) {
	result, err := CreditGrantResult.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return connector.MutationResult[httpconnector.Response]{}, nil
	}
	return result, err
}

func buildProfileQuery(input Input) (httpconnector.Request, error) {
	if input.CustomerID == "" || input.Credits <= 0 {
		return httpconnector.Request{}, fmt.Errorf("customer ID and positive credits are required")
	}
	return httpconnector.Request{Method: http.MethodGet, Path: "/profiles/" + input.CustomerID}, nil
}

func buildGrantMutation(output profileStepOutput) (httpconnector.Request, error) {
	var customerProfile Profile
	if err := json.Unmarshal(output.Result.Value.Body, &customerProfile); err != nil {
		return httpconnector.Request{}, fmt.Errorf("decode profile: %w", err)
	}
	path := "/credits"
	if output.Input.SimulateUnknown {
		path = "/credits-unknown"
	}
	return httpconnector.Request{
		Method: http.MethodPost, Path: path,
		Body: map[string]any{"customerId": customerProfile.CustomerID, "credits": output.Input.Credits},
	}, nil
}

func buildReconciliationQuery(output grantStepOutput) (httpconnector.Request, error) {
	if output.Result.Receipt.CallID == "" {
		return httpconnector.Request{}, fmt.Errorf("credit grant receipt is missing")
	}
	return httpconnector.Request{
		Method: http.MethodGet, Path: "/mutations/" + string(output.Result.Receipt.CallID),
	}, nil
}

// dex:group group-id:failure group-label:"Failure"
// dex:explanation text:"Close after a profile query failure or local input defect."
type ProfileReadFailedStep struct {
	dex.StepDefaultsNoWaitFor[profileStepOutput]
}

func (ProfileReadFailedStep) Execute(_ dex.Context, output profileStepOutput) (*dex.StepDecision, error) {
	return dex.ForceFail(resultFailure("profile read", output.Result.Failure)), nil
}

// dex:group group-id:onboarding group-label:"Onboarding"
// dex:explanation text:"Complete after the provider confirms the credit grant."
type CreditGrantSucceededStep struct {
	dex.StepDefaultsNoWaitFor[grantStepOutput]
}

func (CreditGrantSucceededStep) Execute(ctx dex.Context, output grantStepOutput) (*dex.StepDecision, error) {
	if err := validateCommittedCreditGrant(ctx, output.Result.Receipt.CallID); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(Output{
		CustomerID: output.Input.Input.CustomerID,
		CallID:     output.Result.Receipt.CallID,
		Branch:     output.Result.Branch,
	}), nil
}

// dex:group group-id:failure group-label:"Failure"
// dex:explanation text:"Close after a confirmed credit grant rejection or local defect."
type CreditGrantFailedStep struct {
	dex.StepDefaultsNoWaitFor[grantStepOutput]
}

func (CreditGrantFailedStep) Execute(_ dex.Context, output grantStepOutput) (*dex.StepDecision, error) {
	return dex.ForceFail(resultFailure("credit grant", output.Result.Failure)), nil
}

// dex:group group-id:recovery group-label:"Recovery"
// dex:explanation text:"Complete after reconciliation confirms the original mutation."
type CreditGrantReconciledStep struct {
	dex.StepDefaultsNoWaitFor[reconcileStepOutput]
}

func (CreditGrantReconciledStep) Execute(ctx dex.Context, output reconcileStepOutput) (*dex.StepDecision, error) {
	var status struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(output.Result.Value.Body, &status); err != nil || status.Status != "succeeded" {
		return dex.ForceFail("credit grant reconciliation returned an invalid status"), nil
	}
	grant := output.Input
	if err := validateCommittedCreditGrant(ctx, grant.Result.Receipt.CallID); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(Output{
		CustomerID: grant.Input.Input.CustomerID,
		CallID:     grant.Result.Receipt.CallID,
		Branch:     httpconnector.MutationBranchSucceeded,
	}), nil
}

// dex:group group-id:failure group-label:"Failure"
// dex:explanation text:"Close after reconciliation cannot confirm the original mutation."
type CreditGrantReconcileFailedStep struct {
	dex.StepDefaultsNoWaitFor[reconcileStepOutput]
}

func (CreditGrantReconcileFailedStep) Execute(_ dex.Context, output reconcileStepOutput) (*dex.StepDecision, error) {
	return dex.ForceFail(resultFailure("credit grant reconciliation", output.Result.Failure)), nil
}

// dex:group group-id:failure group-label:"Failure"
// dex:explanation text:"Close after the credit grant Step exhausts its Dex retry policy."
type GrantExecuteFailedStep struct {
	dex.StepDefaultsNoWaitFor[profileStepOutput]
}

func (GrantExecuteFailedStep) Execute(_ dex.Context, _ profileStepOutput) (*dex.StepDecision, error) {
	return dex.ForceFail("credit grant connector Step exhausted retries"), nil
}

func resultFailure(operation string, failure *connector.Failure) string {
	if failure == nil {
		return operation + " failed without a classified failure"
	}
	return fmt.Sprintf("%s failed: %s", operation, failure.Kind)
}

func validateCommittedCreditGrant(ctx dex.Context, callID connector.CallID) error {
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
