// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package customeronboarding demonstrates Query, Action, and unknown-outcome recovery in Dex Steps.
package customeronboarding

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	httpconnector "github.com/superdurable/dex-connectors-library/connectors/http"
	connector "github.com/superdurable/dex-connectors-library/sdk/go"
	"github.com/superdurable/dex/sdk-go/dex"
)

var CreditGrantCallID = dex.DefineAttribute[string]("CreditGrantCallID")

type Input struct {
	CustomerID string                  `json:"customerId"`
	Connection connector.ConnectionRef `json:"connection"`
	Credits    int                     `json:"credits"`
}

type profile struct {
	CustomerID string `json:"customerId"`
	Name       string `json:"name"`
	Headline   string `json:"headline"`
}

type actionState struct {
	Input   Input            `json:"input"`
	Profile profile          `json:"profile"`
	CallID  connector.CallID `json:"callId"`
}

type Output struct {
	CustomerID string                  `json:"customerId"`
	CallID     connector.CallID        `json:"callId"`
	Outcome    connector.ActionOutcome `json:"outcome"`
}

type CustomerOnboardingConnectorFlow struct {
	dex.FlowDefaults
	client *httpconnector.Client
}

func NewCustomerOnboardingConnectorFlow(client *httpconnector.Client) *CustomerOnboardingConnectorFlow {
	if client == nil {
		panic("customer onboarding connector Flow requires an HTTP connector")
	}
	return &CustomerOnboardingConnectorFlow{client: client}
}

func (flow *CustomerOnboardingConnectorFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(readProfileStep{client: flow.client}),
		dex.DefineStep(grantCreditsStep{client: flow.client}),
		dex.DefineStep(recoverCreditGrantStep{client: flow.client}),
	}
}

func (*CustomerOnboardingConnectorFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{CreditGrantCallID}}
}

type readProfileStep struct {
	dex.StepDefaultsNoWaitFor[Input]
	client *httpconnector.Client
}

func (step readProfileStep) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	if input.CustomerID == "" || input.Credits <= 0 {
		return nil, fmt.Errorf("customer ID and positive credits are required")
	}
	result, err := step.client.Query(ctx, http.MethodGet, httpconnector.Request{
		Connection: input.Connection,
		Path:       "/profiles/" + input.CustomerID,
	})
	if err != nil {
		if connector.IsRetryable(err) {
			return nil, dex.RetryAfter(time.Second, err)
		}
		return nil, err
	}
	var customerProfile profile
	if err := json.Unmarshal(result.Value.Body, &customerProfile); err != nil {
		return nil, fmt.Errorf("decode profile: %w", err)
	}
	callID := connector.NewCallID()
	if err := CreditGrantCallID.Set(ctx, string(callID)); err != nil {
		return nil, err
	}
	return dex.GoTo(grantCreditsStep{}, actionState{Input: input, Profile: customerProfile, CallID: callID}), nil
}

type grantCreditsStep struct {
	dex.StepDefaultsNoWaitFor[actionState]
	client *httpconnector.Client
}

func (step grantCreditsStep) Execute(ctx dex.Context, state actionState) (*dex.StepDecision, error) {
	result, err := step.client.Action(ctx, http.MethodPost, state.CallID, httpconnector.Request{
		Connection: state.Input.Connection,
		Path:       "/credits",
		Body: map[string]any{
			"customerId": state.Profile.CustomerID,
			"credits":    state.Input.Credits,
		},
	})
	if err != nil {
		if connector.IsUnknownMutation(err) {
			return dex.GoTo(recoverCreditGrantStep{}, state), nil
		}
		if connector.IsRetryable(err) {
			return nil, dex.RetryAfter(time.Second, err)
		}
		return nil, err
	}
	return dex.GracefulComplete(Output{CustomerID: state.Input.CustomerID, CallID: state.CallID, Outcome: result.Receipt.Outcome}), nil
}

type recoverCreditGrantStep struct {
	dex.StepDefaultsNoWaitFor[actionState]
	client *httpconnector.Client
}

func (step recoverCreditGrantStep) Execute(ctx dex.Context, state actionState) (*dex.StepDecision, error) {
	result, err := step.client.Query(ctx, http.MethodGet, httpconnector.Request{
		Connection: state.Input.Connection,
		Path:       "/actions/" + string(state.CallID),
	})
	if err != nil {
		if connector.IsKind(err, connector.ErrorNotFound) || connector.IsRetryable(err) {
			return nil, dex.RetryAfter(time.Second, err)
		}
		return nil, err
	}
	var status struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(result.Value.Body, &status); err != nil {
		return nil, fmt.Errorf("decode action recovery result: %w", err)
	}
	if status.Status != "succeeded" {
		return nil, dex.RetryAfter(time.Second, fmt.Errorf("credit grant remains unresolved"))
	}
	return dex.GracefulComplete(Output{CustomerID: state.Input.CustomerID, CallID: state.CallID, Outcome: connector.ActionSucceeded}), nil
}

var _ dex.Flow = (*CustomerOnboardingConnectorFlow)(nil)
