// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package customeronboarding demonstrates Query, Mutation, and explicit recovery in Dex Steps.
package customeronboarding

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	httpconnector "github.com/superdurable/dex-connectors-library/connectors/http"
	connector "github.com/superdurable/dex-connectors-library/sdk/go"
	"github.com/superdurable/dex/sdk-go/dex"
)

var CreditGrantReceipt = dex.DefineAttribute[connector.Receipt]("CreditGrantReceipt")

type Input struct {
	CustomerID      string `json:"customerId"`
	Credits         int    `json:"credits"`
	SimulateUnknown bool   `json:"simulateUnknown,omitempty"`
}

type profile struct {
	CustomerID string `json:"customerId"`
	Name       string `json:"name"`
	Headline   string `json:"headline"`
}

type grantCreditsState struct {
	Input   Input   `json:"input"`
	Profile profile `json:"profile"`
}

type reconcileCreditGrantState struct {
	Grant   grantCreditsState `json:"grant"`
	Receipt connector.Receipt `json:"receipt"`
}

type creditGrantFailureState struct {
	Receipt connector.Receipt `json:"receipt"`
	Failure connector.Failure `json:"failure"`
}

type profileReadFailureState struct {
	Failure connector.Failure `json:"failure"`
}

type Output struct {
	CustomerID string                    `json:"customerId"`
	CallID     connector.CallID          `json:"callId"`
	Outcome    connector.MutationOutcome `json:"outcome"`
}

type CustomerOnboardingConnectorFlow struct {
	dex.FlowDefaults
	client             *httpconnector.Client
	providerConnection connector.ConnectionRef
}

func NewCustomerOnboardingConnectorFlow(
	client *httpconnector.Client,
	providerConnection connector.ConnectionRef,
) *CustomerOnboardingConnectorFlow {
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
		dex.DefineStartStep(ReadCustomerProfileStep{client: flow.client, providerConnection: flow.providerConnection}),
		dex.DefineStep(GrantCustomerCreditsStep{client: flow.client, providerConnection: flow.providerConnection}),
		dex.DefineStep(ReconcileCreditGrantStep{client: flow.client, providerConnection: flow.providerConnection}),
		dex.DefineStep(ProfileReadFailedStep{}),
		dex.DefineStep(CreditGrantFailedStep{}),
	}
}

func (flow *CustomerOnboardingConnectorFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

func (*CustomerOnboardingConnectorFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{CreditGrantReceipt}}
}

// dex:field attribute-key:CreditGrantReceipt value-type:json editable:false description:"Credit grant provider receipt"
func (*CustomerOnboardingConnectorFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	receipt, err := optionalCreditGrantReceipt(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"CreditGrantReceipt": receipt}}, nil
}

// dex:field attribute-key:CreditGrantReceipt value-type:json editable:false description:"Credit grant provider receipt"
func (*CustomerOnboardingConnectorFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	receipt, err := optionalCreditGrantReceipt(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"CreditGrantReceipt": receipt}}, nil
}

func optionalCreditGrantReceipt(ctx dex.Context) (connector.Receipt, error) {
	receipt, err := CreditGrantReceipt.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return connector.Receipt{}, nil
	}
	return receipt, err
}

// dex:group group-id:onboarding group-label:"Onboarding"
// dex:explanation text:"Read the customer profile from the configured provider."
type ReadCustomerProfileStep struct {
	dex.StepDefaultsNoWaitFor[Input]
	client             *httpconnector.Client
	providerConnection connector.ConnectionRef
}

func (step ReadCustomerProfileStep) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	if input.CustomerID == "" || input.Credits <= 0 {
		return nil, fmt.Errorf("customer ID and positive credits are required")
	}
	result, err := connector.RunQuery(ctx, step.client.Query(), step.providerConnection, httpconnector.Request{
		Method: http.MethodGet,
		Path:   "/profiles/" + input.CustomerID,
	})
	if err != nil {
		return nil, err
	}
	if result.Outcome == connector.QueryFailed {
		return dex.GoTo(ProfileReadFailedStep{}, profileReadFailureState{Failure: *result.Failure}), nil
	}
	var customerProfile profile
	if err := json.Unmarshal(result.Value.Body, &customerProfile); err != nil {
		return nil, fmt.Errorf("decode profile: %w", err)
	}
	return dex.GoTo(GrantCustomerCreditsStep{}, grantCreditsState{Input: input, Profile: customerProfile}), nil
}

// dex:group group-id:onboarding group-label:"Onboarding"
// dex:explanation text:"Grant credits through an idempotent provider mutation."
type GrantCustomerCreditsStep struct {
	dex.StepDefaultsNoWaitFor[grantCreditsState]
	client             *httpconnector.Client
	providerConnection connector.ConnectionRef
}

func (step GrantCustomerCreditsStep) Execute(ctx dex.Context, state grantCreditsState) (*dex.StepDecision, error) {
	path := "/credits"
	if state.Input.SimulateUnknown {
		path = "/credits-unknown"
	}
	result, err := connector.RunMutation(ctx, step.client.Mutation(), step.providerConnection, httpconnector.Request{
		Method: http.MethodPost,
		Path:   path,
		Body: map[string]any{
			"customerId": state.Profile.CustomerID,
			"credits":    state.Input.Credits,
		},
	})
	if err != nil {
		return nil, err
	}
	if err := CreditGrantReceipt.Set(ctx, result.Receipt); err != nil {
		return nil, err
	}
	switch result.Outcome {
	case connector.MutationSucceeded:
		return dex.GracefulComplete(Output{
			CustomerID: state.Input.CustomerID, CallID: result.Receipt.CallID, Outcome: result.Outcome,
		}), nil
	case connector.MutationFailed:
		return dex.GoTo(CreditGrantFailedStep{}, creditGrantFailureState{
			Receipt: result.Receipt, Failure: *result.Failure,
		}), nil
	case connector.MutationUnknown:
		return dex.GoTo(ReconcileCreditGrantStep{}, reconcileCreditGrantState{
			Grant: state, Receipt: result.Receipt,
		}), nil
	default:
		return nil, fmt.Errorf("unsupported mutation outcome %q", result.Outcome)
	}
}

// dex:group group-id:recovery group-label:"Recovery"
// dex:explanation text:"Reconcile an unknown credit grant without repeating the mutation."
type ReconcileCreditGrantStep struct {
	dex.StepDefaultsNoWaitFor[reconcileCreditGrantState]
	client             *httpconnector.Client
	providerConnection connector.ConnectionRef
}

func (step ReconcileCreditGrantStep) Execute(ctx dex.Context, state reconcileCreditGrantState) (*dex.StepDecision, error) {
	committedReceipt, err := CreditGrantReceipt.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("load committed credit grant receipt: %w", err)
	}
	if committedReceipt.CallID != state.Receipt.CallID {
		return nil, fmt.Errorf("credit grant receipt does not match recovery state")
	}
	result, err := connector.RunQuery(ctx, step.client.Query(), step.providerConnection, httpconnector.Request{
		Method: http.MethodGet,
		Path:   "/mutations/" + string(state.Receipt.CallID),
	})
	if err != nil {
		return nil, err
	}
	if result.Outcome == connector.QueryFailed {
		if result.Failure.Kind == connector.FailureNotFound {
			return nil, dex.RetryAfter(time.Second, fmt.Errorf("credit grant is not visible yet"))
		}
		return dex.ForceFail(fmt.Sprintf("credit grant reconciliation failed: %s", result.Failure.Kind)), nil
	}
	var status struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(result.Value.Body, &status); err != nil {
		return nil, fmt.Errorf("decode mutation recovery result: %w", err)
	}
	if status.Status != "succeeded" {
		return nil, dex.RetryAfter(time.Second, fmt.Errorf("credit grant remains unresolved"))
	}
	state.Receipt.ProviderRequestID = result.Receipt.ProviderRequestID
	state.Receipt.ObservedAt = result.Receipt.ObservedAt
	if err := CreditGrantReceipt.Set(ctx, state.Receipt); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(Output{
		CustomerID: state.Grant.Input.CustomerID,
		CallID:     state.Receipt.CallID,
		Outcome:    connector.MutationSucceeded,
	}), nil
}

// dex:group group-id:failure group-label:"Failure"
// dex:explanation text:"Close the process after a confirmed profile query failure."
type ProfileReadFailedStep struct {
	dex.StepDefaultsNoWaitFor[profileReadFailureState]
}

func (ProfileReadFailedStep) Execute(_ dex.Context, state profileReadFailureState) (*dex.StepDecision, error) {
	return dex.ForceFail(fmt.Sprintf("profile read failed: %s", state.Failure.Kind)), nil
}

// dex:group group-id:failure group-label:"Failure"
// dex:explanation text:"Close the process after a confirmed credit grant failure."
type CreditGrantFailedStep struct {
	dex.StepDefaultsNoWaitFor[creditGrantFailureState]
}

func (CreditGrantFailedStep) Execute(_ dex.Context, state creditGrantFailureState) (*dex.StepDecision, error) {
	return dex.ForceFail(fmt.Sprintf("credit grant failed: %s", state.Failure.Kind)), nil
}

var _ dex.Flow = (*CustomerOnboardingConnectorFlow)(nil)
