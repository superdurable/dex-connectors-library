//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package customeronboarding_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	githubconnector "github.com/superdurable/dex-connectors-library/connectors/github"
	gmail "github.com/superdurable/dex-connectors-library/connectors/google/gmail"
	spreadsheet "github.com/superdurable/dex-connectors-library/connectors/google/spreadsheet"
	httpconnector "github.com/superdurable/dex-connectors-library/connectors/http"
	openai "github.com/superdurable/dex-connectors-library/connectors/openai"
	customeronboarding "github.com/superdurable/dex-connectors-library/examples/customer-onboarding"
	mockprovider "github.com/superdurable/dex-connectors-library/examples/customer-onboarding/internal/mockprovider"
	connector "github.com/superdurable/dex-connectors-library/sdk/go"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestQueryAndMutationRetriesUseRealDexIdentity(t *testing.T) {
	provider := mockprovider.StartWithOptions(mockprovider.Options{ProfileFailures: 1, MutationRateLimits: 1})
	defer provider.Close()
	_, connection, _ := connectorClient(t, provider)
	flow := customeronboarding.NewCustomerOnboardingConnectorFlow(connection)
	harness := newDexHarness(t, []dex.Flow{flow})
	harness.startWorker(t)

	output := runCustomerOnboarding(t, harness.client, flow, uniqueFlowID("retry"), customeronboarding.Input{
		CustomerID: "customer-1", Credits: 100,
	})
	require.Equal(t, httpconnector.MutationBranchSucceeded, output.Branch)
	require.Equal(t, 2, provider.ProfileRequests())
	require.Equal(t, 2, provider.MutationAttempts(output.CallID))
	require.Equal(t, 1, provider.MutationCount(output.CallID))
	mutation, ok := provider.Mutation(string(output.CallID))
	require.True(t, ok)
	require.Equal(t, 100, mutation.Credits)
}

func TestGitHubFactoryPersistsAuthenticatedProfileWithRealDex(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		response.Header().Set("X-OAuth-Scopes", "read:user, user:email")
		response.Header().Set("X-GitHub-Request-Id", "github-integration")
		switch request.URL.Path {
		case "/user":
			_, _ = response.Write([]byte(`{"id":42,"login":"octocat","name":"Mona Lisa"}`))
		case "/user/emails":
			_, _ = response.Write([]byte(`[{"email":"octocat@example.com","primary":true,"verified":true}]`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	reference := connector.ConnectionRef{Provider: "github", Name: "signup"}
	client, err := githubconnector.New(githubconnector.Config{BaseURL: server.URL}, connector.StaticCredentialProvider[githubconnector.Credentials]{
		reference: {AccessToken: connector.NewSecretString("one-use-token")},
	})
	require.NoError(t, err)
	connection, err := githubconnector.NewConnection(client, reference)
	require.NoError(t, err)
	flow := githubProfileFlow{connection: connection}
	harness := newDexHarness(t, []dex.Flow{flow})
	harness.startWorker(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	flowID := uniqueFlowID("github-profile")
	_, err = harness.client.StartFlow(ctx, flow, flowID, struct{}{}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status)
	var profile githubconnector.AuthenticatedProfile
	require.NoError(t, result.DecodeSingleOutput(&profile))
	require.Equal(t, "42", profile.Subject)
	require.Equal(t, "octocat@example.com", profile.VerifiedEmail)
}

func TestUnknownMutationUsesExplicitRecoveryAndCommittedReceipt(t *testing.T) {
	provider := mockprovider.Start()
	defer provider.Close()
	_, connection, _ := connectorClient(t, provider)
	flow := customeronboarding.NewCustomerOnboardingConnectorFlow(connection)
	harness := newDexHarness(t, []dex.Flow{flow})
	harness.startWorker(t)

	output := runCustomerOnboarding(t, harness.client, flow, uniqueFlowID("unknown"), customeronboarding.Input{
		CustomerID: "customer-2", Credits: 200, SimulateUnknown: true,
	})
	require.Equal(t, httpconnector.MutationBranchSucceeded, output.Branch)
	require.Equal(t, 1, provider.MutationCount(output.CallID))
	require.GreaterOrEqual(t, provider.RecoveryRequests(), 1)
}

func TestFlowContinuesAfterWorkerRestart(t *testing.T) {
	provider := mockprovider.StartWithOptions(mockprovider.Options{RecoveryFailures: 1})
	defer provider.Close()
	_, connection, _ := connectorClient(t, provider)
	flow := customeronboarding.NewCustomerOnboardingConnectorFlow(connection)
	harness := newDexHarness(t, []dex.Flow{flow})
	harness.startWorker(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	flowID := uniqueFlowID("restart")
	_, err := harness.client.StartFlow(ctx, flow, flowID, customeronboarding.Input{
		CustomerID: "customer-3", Credits: 300, SimulateUnknown: true,
	}, dex.StartFlowOptions{})
	require.NoError(t, err)
	require.Eventually(t, func() bool { return provider.RecoveryRequests() >= 1 }, 20*time.Second, 20*time.Millisecond)
	harness.stopWorker(t)
	harness.startWorker(t)

	result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status)
	var output customeronboarding.Output
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.Equal(t, httpconnector.MutationBranchSucceeded, output.Branch)
	require.Equal(t, 1, provider.MutationCount(output.CallID))
}

func TestFlowRPCCannotCallProvider(t *testing.T) {
	provider := mockprovider.Start()
	defer provider.Close()
	connection, _, httpClient := connectorClient(t, provider)
	flow := &rpcBoundaryFlow{query: httpClient.Query()}
	harness := newDexHarness(t, []dex.Flow{flow})
	harness.startWorker(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	flowID := uniqueFlowID("rpc-boundary")
	_, err := harness.client.StartFlow(ctx, flow, flowID, struct{}{}, dex.StartFlowOptions{})
	require.NoError(t, err)
	var output bool
	err = harness.client.InvokeRPC(ctx, flowID, flow.AttemptProviderQuery, connection, &output)
	require.NoError(t, err)
	require.False(t, output)
	require.Zero(t, provider.ProfileRequests())
}

func TestGoogleSheetsFactoryCommitsResultAndTransition(t *testing.T) {
	rows := [][]string{{"accountId", "name"}}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		switch request.Method {
		case http.MethodGet:
			_ = json.NewEncoder(response).Encode(map[string]any{"range": "Customers", "majorDimension": "ROWS", "values": rows})
		case http.MethodPost:
			var payload struct {
				Values [][]string `json:"values"`
			}
			require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
			rows = append(rows, payload.Values[0])
			_, _ = response.Write([]byte(`{"updates":{"updatedRange":"'Customers'!A2:B2"}}`))
		default:
			response.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()
	connectionRef := connector.ConnectionRef{Provider: "google", Name: "sheets"}
	client, err := spreadsheet.New(spreadsheet.Config{Endpoint: server.URL}, connector.StaticCredentialProvider[spreadsheet.Credentials]{
		connectionRef: {AccessToken: connector.NewSecretString("token")},
	})
	require.NoError(t, err)
	connection, err := spreadsheet.NewConnection(client, connectionRef)
	require.NoError(t, err)
	flow := &sheetsIntegrationFlow{connection: connection}
	harness := newDexHarness(t, []dex.Flow{flow})
	harness.startWorker(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	flowID := uniqueFlowID("google-sheets")
	_, err = harness.client.StartFlow(ctx, flow, flowID, "account-1", dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status)
	var output connector.MutationResult[spreadsheet.UpsertRowOutput]
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.Equal(t, spreadsheet.UpsertRowBranchUpserted, output.Branch)
	require.Equal(t, "inserted", output.Value.Action)
	require.Len(t, rows, 2)
}

func TestGmailFactoryRoutesUnknownWithoutResend(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		response.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	connectionRef := connector.ConnectionRef{Provider: "google", Name: "gmail"}
	client, err := gmail.New(gmail.Config{Endpoint: server.URL}, connector.StaticCredentialProvider[gmail.Credentials]{
		connectionRef: {AccessToken: connector.NewSecretString("token"), PrimaryEmail: "owner@example.com"},
	})
	require.NoError(t, err)
	connection, err := gmail.NewConnection(client, connectionRef)
	require.NoError(t, err)
	flow := &gmailIntegrationFlow{connection: connection}
	harness := newDexHarness(t, []dex.Flow{flow})
	harness.startWorker(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	flowID := uniqueFlowID("gmail-unknown")
	_, err = harness.client.StartFlow(ctx, flow, flowID, "customer@example.com", dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status)
	var branch connector.BranchID
	require.NoError(t, result.DecodeSingleOutput(&branch))
	require.Equal(t, gmail.SendMessageBranchUncertain, branch)
	require.Equal(t, int32(1), requests.Load())
}

var (
	githubProfileResult             = dex.DefineAttribute[connector.QueryResult[githubconnector.AuthenticatedProfile]]("github-profile-result")
	openAIProgress                  = dex.DefineStream[connector.ProgressUpdate]("openai-progress", 1<<20)
	openAIText                      = dex.DefineStream[string]("openai-text", 1<<20)
	openAIResult                    = dex.DefineAttribute[connector.MutationResult[openai.Response]]("openai-result")
	retryProgress                   = dex.DefineStream[connector.ProgressUpdate]("retry-progress", 1<<20)
	sheetsIntegrationResult         = dex.DefineAttribute[connector.MutationResult[spreadsheet.UpsertRowOutput]]("sheets-integration-result")
	gmailIntegrationResult          = dex.DefineAttribute[connector.MutationResult[gmail.SendMessageOutput]]("gmail-integration-result")
	integrationQueryBranchSucceeded = connector.BranchID("succeeded")
	integrationQueryBranchFailed    = connector.BranchID("failed")
	integrationQueryBranchDefect    = connector.BranchID("defect")
)

type githubProfileFlow struct {
	dex.FlowDefaults
	connection githubconnector.Connection
}

func (flow githubProfileFlow) GetSteps() []dex.StepDef {
	step := githubconnector.NewGetAuthenticatedProfileStep(githubconnector.GetAuthenticatedProfileStepConfig[struct{}]{
		StepType: "ReadGitHubSignupProfile",
		Presentation: connector.StepPresentation{
			GroupID: "signup", GroupLabel: "Signup", Explanation: "Read the authenticated GitHub signup profile.",
		},
		Connection: flow.connection,
		BuildInput: func(struct{}) (githubconnector.GetAuthenticatedProfileInput, error) {
			return githubconnector.GetAuthenticatedProfileInput{}, nil
		},
		ProfileLoaded:         connector.GoTo(githubProfileTerminalStep{}),
		VerifiedEmailRequired: connector.GoTo(githubProfileTerminalStep{}),
		InsufficientScope:     connector.GoTo(githubProfileTerminalStep{}),
		AuthorizationRevoked:  connector.GoTo(githubProfileTerminalStep{}),
		NotFound:              connector.GoTo(githubProfileTerminalStep{}),
		Failed:                connector.GoTo(githubProfileTerminalStep{}),
		Defect:                connector.GoTo(githubProfileTerminalStep{}),
		ResultAttribute:       &githubProfileResult,
	})
	return []dex.StepDef{dex.DefineStartStep(step), dex.DefineStep(githubProfileTerminalStep{})}
}

func (githubProfileFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{githubProfileResult}}
}

type githubProfileTerminalStep struct {
	dex.StepDefaultsNoWaitFor[githubconnector.GetAuthenticatedProfileStepOutput[struct{}]]
}

func (githubProfileTerminalStep) Execute(ctx dex.Context, output githubconnector.GetAuthenticatedProfileStepOutput[struct{}]) (*dex.StepDecision, error) {
	persisted, err := githubProfileResult.Get(ctx)
	if err != nil {
		return nil, err
	}
	if persisted.Receipt.CallID != output.Result.Receipt.CallID {
		return nil, fmt.Errorf("GitHub profile Result Attribute did not commit with its transition")
	}
	if output.Result.Branch != githubconnector.GetAuthenticatedProfileBranchProfileLoaded {
		return dex.ForceFail("GitHub profile query did not load the profile"), nil
	}
	return dex.GracefulComplete(output.Result.Value), nil
}

func integrationQueryDefinition(operationID string, progress bool) connector.QueryDefinition {
	return connector.QueryDefinition{
		Operation: connector.OperationRef{ConnectorID: "mock", OperationID: operationID},
		Branches: []connector.BranchDefinition{
			{ID: integrationQueryBranchSucceeded, Description: "succeeded"},
			{ID: integrationQueryBranchFailed, Description: "failed"},
			{ID: integrationQueryBranchDefect, Description: "defect"},
		},
		DefectBranch:    integrationQueryBranchDefect,
		ResultAttribute: connector.RequirementOptional,
		Progress:        connector.ProgressCapabilities{Structured: progress},
	}
}

func TestOpenAIStreamingWritesRealDexStreams(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "Bearer test-key", request.Header.Get("Authorization"))
		require.NotEmpty(t, request.Header.Get("Idempotency-Key"))
		response.Header().Set("Content-Type", "text/event-stream")
		response.Header().Set("X-Request-Id", "req_stream")
		_, _ = response.Write([]byte(strings.Join([]string{
			`data: {"type":"response.created","sequence_number":1,"response":{"id":"resp_stream","status":"in_progress"}}`,
			`data: {"type":"response.output_text.delta","sequence_number":2,"delta":"hel"}`,
			`data: {"type":"response.output_text.delta","sequence_number":3,"delta":"lo"}`,
			`data: {"type":"response.completed","sequence_number":4,"response":{"id":"resp_stream","model":"gpt-test","status":"completed","output":[{"content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":2,"output_tokens":1,"total_tokens":3}}}`,
		}, "\n\n") + "\n\n"))
	}))
	defer server.Close()
	connection := connector.ConnectionRef{Provider: "openai", Name: "default"}
	client, err := openai.New(openai.Config{Endpoint: server.URL}, connector.StaticCredentialProvider[openai.Credentials]{
		connection: {APIKey: connector.NewSecretString("test-key")},
	})
	require.NoError(t, err)
	typedConnection, err := openai.NewConnection(client, connection)
	require.NoError(t, err)
	flow := &openAIStreamingFlow{connection: typedConnection}
	harness := newDexHarness(t, []dex.Flow{flow})
	harness.startWorker(t)
	flowID := uniqueFlowID("openai-stream")

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	_, err = harness.client.StartFlow(ctx, flow, flowID, struct{}{}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status)
	var output openAIStreamingOutput
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.Equal(t, openai.CreateResponseBranchCompleted, output.Branch)
	require.Equal(t, "hello", output.Text)
	require.Equal(t, 3, output.TotalTokens)

	var progressPage dex.StreamMessagesPage[connector.ProgressUpdate]
	require.NoError(t, harness.client.ListStreamMessages(ctx, flowID, openAIProgress, 10, "", &progressPage))
	require.Len(t, progressPage.Messages, 1)
	update := progressPage.Messages[0].Value
	require.Equal(t, output.CallID, update.CallID)
	require.Equal(t, int32(1), update.Attempt)
	require.Equal(t, uint64(1), update.Sequence)
	require.Equal(t, "response.created", update.Phase)

	var textPage dex.StreamMessagesPage[string]
	require.NoError(t, harness.client.ListStreamMessages(ctx, flowID, openAIText, 10, "", &textPage))
	require.Len(t, textPage.Messages, 2)
	require.Equal(t, "hello", textPage.Messages[1].Value+textPage.Messages[0].Value)
}

func TestProgressStreamDistinguishesDexRetryAttempts(t *testing.T) {
	flow := retryProgressFlow{}
	harness := newDexHarness(t, []dex.Flow{flow})
	harness.startWorker(t)
	flowID := uniqueFlowID("retry-progress")

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	_, err := harness.client.StartFlow(ctx, flow, flowID, struct{}{}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status)
	var callID connector.CallID
	require.NoError(t, result.DecodeSingleOutput(&callID))

	var page dex.StreamMessagesPage[connector.ProgressUpdate]
	require.NoError(t, harness.client.ListStreamMessages(ctx, flowID, retryProgress, 10, "", &page))
	require.Len(t, page.Messages, 2)
	require.Equal(t, callID, page.Messages[0].Value.CallID)
	require.Equal(t, callID, page.Messages[1].Value.CallID)
	require.Equal(t, []int32{2, 1}, []int32{page.Messages[0].Value.Attempt, page.Messages[1].Value.Attempt})
	require.Equal(t, uint64(1), page.Messages[0].Value.Sequence)
	require.Equal(t, uint64(1), page.Messages[1].Value.Sequence)
}

type sheetsIntegrationFlow struct {
	dex.FlowDefaults
	connection spreadsheet.Connection
}

func (flow *sheetsIntegrationFlow) GetSteps() []dex.StepDef {
	step := spreadsheet.NewUpsertRowStep(spreadsheet.UpsertRowStepConfig[string]{
		StepType:     "IntegrationUpsertSheetRow",
		Presentation: connector.StepPresentation{GroupID: "google", GroupLabel: "Google", Explanation: "Upsert a customer row."},
		Connection:   flow.connection,
		BuildInput: func(accountID string) (spreadsheet.UpsertRowInput, error) {
			return spreadsheet.UpsertRowInput{SpreadsheetID: "sheet", SheetName: "Customers", KeyColumn: "accountId", KeyValue: accountID, Values: map[string]string{"name": "Ada"}}, nil
		},
		Upserted:        connector.GoTo(sheetsIntegrationFinishedStep{}),
		Conflict:        connector.GoTo(sheetsIntegrationFinishedStep{}),
		Rejected:        connector.GoTo(sheetsIntegrationFinishedStep{}),
		Uncertain:       connector.GoTo(sheetsIntegrationFinishedStep{}),
		Defect:          connector.GoTo(sheetsIntegrationFinishedStep{}),
		ResultAttribute: &sheetsIntegrationResult,
	})
	return []dex.StepDef{dex.DefineStartStep(step), dex.DefineStep(sheetsIntegrationFinishedStep{})}
}

func (*sheetsIntegrationFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{sheetsIntegrationResult}}
}

type sheetsIntegrationFinishedStep struct {
	dex.StepDefaultsNoWaitFor[spreadsheet.UpsertRowStepOutput[string]]
}

func (sheetsIntegrationFinishedStep) Execute(_ dex.Context, output spreadsheet.UpsertRowStepOutput[string]) (*dex.StepDecision, error) {
	return dex.GracefulComplete(output.Result), nil
}

type gmailIntegrationFlow struct {
	dex.FlowDefaults
	connection gmail.Connection
}

func (flow *gmailIntegrationFlow) GetSteps() []dex.StepDef {
	step := gmail.NewSendMessageStep(gmail.SendMessageStepConfig[string]{
		StepType:     "IntegrationSendGmailMessage",
		Presentation: connector.StepPresentation{GroupID: "google", GroupLabel: "Google", Explanation: "Send a customer message."},
		Connection:   flow.connection,
		BuildInput: func(recipient string) (gmail.SendMessageInput, error) {
			return gmail.SendMessageInput{To: []string{recipient}, Subject: "Progress", TextBody: "Keep going"}, nil
		},
		Sent:            connector.GoTo(gmailIntegrationFinishedStep{}),
		Rejected:        connector.GoTo(gmailIntegrationFinishedStep{}),
		Uncertain:       connector.GoTo(gmailIntegrationFinishedStep{}),
		Defect:          connector.GoTo(gmailIntegrationFinishedStep{}),
		ResultAttribute: &gmailIntegrationResult,
	})
	return []dex.StepDef{dex.DefineStartStep(step), dex.DefineStep(gmailIntegrationFinishedStep{})}
}

func (*gmailIntegrationFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{gmailIntegrationResult}}
}

type gmailIntegrationFinishedStep struct {
	dex.StepDefaultsNoWaitFor[gmail.SendMessageStepOutput[string]]
}

func (gmailIntegrationFinishedStep) Execute(_ dex.Context, output gmail.SendMessageStepOutput[string]) (*dex.StepDecision, error) {
	return dex.GracefulComplete(output.Result.Branch), nil
}

func TestQueryFailureDoesNotUseDexRetry(t *testing.T) {
	var calls atomic.Int32
	flow := queryFailureFlow{calls: &calls}
	harness := newDexHarness(t, []dex.Flow{flow})
	harness.startWorker(t)
	flowID := uniqueFlowID("query-failure")

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	_, err := harness.client.StartFlow(ctx, flow, flowID, struct{}{}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{})
	require.NoError(t, err)
	require.Equal(t, dex.FlowFailed, result.Status)
	require.Equal(t, int32(1), calls.Load())
}

func TestFactoryFailsWhenResultAttributeIsNotRegistered(t *testing.T) {
	flow := missingSchemaFlow{}
	harness := newDexHarness(t, []dex.Flow{flow})
	harness.startWorker(t)
	flowID := uniqueFlowID("missing-schema")

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	_, err := harness.client.StartFlow(ctx, flow, flowID, struct{}{}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{})
	require.NoError(t, err)
	require.Equal(t, dex.FlowFailed, result.Status)
}

func TestOpenAIEarlyEOFReconcilesByResponseID(t *testing.T) {
	var creates atomic.Int32
	var retrieves atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodPost && request.URL.Path == "/responses":
			creates.Add(1)
			response.Header().Set("Content-Type", "text/event-stream")
			_, _ = response.Write([]byte("data: {\"type\":\"response.created\",\"sequence_number\":1,\"response\":{\"id\":\"resp_recover\",\"status\":\"in_progress\"}}\n\n"))
		case request.Method == http.MethodGet && request.URL.Path == "/responses/resp_recover":
			retrieves.Add(1)
			_, _ = response.Write([]byte(`{"id":"resp_recover","model":"gpt-test","status":"completed","output":[{"content":[{"type":"output_text","text":"recovered"}]}],"usage":{"total_tokens":5}}`))
		default:
			response.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	connection := connector.ConnectionRef{Provider: "openai", Name: "default"}
	client, err := openai.New(openai.Config{Endpoint: server.URL}, connector.StaticCredentialProvider[openai.Credentials]{
		connection: {APIKey: connector.NewSecretString("test-key")},
	})
	require.NoError(t, err)
	flow := &openAIRecoveryFlow{client: client, connection: connection}
	harness := newDexHarness(t, []dex.Flow{flow})
	harness.startWorker(t)
	flowID := uniqueFlowID("openai-recovery")

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	_, err = harness.client.StartFlow(ctx, flow, flowID, struct{}{}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status)
	var output openai.Response
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.Equal(t, "resp_recover", output.ID)
	require.Equal(t, "recovered", output.OutputText)
	require.Equal(t, int32(1), creates.Load())
	require.Equal(t, int32(1), retrieves.Load())
}

type openAIStreamingOutput struct {
	CallID      connector.CallID   `json:"callId"`
	Branch      connector.BranchID `json:"branch"`
	Text        string             `json:"text"`
	TotalTokens int                `json:"totalTokens"`
}

type openAIStreamingFlow struct {
	dex.FlowDefaults
	connection openai.Connection
}

func (flow *openAIStreamingFlow) GetSteps() []dex.StepDef {
	step := openai.NewCreateResponseStep(openai.CreateResponseStepConfig[struct{}]{
		StepType: "OpenAIStreaming",
		Presentation: connector.StepPresentation{
			GroupID: "openai", GroupLabel: "OpenAI", Explanation: "Create a streaming OpenAI response.",
		},
		Connection: flow.connection,
		BuildInput: func(struct{}) (openai.CreateRequest, error) {
			return openai.CreateRequest{Model: "gpt-test", Input: "stream this"}, nil
		},
		Completed:       connector.GoTo(openAIStreamingSucceededStep{}),
		Failed:          connector.GoTo(openAIStreamingFailedStep{}),
		Uncertain:       connector.GoTo(openAIStreamingFailedStep{}),
		Defect:          connector.GoTo(openAIStreamingFailedStep{}),
		ResultAttribute: &openAIResult, ProgressStream: &openAIProgress, TextStream: &openAIText,
		TextOptions: []dex.BufferedTextStreamOption{dex.BufferedTextStreamMaxBytes(1)},
	})
	return []dex.StepDef{
		dex.DefineStartStep(step),
		dex.DefineStep(openAIStreamingSucceededStep{}),
		dex.DefineStep(openAIStreamingFailedStep{}),
	}
}

func (*openAIStreamingFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{
		Attributes: []dex.AttributeDef{openAIResult},
		Streams:    []dex.StreamDef{openAIProgress, openAIText},
	}
}

type openAIStreamingSucceededStep struct {
	dex.StepDefaultsNoWaitFor[connector.MutationStepOutput[struct{}, openai.Response]]
}

func (openAIStreamingSucceededStep) Execute(_ dex.Context, output connector.MutationStepOutput[struct{}, openai.Response]) (*dex.StepDecision, error) {
	result := output.Result
	return dex.GracefulComplete(openAIStreamingOutput{
		CallID: result.Receipt.CallID, Branch: result.Branch,
		Text: result.Value.OutputText, TotalTokens: result.Value.Usage.TotalTokens,
	}), nil
}

type openAIStreamingFailedStep struct {
	dex.StepDefaultsNoWaitFor[connector.MutationStepOutput[struct{}, openai.Response]]
}

func (openAIStreamingFailedStep) Execute(_ dex.Context, output connector.MutationStepOutput[struct{}, openai.Response]) (*dex.StepDecision, error) {
	return dex.ForceFail("OpenAI streaming ended on branch " + string(output.Result.Branch)), nil
}

type retryProgressFlow struct{ dex.FlowDefaults }

func (retryProgressFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{dex.DefineStartStep(retryProgressStep{})}
}

func (retryProgressFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Streams: []dex.StreamDef{retryProgress}}
}

type retryProgressStep struct {
	dex.StepDefaultsNoWaitFor[struct{}]
}

func (retryProgressStep) Execute(ctx dex.Context, _ struct{}) (*dex.StepDecision, error) {
	result, err := connector.RunQuery(
		ctx, retryProgressQuery{}, connector.ConnectionRef{Provider: "mock", Name: "default"}, struct{}{},
		connector.WithProgressStream(retryProgress),
	)
	if err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Receipt.CallID), nil
}

type retryProgressQuery struct{}

func (retryProgressQuery) Definition() connector.QueryDefinition {
	return integrationQueryDefinition("retryProgress", true)
}

func (retryProgressQuery) Invoke(call connector.Call, _ struct{}) connector.QueryAttempt[struct{}] {
	if err := call.ReportProgress(connector.Progress{Phase: "attempt"}); err != nil {
		return connector.NewQueryRetry[struct{}](connector.Failure{
			Kind: connector.FailureAvailability, Provider: "mock", Operation: "retryProgress", Message: "progress delivery failed",
		}, 0)
	}
	if call.Context.Attempt() == 1 {
		return connector.NewQueryRetry[struct{}](connector.Failure{
			Kind: connector.FailureAvailability, Provider: "mock", Operation: "retryProgress", Message: "retry fixture",
		}, 10*time.Millisecond)
	}
	return connector.NewQueryBranch(integrationQueryBranchSucceeded, struct{}{}, nil, connector.Receipt{})
}

type queryFailureFlow struct {
	dex.FlowDefaults
	calls *atomic.Int32
}

var missingSchemaResult = dex.DefineAttribute[connector.QueryResult[struct{}]]("missing-schema-result")

type missingSchemaFlow struct{ dex.FlowDefaults }

func (missingSchemaFlow) GetSteps() []dex.StepDef {
	step := connector.MustNewQueryStep(connector.QueryStepConfig[struct{}, struct{}, struct{}]{
		StepType: "MissingSchemaQuery",
		Presentation: connector.StepPresentation{
			GroupID: "test", GroupLabel: "Test", Explanation: "Verify missing schema registration fails.",
		},
		Operation: missingSchemaQuery{}, Connection: connector.ConnectionRef{Provider: "mock", Name: "default"},
		BuildInput: func(struct{}) (struct{}, error) { return struct{}{}, nil },
		Branches: []connector.BranchTarget[connector.QueryStepOutput[struct{}, struct{}]]{
			connector.GoToBranch(integrationQueryBranchSucceeded, missingSchemaTerminalStep{}),
			connector.GoToBranch(integrationQueryBranchFailed, missingSchemaTerminalStep{}),
			connector.GoToBranch(integrationQueryBranchDefect, missingSchemaTerminalStep{}),
		},
		ResultAttribute:     &missingSchemaResult,
		StepOptionsOverride: &dex.StepOptions{ExecuteRetry: &dex.RetryPolicy{MaximumAttempts: 1}},
	})
	return []dex.StepDef{dex.DefineStartStep(step), dex.DefineStep(missingSchemaTerminalStep{})}
}

func (missingSchemaFlow) GetPersistenceSchema() dex.PersistenceSchema { return dex.PersistenceSchema{} }

type missingSchemaQuery struct{}

func (missingSchemaQuery) Definition() connector.QueryDefinition {
	definition := integrationQueryDefinition("missingSchema", false)
	definition.ResultAttribute = connector.RequirementRequired
	return definition
}

func (missingSchemaQuery) Invoke(connector.Call, struct{}) connector.QueryAttempt[struct{}] {
	return connector.NewQueryBranch(integrationQueryBranchSucceeded, struct{}{}, nil, connector.Receipt{})
}

type missingSchemaTerminalStep struct {
	dex.StepDefaultsNoWaitFor[connector.QueryStepOutput[struct{}, struct{}]]
}

func (missingSchemaTerminalStep) Execute(dex.Context, connector.QueryStepOutput[struct{}, struct{}]) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

func (flow queryFailureFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{dex.DefineStartStep(queryFailureStep{calls: flow.calls})}
}

func (queryFailureFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{}
}

type queryFailureStep struct {
	dex.StepDefaultsNoWaitFor[struct{}]
	calls *atomic.Int32
}

func (step queryFailureStep) Execute(ctx dex.Context, _ struct{}) (*dex.StepDecision, error) {
	result, err := connector.RunQuery(
		ctx, terminalFailureQuery{calls: step.calls},
		connector.ConnectionRef{Provider: "mock", Name: "default"}, struct{}{},
	)
	if err != nil {
		return nil, err
	}
	if result.Branch == integrationQueryBranchFailed {
		return dex.ForceFail("confirmed query failure"), nil
	}
	return dex.GracefulComplete(struct{}{}), nil
}

type terminalFailureQuery struct{ calls *atomic.Int32 }

func (terminalFailureQuery) Definition() connector.QueryDefinition {
	return integrationQueryDefinition("terminalFailure", false)
}

func (operation terminalFailureQuery) Invoke(connector.Call, struct{}) connector.QueryAttempt[struct{}] {
	operation.calls.Add(1)
	failure := connector.Failure{
		Kind: connector.FailureNotFound, Provider: "mock", Operation: "terminalFailure", Message: "object was not found",
	}
	return connector.NewQueryBranch(integrationQueryBranchFailed, struct{}{}, &failure, connector.Receipt{})
}

type openAIRecoveryFlow struct {
	dex.FlowDefaults
	client     *openai.Client
	connection connector.ConnectionRef
}

func (flow *openAIRecoveryFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(openAIRecoveryStartStep{client: flow.client, connection: flow.connection}),
		dex.DefineStep(openAIRetrieveStep{client: flow.client, connection: flow.connection}),
	}
}

func (*openAIRecoveryFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Streams: []dex.StreamDef{openAIProgress}}
}

type openAIRecoveryStartStep struct {
	dex.StepDefaultsNoWaitFor[struct{}]
	client     *openai.Client
	connection connector.ConnectionRef
}

func (step openAIRecoveryStartStep) Execute(ctx dex.Context, _ struct{}) (*dex.StepDecision, error) {
	result, err := connector.RunMutation(
		ctx, step.client.CreateResponse(), step.connection,
		openai.CreateRequest{Model: "gpt-test", Input: "recover this"},
		connector.WithProgressStream(openAIProgress),
	)
	if err != nil {
		return nil, err
	}
	if result.Branch != openai.CreateResponseBranchUncertain || result.Value.ID == "" {
		return dex.ForceFail("expected a recoverable unknown OpenAI response"), nil
	}
	return dex.GoTo(openAIRetrieveStep{}, openai.RetrieveRequest{ResponseID: result.Value.ID}), nil
}

type openAIRetrieveStep struct {
	dex.StepDefaultsNoWaitFor[openai.RetrieveRequest]
	client     *openai.Client
	connection connector.ConnectionRef
}

func (step openAIRetrieveStep) Execute(ctx dex.Context, input openai.RetrieveRequest) (*dex.StepDecision, error) {
	result, err := connector.RunQuery(ctx, step.client.RetrieveResponse(), step.connection, input)
	if err != nil {
		return nil, err
	}
	if result.Branch == openai.RetrieveResponseBranchFailed {
		return dex.ForceFail("OpenAI response reconciliation failed"), nil
	}
	return dex.GracefulComplete(result.Value), nil
}

type rpcBoundaryFlow struct {
	dex.FlowDefaults
	query httpconnector.QueryOperation
}

func (flow *rpcBoundaryFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{dex.DefineStartStep(rpcBoundaryStartStep{})}
}

func (flow *rpcBoundaryFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.AttemptProviderQuery, nil)}
}

func (*rpcBoundaryFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{}
}

func (flow *rpcBoundaryFlow) AttemptProviderQuery(ctx dex.Context, connection connector.ConnectionRef) (*dex.RPCResult[bool], error) {
	result, err := connector.RunQuery(ctx, flow.query, connection, httpconnector.Request{
		Method: http.MethodGet, Path: "/profiles/customer-rpc",
	})
	return &dex.RPCResult[bool]{Output: err == nil && result.Branch == httpconnector.QueryBranchSucceeded}, err
}

type rpcBoundaryStartStep struct {
	dex.StepDefaultsNoWaitFor[struct{}]
}

func (rpcBoundaryStartStep) Execute(dex.Context, struct{}) (*dex.StepDecision, error) {
	return dex.DeadEnd(), nil
}

type dexHarness struct {
	registry      *dex.Registry
	cache         *blobcache.Cache
	serverAddress string
	workerAddress string
	worker        *dex.Worker
	workerResult  chan error
	client        *dex.Client
}

func newDexHarness(t *testing.T, flows []dex.Flow) *dexHarness {
	t.Helper()
	registry, err := dex.NewRegistry(flows)
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	address := net.JoinHostPort("127.0.0.1", availablePort(t))
	harness := &dexHarness{
		registry: registry, cache: cache,
		serverAddress: environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801"),
		workerAddress: address,
	}
	harness.client, err = dex.NewClient(registry, cache, dex.ClientOptions{
		FlowServiceAddress: harness.serverAddress,
		WorkerTarget:       &dex.WorkerTarget{Address: address},
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		if harness.worker != nil {
			harness.stopWorker(t)
		}
		require.NoError(t, errors.Join(harness.client.Close(), harness.cache.Close()))
	})
	return harness
}

func (harness *dexHarness) startWorker(t *testing.T) {
	t.Helper()
	require.Nil(t, harness.worker)
	worker, err := dex.NewWorker(harness.registry, harness.cache, dex.WorkerOptions{
		BindAddress: harness.workerAddress, FlowServiceAddress: harness.serverAddress,
		WorkerTarget: dex.WorkerTarget{Address: harness.workerAddress},
	})
	require.NoError(t, err)
	harness.worker = worker
	harness.workerResult = make(chan error, 1)
	go func() { harness.workerResult <- worker.Start() }()
}

func (harness *dexHarness) stopWorker(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, errors.Join(harness.worker.Stop(ctx), <-harness.workerResult))
	harness.worker = nil
	harness.workerResult = nil
}

func connectorClient(t *testing.T, provider *mockprovider.Provider) (connector.ConnectionRef, httpconnector.Connection, *httpconnector.Client) {
	t.Helper()
	connection := connector.ConnectionRef{Provider: "mock", Name: "default"}
	client, err := httpconnector.New(httpconnector.Config{
		BaseURL: provider.URL(), CredentialHeaders: map[string]string{"api_key": "X-Mock-Api-Key"},
	}, connector.StaticCredentialProvider[httpconnector.Credentials]{
		connection: {APIKey: connector.NewSecretString("test-key")},
	})
	require.NoError(t, err)
	typedConnection, err := httpconnector.NewConnection(client, connection)
	require.NoError(t, err)
	return connection, typedConnection, client
}

func runCustomerOnboarding(
	t *testing.T,
	client *dex.Client,
	flow *customeronboarding.CustomerOnboardingConnectorFlow,
	flowID string,
	input customeronboarding.Input,
) customeronboarding.Output {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	_, err := client.StartFlow(ctx, flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status)
	var output customeronboarding.Output
	require.NoError(t, result.DecodeSingleOutput(&output))
	return output
}

func uniqueFlowID(prefix string) string {
	return fmt.Sprintf("connector-%s-%d", prefix, time.Now().UnixNano())
}

func availablePort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	return strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
}

func environmentOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
