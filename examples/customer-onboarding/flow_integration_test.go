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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	githubconnector "github.com/superdurable/dex-connectors-library/connectors/github"
	gmail "github.com/superdurable/dex-connectors-library/connectors/google/gmail"
	spreadsheet "github.com/superdurable/dex-connectors-library/connectors/google/spreadsheet"
	httpconnector "github.com/superdurable/dex-connectors-library/connectors/http"
	linkedinconnector "github.com/superdurable/dex-connectors-library/connectors/linkedin"
	openai "github.com/superdurable/dex-connectors-library/connectors/openai"
	customeronboarding "github.com/superdurable/dex-connectors-library/examples/customer-onboarding"
	mockprovider "github.com/superdurable/dex-connectors-library/examples/customer-onboarding/internal/mockprovider"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
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
	reference := sdkgo.ConnectionRef{Provider: "github", Name: "signup"}
	client, err := githubconnector.New(githubconnector.Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[githubconnector.Credentials]{
		reference: {AccessToken: sdkgo.NewSecretString("one-use-token")},
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

func TestGitHubLocalConnectionsUseNamedCredentialsAndReloadReplacements(t *testing.T) {
	var authorizationHeadersMu sync.Mutex
	authorizationHeaders := make([]string, 0, 6)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		authorizationHeadersMu.Lock()
		authorizationHeaders = append(authorizationHeaders, request.Header.Get("Authorization"))
		authorizationHeadersMu.Unlock()
		response.Header().Set("Content-Type", "application/json")
		response.Header().Set("X-OAuth-Scopes", "read:user, user:email")
		switch request.URL.Path {
		case "/user":
			_, _ = response.Write([]byte(`{"id":42,"login":"octocat"}`))
		case "/user/emails":
			_, _ = response.Write([]byte(`[{"email":"octocat@example.com","primary":true,"verified":true}]`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "connections.json")
	writeLocalConnections(t, path,
		localConnectionRecord("github", "github", "signup-a", map[string]any{"baseUrl": server.URL}, map[string]any{"access_token": "token-a"}, time.Now().Add(time.Hour)),
		localConnectionRecord("github", "github", "signup-b", map[string]any{"baseUrl": server.URL}, map[string]any{"access_token": "token-b"}, time.Now().Add(time.Hour)),
	)
	store, err := localconfig.LoadFile(path)
	require.NoError(t, err)
	connectionA, err := githubconnector.NewLocalConnection(store, "signup-a")
	require.NoError(t, err)
	flowA := githubProfileFlow{connection: connectionA, connectionName: "signup-a"}
	harnessA := newDexHarness(t, []dex.Flow{flowA})
	harnessA.startWorker(t)
	runGitHubProfile(t, harnessA.client, flowA, uniqueFlowID("github-local-a"))

	connectionB, err := githubconnector.NewLocalConnection(store, "signup-b")
	require.NoError(t, err)
	flowB := githubProfileFlow{connection: connectionB, connectionName: "signup-b"}
	harnessB := newDexHarness(t, []dex.Flow{flowB})
	harnessB.startWorker(t)
	runGitHubProfile(t, harnessB.client, flowB, uniqueFlowID("github-local-b"))

	writeLocalConnections(t, path,
		localConnectionRecord("github", "github", "signup-a", map[string]any{"baseUrl": "http://127.0.0.1:1"}, map[string]any{"access_token": "token-a-replaced"}, time.Now().Add(time.Hour)),
		localConnectionRecord("github", "github", "signup-b", map[string]any{"baseUrl": server.URL}, map[string]any{"access_token": "token-b"}, time.Now().Add(time.Hour)),
	)
	runGitHubProfile(t, harnessA.client, flowA, uniqueFlowID("github-local-a-replaced"))

	authorizationHeadersMu.Lock()
	defer authorizationHeadersMu.Unlock()
	require.Equal(t, []string{
		"Bearer token-a", "Bearer token-a", "Bearer token-b", "Bearer token-b",
		"Bearer token-a-replaced", "Bearer token-a-replaced",
	}, authorizationHeaders)
}

func TestLinkedInFactoryPersistsAuthenticatedProfileWithRealDex(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/v2/userinfo", request.URL.Path)
		require.Equal(t, "Bearer one-use-token", request.Header.Get("Authorization"))
		response.Header().Set("Content-Type", "application/json")
		response.Header().Set("X-LI-UUID", "linkedin-integration")
		_, _ = response.Write([]byte(`{"sub":"member-42","name":"Ada Lovelace","email":"ada@example.com","email_verified":true}`))
	}))
	defer server.Close()
	reference := sdkgo.ConnectionRef{Provider: "linkedin", Name: "signup"}
	client, err := linkedinconnector.New(linkedinconnector.Config{UserInfoURL: server.URL + "/v2/userinfo"}, sdkgo.StaticCredentialProvider[linkedinconnector.Credentials]{
		reference: {AccessToken: sdkgo.NewSecretString("one-use-token")},
	})
	require.NoError(t, err)
	connection, err := linkedinconnector.NewConnection(client, reference)
	require.NoError(t, err)
	flow := linkedinProfileFlow{connection: connection}
	harness := newDexHarness(t, []dex.Flow{flow})
	harness.startWorker(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	flowID := uniqueFlowID("linkedin-profile")
	_, err = harness.client.StartFlow(ctx, flow, flowID, struct{}{}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status)
	var profile linkedinconnector.AuthenticatedProfile
	require.NoError(t, result.DecodeSingleOutput(&profile))
	require.Equal(t, "member-42", profile.Subject)
	require.Equal(t, "ada@example.com", profile.VerifiedEmail)
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
	connectionRef := sdkgo.ConnectionRef{Provider: "google", Name: "sheets"}
	client, err := spreadsheet.New(spreadsheet.Config{Endpoint: server.URL}, sdkgo.StaticCredentialProvider[spreadsheet.Credentials]{
		connectionRef: {AccessToken: sdkgo.NewSecretString("token")},
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
	var output sdkgo.MutationResult[spreadsheet.UpsertRowOutput]
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
	connectionRef := sdkgo.ConnectionRef{Provider: "google", Name: "gmail"}
	client, err := gmail.New(gmail.Config{Endpoint: server.URL}, sdkgo.StaticCredentialProvider[gmail.Credentials]{
		connectionRef: {AccessToken: sdkgo.NewSecretString("token"), PrimaryEmail: "owner@example.com"},
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
	var branch sdkgo.BranchID
	require.NoError(t, result.DecodeSingleOutput(&branch))
	require.Equal(t, gmail.SendMessageBranchUncertain, branch)
	require.Equal(t, int32(1), requests.Load())
}

func TestGmailLocalConnectionRejectsExpiredCredentialsBeforeProviderCall(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		response.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "connections.json")
	writeLocalConnections(t, path, localConnectionRecord(
		"gmail", "google", "gmail", map[string]any{"endpoint": server.URL},
		map[string]any{"access_token": "expired", "primary_email": "owner@example.com"}, time.Now().Add(-time.Minute),
	))
	store, err := localconfig.LoadFile(path)
	require.NoError(t, err)
	connection, err := gmail.NewLocalConnection(store, "gmail")
	require.NoError(t, err)
	flow := &gmailIntegrationFlow{connection: connection, connectionName: "gmail"}
	harness := newDexHarness(t, []dex.Flow{flow})
	harness.startWorker(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	flowID := uniqueFlowID("gmail-expired-local")
	_, err = harness.client.StartFlow(ctx, flow, flowID, "customer@example.com", dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	var branch sdkgo.BranchID
	require.NoError(t, result.DecodeSingleOutput(&branch))
	require.Equal(t, gmail.SendMessageBranchRejected, branch)
	require.Zero(t, requests.Load())
}

var (
	githubProfileResult             = dex.DefineAttribute[sdkgo.QueryResult[githubconnector.AuthenticatedProfile]]("github-profile-result")
	linkedinProfileResult           = dex.DefineAttribute[sdkgo.QueryResult[linkedinconnector.AuthenticatedProfile]]("linkedin-profile-result")
	openAIProgress                  = dex.DefineStream[sdkgo.ProgressUpdate]("openai-progress", 1<<20)
	openAIText                      = dex.DefineStream[string]("openai-text", 1<<20)
	openAIResult                    = dex.DefineAttribute[sdkgo.MutationResult[openai.Response]]("openai-result")
	retryProgress                   = dex.DefineStream[sdkgo.ProgressUpdate]("retry-progress", 1<<20)
	sheetsIntegrationResult         = dex.DefineAttribute[sdkgo.MutationResult[spreadsheet.UpsertRowOutput]]("sheets-integration-result")
	gmailIntegrationResult          = dex.DefineAttribute[sdkgo.MutationResult[gmail.SendMessageOutput]]("gmail-integration-result")
	integrationQueryBranchSucceeded = sdkgo.BranchID("succeeded")
	integrationQueryBranchFailed    = sdkgo.BranchID("failed")
	integrationQueryBranchDefect    = sdkgo.BranchID("defect")
)

type githubProfileFlow struct {
	dex.FlowDefaults
	connection     githubconnector.Connection
	connectionName string
}

func (flow githubProfileFlow) GetSteps() []dex.StepDef {
	step := githubconnector.NewGetAuthenticatedProfileStep(githubconnector.GetAuthenticatedProfileStepConfig[struct{}]{
		StepType:       "ReadGitHubSignupProfile",
		ConnectionName: flow.connectionName,
		Presentation: sdkgo.StepPresentation{
			GroupID: "signup", GroupLabel: "Signup", Explanation: "Read the authenticated GitHub signup profile.",
		},
		Connection: flow.connection,
		BuildInput: func(struct{}) (githubconnector.GetAuthenticatedProfileInput, error) {
			return githubconnector.GetAuthenticatedProfileInput{}, nil
		},
		ProfileLoaded:         sdkgo.GoTo(githubProfileTerminalStep{}),
		VerifiedEmailRequired: sdkgo.GoTo(githubProfileTerminalStep{}),
		InsufficientScope:     sdkgo.GoTo(githubProfileTerminalStep{}),
		AuthorizationRevoked:  sdkgo.GoTo(githubProfileTerminalStep{}),
		NotFound:              sdkgo.GoTo(githubProfileTerminalStep{}),
		Failed:                sdkgo.GoTo(githubProfileTerminalStep{}),
		Defect:                sdkgo.GoTo(githubProfileTerminalStep{}),
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

type linkedinProfileFlow struct {
	dex.FlowDefaults
	connection linkedinconnector.Connection
}

func (flow linkedinProfileFlow) GetSteps() []dex.StepDef {
	step := linkedinconnector.NewGetAuthenticatedProfileStep(linkedinconnector.GetAuthenticatedProfileStepConfig[struct{}]{
		StepType: "ReadLinkedInSignupProfile",
		Presentation: sdkgo.StepPresentation{
			GroupID: "signup", GroupLabel: "Signup", Explanation: "Read the authenticated LinkedIn OIDC signup profile.",
		},
		Connection: flow.connection,
		BuildInput: func(struct{}) (linkedinconnector.GetAuthenticatedProfileInput, error) {
			return linkedinconnector.GetAuthenticatedProfileInput{}, nil
		},
		ProfileLoaded:         sdkgo.GoTo(linkedinProfileTerminalStep{}),
		VerifiedEmailRequired: sdkgo.GoTo(linkedinProfileTerminalStep{}),
		InsufficientScope:     sdkgo.GoTo(linkedinProfileTerminalStep{}),
		AuthorizationRevoked:  sdkgo.GoTo(linkedinProfileTerminalStep{}),
		NotFound:              sdkgo.GoTo(linkedinProfileTerminalStep{}),
		Failed:                sdkgo.GoTo(linkedinProfileTerminalStep{}),
		Defect:                sdkgo.GoTo(linkedinProfileTerminalStep{}),
		ResultAttribute:       &linkedinProfileResult,
	})
	return []dex.StepDef{dex.DefineStartStep(step), dex.DefineStep(linkedinProfileTerminalStep{})}
}

func (linkedinProfileFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{linkedinProfileResult}}
}

type linkedinProfileTerminalStep struct {
	dex.StepDefaultsNoWaitFor[linkedinconnector.GetAuthenticatedProfileStepOutput[struct{}]]
}

func (linkedinProfileTerminalStep) Execute(ctx dex.Context, output linkedinconnector.GetAuthenticatedProfileStepOutput[struct{}]) (*dex.StepDecision, error) {
	persisted, err := linkedinProfileResult.Get(ctx)
	if err != nil {
		return nil, err
	}
	if persisted.Receipt.CallID != output.Result.Receipt.CallID {
		return nil, fmt.Errorf("LinkedIn profile Result Attribute did not commit with its transition")
	}
	if output.Result.Branch != linkedinconnector.GetAuthenticatedProfileBranchProfileLoaded {
		return dex.ForceFail("LinkedIn profile query did not load the profile"), nil
	}
	return dex.GracefulComplete(output.Result.Value), nil
}

func integrationQueryDefinition(operationID string, progress bool) sdkgo.QueryDefinition {
	return sdkgo.QueryDefinition{
		Operation: sdkgo.OperationRef{ConnectorID: "mock", OperationID: operationID},
		Branches: []sdkgo.BranchDefinition{
			{ID: integrationQueryBranchSucceeded, Description: "succeeded"},
			{ID: integrationQueryBranchFailed, Description: "failed"},
			{ID: integrationQueryBranchDefect, Description: "defect"},
		},
		DefectBranch:    integrationQueryBranchDefect,
		ResultAttribute: sdkgo.RequirementOptional,
		Progress:        sdkgo.ProgressCapabilities{Structured: progress},
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
	connection := sdkgo.ConnectionRef{Provider: "openai", Name: "default"}
	client, err := openai.New(openai.Config{Endpoint: server.URL}, sdkgo.StaticCredentialProvider[openai.Credentials]{
		connection: {APIKey: sdkgo.NewSecretString("test-key")},
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

	var progressPage dex.StreamMessagesPage[sdkgo.ProgressUpdate]
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
	var callID sdkgo.CallID
	require.NoError(t, result.DecodeSingleOutput(&callID))

	var page dex.StreamMessagesPage[sdkgo.ProgressUpdate]
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
		Presentation: sdkgo.StepPresentation{GroupID: "google", GroupLabel: "Google", Explanation: "Upsert a customer row."},
		Connection:   flow.connection,
		BuildInput: func(accountID string) (spreadsheet.UpsertRowInput, error) {
			return spreadsheet.UpsertRowInput{SpreadsheetID: "sheet", SheetName: "Customers", KeyColumn: "accountId", KeyValue: accountID, Values: map[string]string{"name": "Ada"}}, nil
		},
		Upserted:        sdkgo.GoTo(sheetsIntegrationFinishedStep{}),
		Conflict:        sdkgo.GoTo(sheetsIntegrationFinishedStep{}),
		Rejected:        sdkgo.GoTo(sheetsIntegrationFinishedStep{}),
		Uncertain:       sdkgo.GoTo(sheetsIntegrationFinishedStep{}),
		Defect:          sdkgo.GoTo(sheetsIntegrationFinishedStep{}),
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
	connection     gmail.Connection
	connectionName string
}

func (flow *gmailIntegrationFlow) GetSteps() []dex.StepDef {
	step := gmail.NewSendMessageStep(gmail.SendMessageStepConfig[string]{
		StepType:       "IntegrationSendGmailMessage",
		ConnectionName: flow.connectionName,
		Presentation:   sdkgo.StepPresentation{GroupID: "google", GroupLabel: "Google", Explanation: "Send a customer message."},
		Connection:     flow.connection,
		BuildInput: func(recipient string) (gmail.SendMessageInput, error) {
			return gmail.SendMessageInput{To: []string{recipient}, Subject: "Progress", TextBody: "Keep going"}, nil
		},
		Sent:            sdkgo.GoTo(gmailIntegrationFinishedStep{}),
		Rejected:        sdkgo.GoTo(gmailIntegrationFinishedStep{}),
		Uncertain:       sdkgo.GoTo(gmailIntegrationFinishedStep{}),
		Defect:          sdkgo.GoTo(gmailIntegrationFinishedStep{}),
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
	connection := sdkgo.ConnectionRef{Provider: "openai", Name: "default"}
	client, err := openai.New(openai.Config{Endpoint: server.URL}, sdkgo.StaticCredentialProvider[openai.Credentials]{
		connection: {APIKey: sdkgo.NewSecretString("test-key")},
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
	CallID      sdkgo.CallID   `json:"callId"`
	Branch      sdkgo.BranchID `json:"branch"`
	Text        string         `json:"text"`
	TotalTokens int            `json:"totalTokens"`
}

type openAIStreamingFlow struct {
	dex.FlowDefaults
	connection openai.Connection
}

func (flow *openAIStreamingFlow) GetSteps() []dex.StepDef {
	step := openai.NewCreateResponseStep(openai.CreateResponseStepConfig[struct{}]{
		StepType: "OpenAIStreaming",
		Presentation: sdkgo.StepPresentation{
			GroupID: "openai", GroupLabel: "OpenAI", Explanation: "Create a streaming OpenAI response.",
		},
		Connection: flow.connection,
		BuildInput: func(struct{}) (openai.CreateRequest, error) {
			return openai.CreateRequest{Model: "gpt-test", Input: "stream this"}, nil
		},
		Completed:       sdkgo.GoTo(openAIStreamingSucceededStep{}),
		Failed:          sdkgo.GoTo(openAIStreamingFailedStep{}),
		Uncertain:       sdkgo.GoTo(openAIStreamingFailedStep{}),
		Defect:          sdkgo.GoTo(openAIStreamingFailedStep{}),
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
	dex.StepDefaultsNoWaitFor[sdkgo.MutationStepOutput[struct{}, openai.Response]]
}

func (openAIStreamingSucceededStep) Execute(_ dex.Context, output sdkgo.MutationStepOutput[struct{}, openai.Response]) (*dex.StepDecision, error) {
	result := output.Result
	return dex.GracefulComplete(openAIStreamingOutput{
		CallID: result.Receipt.CallID, Branch: result.Branch,
		Text: result.Value.OutputText, TotalTokens: result.Value.Usage.TotalTokens,
	}), nil
}

type openAIStreamingFailedStep struct {
	dex.StepDefaultsNoWaitFor[sdkgo.MutationStepOutput[struct{}, openai.Response]]
}

func (openAIStreamingFailedStep) Execute(_ dex.Context, output sdkgo.MutationStepOutput[struct{}, openai.Response]) (*dex.StepDecision, error) {
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
	result, err := sdkgo.RunQuery(
		ctx, retryProgressQuery{}, sdkgo.ConnectionRef{Provider: "mock", Name: "default"}, struct{}{},
		sdkgo.WithProgressStream(retryProgress),
	)
	if err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Receipt.CallID), nil
}

type retryProgressQuery struct{}

func (retryProgressQuery) Definition() sdkgo.QueryDefinition {
	return integrationQueryDefinition("retryProgress", true)
}

func (retryProgressQuery) Invoke(call sdkgo.Call, _ struct{}) sdkgo.QueryAttempt[struct{}] {
	if err := call.ReportProgress(sdkgo.Progress{Phase: "attempt"}); err != nil {
		return sdkgo.NewQueryRetry[struct{}](sdkgo.Failure{
			Kind: sdkgo.FailureAvailability, Provider: "mock", Operation: "retryProgress", Message: "progress delivery failed",
		}, 0)
	}
	if call.Context.Attempt() == 1 {
		return sdkgo.NewQueryRetry[struct{}](sdkgo.Failure{
			Kind: sdkgo.FailureAvailability, Provider: "mock", Operation: "retryProgress", Message: "retry fixture",
		}, 10*time.Millisecond)
	}
	return sdkgo.NewQueryBranch(integrationQueryBranchSucceeded, struct{}{}, nil, sdkgo.Receipt{})
}

type queryFailureFlow struct {
	dex.FlowDefaults
	calls *atomic.Int32
}

var missingSchemaResult = dex.DefineAttribute[sdkgo.QueryResult[struct{}]]("missing-schema-result")

type missingSchemaFlow struct{ dex.FlowDefaults }

func (missingSchemaFlow) GetSteps() []dex.StepDef {
	step := sdkgo.MustNewQueryStep(sdkgo.QueryStepConfig[struct{}, struct{}, struct{}]{
		StepType: "MissingSchemaQuery",
		Presentation: sdkgo.StepPresentation{
			GroupID: "test", GroupLabel: "Test", Explanation: "Verify missing schema registration fails.",
		},
		Operation: missingSchemaQuery{}, Connection: sdkgo.ConnectionRef{Provider: "mock", Name: "default"},
		BuildInput: func(struct{}) (struct{}, error) { return struct{}{}, nil },
		Branches: []sdkgo.BranchTarget[sdkgo.QueryStepOutput[struct{}, struct{}]]{
			sdkgo.GoToBranch(integrationQueryBranchSucceeded, missingSchemaTerminalStep{}),
			sdkgo.GoToBranch(integrationQueryBranchFailed, missingSchemaTerminalStep{}),
			sdkgo.GoToBranch(integrationQueryBranchDefect, missingSchemaTerminalStep{}),
		},
		ResultAttribute:     &missingSchemaResult,
		StepOptionsOverride: &dex.StepOptions{ExecuteRetry: &dex.RetryPolicy{MaximumAttempts: 1}},
	})
	return []dex.StepDef{dex.DefineStartStep(step), dex.DefineStep(missingSchemaTerminalStep{})}
}

func (missingSchemaFlow) GetPersistenceSchema() dex.PersistenceSchema { return dex.PersistenceSchema{} }

type missingSchemaQuery struct{}

func (missingSchemaQuery) Definition() sdkgo.QueryDefinition {
	definition := integrationQueryDefinition("missingSchema", false)
	definition.ResultAttribute = sdkgo.RequirementRequired
	return definition
}

func (missingSchemaQuery) Invoke(sdkgo.Call, struct{}) sdkgo.QueryAttempt[struct{}] {
	return sdkgo.NewQueryBranch(integrationQueryBranchSucceeded, struct{}{}, nil, sdkgo.Receipt{})
}

type missingSchemaTerminalStep struct {
	dex.StepDefaultsNoWaitFor[sdkgo.QueryStepOutput[struct{}, struct{}]]
}

func (missingSchemaTerminalStep) Execute(dex.Context, sdkgo.QueryStepOutput[struct{}, struct{}]) (*dex.StepDecision, error) {
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
	result, err := sdkgo.RunQuery(
		ctx, terminalFailureQuery{calls: step.calls},
		sdkgo.ConnectionRef{Provider: "mock", Name: "default"}, struct{}{},
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

func (terminalFailureQuery) Definition() sdkgo.QueryDefinition {
	return integrationQueryDefinition("terminalFailure", false)
}

func (operation terminalFailureQuery) Invoke(sdkgo.Call, struct{}) sdkgo.QueryAttempt[struct{}] {
	operation.calls.Add(1)
	failure := sdkgo.Failure{
		Kind: sdkgo.FailureNotFound, Provider: "mock", Operation: "terminalFailure", Message: "object was not found",
	}
	return sdkgo.NewQueryBranch(integrationQueryBranchFailed, struct{}{}, &failure, sdkgo.Receipt{})
}

type openAIRecoveryFlow struct {
	dex.FlowDefaults
	client     *openai.Client
	connection sdkgo.ConnectionRef
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
	connection sdkgo.ConnectionRef
}

func (step openAIRecoveryStartStep) Execute(ctx dex.Context, _ struct{}) (*dex.StepDecision, error) {
	result, err := sdkgo.RunMutation(
		ctx, step.client.CreateResponse(), step.connection,
		openai.CreateRequest{Model: "gpt-test", Input: "recover this"},
		sdkgo.WithProgressStream(openAIProgress),
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
	connection sdkgo.ConnectionRef
}

func (step openAIRetrieveStep) Execute(ctx dex.Context, input openai.RetrieveRequest) (*dex.StepDecision, error) {
	result, err := sdkgo.RunQuery(ctx, step.client.RetrieveResponse(), step.connection, input)
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

func (flow *rpcBoundaryFlow) AttemptProviderQuery(ctx dex.Context, connection sdkgo.ConnectionRef) (*dex.RPCResult[bool], error) {
	result, err := sdkgo.RunQuery(ctx, flow.query, connection, httpconnector.Request{
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

func connectorClient(t *testing.T, provider *mockprovider.Provider) (sdkgo.ConnectionRef, httpconnector.Connection, *httpconnector.Client) {
	t.Helper()
	connection := sdkgo.ConnectionRef{Provider: "mock", Name: "default"}
	client, err := httpconnector.New(httpconnector.Config{
		BaseURL: provider.URL(), CredentialHeaders: map[string]string{"api_key": "X-Mock-Api-Key"},
	}, sdkgo.StaticCredentialProvider[httpconnector.Credentials]{
		connection: {APIKey: sdkgo.NewSecretString("test-key")},
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

func runGitHubProfile(t *testing.T, client *dex.Client, flow githubProfileFlow, flowID string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	_, err := client.StartFlow(ctx, flow, flowID, struct{}{}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status)
}

func localConnectionRecord(
	connectorID string,
	provider string,
	connectionName string,
	configuration map[string]any,
	credentials map[string]any,
	expiresAt time.Time,
) map[string]any {
	modulePath := "github.com/superdurable/dex-connectors-library/connectors/" + connectorID
	if connectorID == "gmail" {
		modulePath = "github.com/superdurable/dex-connectors-library/connectors/google/gmail"
	}
	return map[string]any{
		"connectorId": connectorID, "modulePath": modulePath, "moduleVersion": "v0.1.1",
		"provider": provider, "connectionName": connectionName, "configuration": configuration,
		"credentials": credentials, "credentialExpiresAt": expiresAt.UTC().Format(time.RFC3339),
	}
}

func writeLocalConnections(t *testing.T, path string, connections ...map[string]any) {
	t.Helper()
	contents, err := json.Marshal(map[string]any{
		"schemaVersion": localconfig.SchemaVersion,
		"connections":   connections,
	})
	require.NoError(t, err)
	temporaryFile, err := os.CreateTemp(filepath.Dir(path), ".connections-*.json")
	require.NoError(t, err)
	temporaryPath := temporaryFile.Name()
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(temporaryPath)) })
	require.NoError(t, temporaryFile.Chmod(0o600))
	_, err = temporaryFile.Write(contents)
	require.NoError(t, err)
	require.NoError(t, temporaryFile.Sync())
	require.NoError(t, temporaryFile.Close())
	require.NoError(t, os.Rename(temporaryPath, path))
}
