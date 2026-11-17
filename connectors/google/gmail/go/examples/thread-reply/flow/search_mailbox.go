// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package threadreply

import (
	"errors"

	gmail "github.com/superdurable/dex-connectors-library/connectors/google/gmail/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const searchMailboxFlowType = "GmailSearchMailbox"
const listMailboxMessagesStepType = "ListMailboxMessages"

var mailboxSearchPageAttribute = dex.DefineAttribute[gmail.MessagesPage]("gmail-mailbox-search-page")

// SearchMailboxInput selects a bounded page of Gmail messages.
type SearchMailboxInput struct {
	// Query uses Gmail's mailbox search syntax.
	Query string `json:"query"`
	// MaxResults is 1 through 500; zero uses Gmail's default.
	MaxResults int `json:"maxResults,omitempty"`
}

// SearchMailboxFlow lists message identities in the connected mailbox.
type SearchMailboxFlow struct {
	dex.FlowDefaults
	connection gmail.Connection
}

// NewSearchMailboxFlow binds the example's existing Gmail connection.
func NewSearchMailboxFlow(connection gmail.Connection) *SearchMailboxFlow {
	return &SearchMailboxFlow{connection: connection}
}

// GetFlowType keeps this example Flow's durable identity stable.
func (*SearchMailboxFlow) GetFlowType() string { return searchMailboxFlowType }

// GetSteps registers the start, Gmail Query, and completion Steps.
func (flow *SearchMailboxFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startMailboxSearch{}),
		dex.DefineStep(gmail.NewListMessagesStep(gmail.ListMessagesStepConfig[SearchMailboxInput]{
			StepType: listMailboxMessagesStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "gmail", GroupLabel: "Gmail",
				Explanation: "Search one page of Gmail messages by query.",
			},
			Connection: flow.connection,
			MapToOperationInput: func(input SearchMailboxInput) gmail.ListMessagesInput {
				return gmail.ListMessagesInput{Query: input.Query, MaxResults: input.MaxResults}
			},
			Listed: sdkgo.GoTo(completeMailboxSearch{}),
		})),
		dex.DefineStep(completeMailboxSearch{}),
	}
}

// GetRPCs exposes the result in Dex Web.
func (flow *SearchMailboxFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetPersistenceSchema registers the result Attribute used by Dex Web views.
func (*SearchMailboxFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{mailboxSearchPageAttribute}}
}

// dex:field attribute-key:gmail-mailbox-search-page value-type:json editable:false description:"Message IDs matching the search"
// GetDexSummary returns the last search page to Dex Web.
func (*SearchMailboxFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := optionalMailboxSearchPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"gmail-mailbox-search-page": page}}, nil
}

// dex:field attribute-key:gmail-mailbox-search-page value-type:json editable:false description:"Message IDs and next page token"
// GetDexDisplay returns the last search page to Dex Web.
func (*SearchMailboxFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := optionalMailboxSearchPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"gmail-mailbox-search-page": page}}, nil
}

func optionalMailboxSearchPage(ctx dex.Context) (gmail.MessagesPage, error) {
	page, err := mailboxSearchPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return gmail.MessagesPage{}, nil
	}
	return page, err
}

// dex:group group-id:gmail group-label:"Gmail"
// dex:explanation text:"Accept a mailbox query and request matching message IDs."
type startMailboxSearch struct{ dex.StepDefaults }

func (startMailboxSearch) GetStepType() string { return "StartMailboxSearch" }
func (startMailboxSearch) WaitFor(dex.Context, SearchMailboxInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startMailboxSearch) Execute(_ dex.Context, input SearchMailboxInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[SearchMailboxInput](listMailboxMessagesStepType), input), nil
}

// dex:group group-id:gmail group-label:"Gmail"
// dex:explanation text:"Persist the matching message IDs and complete the search Flow."
type completeMailboxSearch struct {
	dex.StepDefaultsNoWaitFor[gmail.ListMessagesResult]
}

func (completeMailboxSearch) GetStepType() string { return "CompleteMailboxSearch" }
func (completeMailboxSearch) Execute(ctx dex.Context, result gmail.ListMessagesResult) (*dex.StepDecision, error) {
	if err := mailboxSearchPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}
