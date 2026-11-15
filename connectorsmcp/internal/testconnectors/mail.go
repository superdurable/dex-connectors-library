// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package testconnectors

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/superdurable/dex-connectors-library/connectorsmcp/internal/registry"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
	"github.com/superdurable/dex/sdk-go/dex"
)

// Identity of the mail connector, whose writes repeat on an unknown outcome
// by their repeatOnUnknown tier.
const (
	MailConnectorID   = "example-mail"
	MailModuleVersion = "v0.2.0"
	// MailMaxSends is sendMessage's manifest execution.maxSends.
	MailMaxSends int32 = 3
	// mailExecuteTimeout leaves the Fence a request window of seconds.
	mailExecuteTimeout = 30 * time.Second
)

// SendMessageInput is the input of every mail write.
type SendMessageInput struct {
	// To is the recipient address.
	To string `json:"to"`
	// Text is the message text.
	Text string `json:"text"`
}

// SentMessage is the result of every mail write.
type SentMessage struct {
	// MessageID names the accepted message.
	MessageID string `json:"messageId"`
}

func mailBranches() []sdkgo.BranchDefinition {
	return []sdkgo.BranchDefinition{
		{ID: "sent", Description: "The provider accepted the message."},
		{ID: "invalidRecipient", Description: "The recipient is empty; nothing was sent.", Optional: true},
		{ID: "recipientRefused", Description: "The provider refused the recipient.", Optional: true},
		{ID: sdkgo.UncertainBranchID, Description: "The message may have been sent.", Optional: true},
		{ID: sdkgo.DefectBranchID, Description: "The call or the response was invalid.", Optional: true},
	}
}

var sendMessageLimitedDefinition = sdkgo.MutationDefinition{
	Operation:       sdkgo.OperationRef{ConnectorID: MailConnectorID, OperationID: "sendMessage"},
	Branches:        mailBranches(),
	StepDefaults:    sdkgo.StepDefaults{ExecuteMethodTimeout: mailExecuteTimeout, ExecuteDurability: dex.StepDurabilitySync},
	RepeatOnUnknown: sdkgo.RepeatOnUnknownLimited,
	MaxSends:        MailMaxSends,
}

var sendAlertUnsafeDefinition = sdkgo.MutationDefinition{
	Operation:       sdkgo.OperationRef{ConnectorID: MailConnectorID, OperationID: "sendAlert"},
	Branches:        mailBranches(),
	StepDefaults:    sdkgo.StepDefaults{ExecuteMethodTimeout: mailExecuteTimeout, ExecuteDurability: dex.StepDurabilitySync},
	RepeatOnUnknown: sdkgo.RepeatOnUnknownUnsafe,
}

// MailObservations records the send cap every mail attempt saw.
type MailObservations struct {
	mu       sync.Mutex
	maxSends []int32
}

// MaxSendsPerAttempt returns Call.MaxSends of every mail attempt, in order.
func (observations *MailObservations) MaxSendsPerAttempt() []int32 {
	observations.mu.Lock()
	defer observations.mu.Unlock()
	return append([]int32(nil), observations.maxSends...)
}

func (observations *MailObservations) recordMaxSends(maxSends int32) {
	observations.mu.Lock()
	defer observations.mu.Unlock()
	observations.maxSends = append(observations.maxSends, maxSends)
}

// MailExposedAgentTools is what generated ExposedAgentTools would return: the
// limited sendMessage and the unsafe sendAlert.
func MailExposedAgentTools() []sdkgo.AgentToolMetadata {
	refusals := map[sdkgo.BranchID]sdkgo.AgentBranchOutcome{
		"invalidRecipient": sdkgo.AgentBranchOutcomeRejected,
		"recipientRefused": sdkgo.AgentBranchOutcomeRejected,
	}
	schema := json.RawMessage(`{"type":"object","additionalProperties":false,"required":["to","text"],"properties":{"to":{"type":"string"},"text":{"type":"string"}}}`)
	return []sdkgo.AgentToolMetadata{
		{
			Operation: sendMessageLimitedDefinition.Operation, Kind: sdkgo.AgentOperationKindMutation,
			ToolName: MailConnectorID + "_send_message", Description: "Send an email. A repeat may send a duplicate.",
			IsApprovalRecommended: true, RepeatOnUnknown: sdkgo.RepeatOnUnknownLimited, MaxSends: MailMaxSends,
			BranchOutcomes: refusals, InputType: "SendMessageInput", InputSchema: schema,
			Branches: sendMessageLimitedDefinition.Branches, StepDefaults: sendMessageLimitedDefinition.StepDefaults,
		},
		{
			Operation: sendAlertUnsafeDefinition.Operation, Kind: sdkgo.AgentOperationKindMutation,
			ToolName: MailConnectorID + "_send_alert", Description: "Send an alert email that must never be duplicated.",
			RepeatOnUnknown: sdkgo.RepeatOnUnknownUnsafe,
			BranchOutcomes:  refusals, InputType: "SendMessageInput", InputSchema: schema,
			Branches: sendAlertUnsafeDefinition.Branches, StepDefaults: sendAlertUnsafeDefinition.StepDefaults,
		},
	}
}

// MailConnector links the mail connector; its attempts record into observations.
func MailConnector(observations *MailObservations) registry.LinkedConnector {
	return registry.LinkedConnector{
		ConnectorID: MailConnectorID, DisplayName: "Example Mail", ModulePath: "example.com/connectors/example-mail/go",
		ModuleVersion: MailModuleVersion, ExposedAgentTools: MailExposedAgentTools(),
		OpenAgentTools: func(project *projectconfig.LoadedProject, connectionName string) ([]sdkgo.AgentTool, error) {
			return openMailAgentTools(project, connectionName, observations)
		},
	}
}

// MailConnection returns a mail connection served by provider.
func MailConnection(connectionName string, provider *FakeProvider) ConnectionsFileConnection {
	return ConnectionsFileConnection{
		ConnectorID:    MailConnectorID,
		ConnectionName: connectionName,
		Configuration:  NotesConfig{BaseURL: provider.URL()},
		Credentials:    NotesCredentials{Token: ProviderToken},
	}
}

func openMailAgentTools(project *projectconfig.LoadedProject, connectionName string, observations *MailObservations) ([]sdkgo.AgentTool, error) {
	key := projectconfig.ConnectionKey{ConnectorID: MailConnectorID, ConnectionName: connectionName}
	var config NotesConfig
	if err := project.Configuration.DecodeConnectionConfiguration(key, &config); err != nil {
		return nil, err
	}
	credentials, err := provider.NewCredentialProvider(project.Connections, key, decodeNotesCredentials)
	if err != nil {
		return nil, err
	}
	client := &notesClient{baseURL: strings.TrimRight(config.BaseURL, "/"), credentials: credentials, http: &http.Client{}}
	reference := sdkgo.ConnectionRef{Provider: MailConnectorID, Name: connectionName}
	metadata := MailExposedAgentTools()
	sendMessage, err := sdkgo.NewMutationAgentTool[SendMessageInput, SentMessage](mailMutation{definition: sendMessageLimitedDefinition, client: client, observations: observations}, reference, metadata[0])
	if err != nil {
		return nil, err
	}
	sendAlert, err := sdkgo.NewMutationAgentTool[SendMessageInput, SentMessage](mailMutation{definition: sendAlertUnsafeDefinition, client: client, observations: observations}, reference, metadata[1])
	if err != nil {
		return nil, err
	}
	return []sdkgo.AgentTool{sendMessage, sendAlert}, nil
}

func mailFailure(kind sdkgo.FailureKind, operationID string, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: MailConnectorID, Operation: operationID, Message: message}
}

// mailMutation is a fenced write without read-back, so a bounded resend dispatches directly.
type mailMutation struct {
	definition   sdkgo.MutationDefinition
	client       *notesClient
	observations *MailObservations
}

func (mutation mailMutation) Definition() sdkgo.MutationDefinition { return mutation.definition }

func (mailMutation) IdempotencyKey(id sdkgo.CallID, _ SendMessageInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// Invoke sends while the Fence allows and returns Retry after an unconfirmed
// send, so the next attempt's Decide either resends or selects uncertain.
func (mutation mailMutation) Invoke(call sdkgo.Call, input SendMessageInput) sdkgo.MutationAttempt[SentMessage] {
	operationID := mutation.definition.Operation.OperationID
	mutation.observations.recordMaxSends(call.MaxSends())
	if strings.TrimSpace(input.To) == "" {
		failure := mailFailure(sdkgo.FailureValidation, operationID, "the recipient is empty")
		return sdkgo.NewMutationBranch("invalidRecipient", SentMessage{}, &failure, sdkgo.Receipt{})
	}
	fence, err := singledispatch.NewFence(call, mailExecuteTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		failure := mailFailure(sdkgo.FailureLocalDefect, operationID, "the single-dispatch fence is unavailable")
		return sdkgo.NewMutationBranch(sdkgo.DefectBranchID, SentMessage{}, &failure, sdkgo.Receipt{})
	}
	switch fence.Decide().Kind {
	case singledispatch.SendAllowed, singledispatch.ReadBackThenResendAllowed:
	default:
		return sdkgo.NewMutationUncertain(SentMessage{}, mailFailure(sdkgo.FailureTransport, operationID, "an earlier attempt may have sent the message"), sdkgo.Receipt{})
	}
	token, err := mutation.client.token(call)
	if err != nil {
		return sdkgo.NewMutationRetry[SentMessage](mailFailure(sdkgo.FailureAuthentication, operationID, "the credential is unavailable"), 0)
	}
	var answer providerAnswer
	err = fence.Dispatch(func(requestContext context.Context) error {
		var sendErr error
		answer, sendErr = mutation.client.do(requestContext, http.MethodPost, "/messages", input, token, "")
		return sendErr
	})
	switch {
	case errors.Is(err, singledispatch.ErrCheckpoint), errors.Is(err, singledispatch.ErrInsufficientTime), errors.Is(err, singledispatch.ErrDispatchNotAllowed):
		return sdkgo.NewMutationRetry[SentMessage](mailFailure(sdkgo.FailureAvailability, operationID, "the message was not sent"), 0)
	case err != nil:
		return sdkgo.NewMutationRetry[SentMessage](mailFailure(sdkgo.FailureTransport, operationID, "the mail store did not answer"), 0)
	}
	switch {
	case answer.status == http.StatusOK:
		var accepted struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(answer.body, &accepted) != nil {
			return sdkgo.NewMutationRetry[SentMessage](mailFailure(sdkgo.FailureProtocol, operationID, "the mail store answered with an invalid message"), 0)
		}
		return sdkgo.NewMutationBranch("sent", SentMessage{MessageID: accepted.ID}, nil, sdkgo.Receipt{ProviderObjectID: accepted.ID})
	case answer.status == http.StatusBadRequest:
		failure := mailFailure(sdkgo.FailureProviderRejection, operationID, "the mail store refused the recipient")
		return sdkgo.NewMutationBranch("recipientRefused", SentMessage{}, &failure, sdkgo.Receipt{})
	case answer.status == http.StatusTooManyRequests:
		if recordErr := fence.RecordCanResend(); recordErr != nil {
			return sdkgo.NewMutationUncertain(SentMessage{}, mailFailure(sdkgo.FailureRateLimit, operationID, "the mail store asked to retry"), sdkgo.Receipt{})
		}
		return sdkgo.NewMutationRetry[SentMessage](mailFailure(sdkgo.FailureRateLimit, operationID, "the mail store asked to retry"), answer.retryAfter)
	default:
		return sdkgo.NewMutationRetry[SentMessage](mailFailure(sdkgo.FailureAvailability, operationID, "the mail store failed after the send"), answer.retryAfter)
	}
}
