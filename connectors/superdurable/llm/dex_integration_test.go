//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llmrouter_test

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	llmrouter "github.com/superdurable/dex-connectors-library/connectors/superdurable/llm"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm/llmtest"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

var (
	routingTextStream = dex.DefineStream[string]("llm-routing-text", 1<<20)
	routingResults    = dex.DefineAttribute[[]llm.TextGenerationResult]("llm-routing-results")
)

// TestGenerateTextRunsTheTextGenerationScenariosThroughEveryRouteWithRealDex runs the shared real-Dex scenarios through each route.
func TestGenerateTextRunsTheTextGenerationScenariosThroughEveryRouteWithRealDex(t *testing.T) {
	for _, routed := range routedProviders {
		t.Run(string(routed.provider), func(t *testing.T) {
			llmtest.RunTextGenerationDexScenarios(t, &llmtest.TextGenerationDexScenarioSuite{
				Dialect: routed.dexScenarioDialect(), ConnectorID: llmrouter.ConnectorID,
				NewGenerateTextStep: func(
					t testing.TB, connection llmtest.FakeConnection, config llmtest.DexScenarioStepConfig,
				) sdkgo.QueryStep[llmtest.DexScenarioInput, llm.TextGenerationRequest, llm.TextGenerationResponse] {
					llmConnection, err := llmrouter.NewConnection(newFakeConnectionClient(t, routed, connection), connection.Reference)
					require.NoError(t, err)
					return llmrouter.NewGenerateTextStep(llmrouter.GenerateTextStepConfig[llmtest.DexScenarioInput]{
						StepType: config.StepType, Annotations: config.Annotations, Connection: llmConnection,
						MapToOperationInput: qualifyStepModel(routed.provider, config.MapToOperationInput),
						Generated:           config.Generated, Truncated: config.Truncated, Blocked: config.Blocked,
						ProviderRejected: config.ProviderRejected, InvalidResponse: config.InvalidResponse, Defect: config.Defect,
						TextStream: config.TextStream, StepOptionsOverride: config.StepOptionsOverride,
					})
				},
			})
		})
	}
}

// qualifyStepModel prefixes a scenario's bare model pick with the route's provider, as a picker would save it.
func qualifyStepModel(
	provider llmrouter.Provider, mapToOperationInput func(llmtest.DexScenarioInput) llm.TextGenerationRequest,
) func(llmtest.DexScenarioInput) llm.TextGenerationRequest {
	return func(input llmtest.DexScenarioInput) llm.TextGenerationRequest {
		request := mapToOperationInput(input)
		if request.Model != "" {
			request.Model = string(provider) + "/" + request.Model
		}
		return request
	}
}

// TestStepsPinnedToEachProviderRouteWithRealDex runs three Steps of one Flow, one per provider, on one connection.
func TestStepsPinnedToEachProviderRouteWithRealDex(t *testing.T) {
	fakes := newProviderFakes(t)
	for _, routed := range routedProviders {
		fakes.providers[routed.provider].EnqueueReplies(routed.generatedReply(routed.connectorID + " text. "))
	}
	connection := newRoutingConnection(t, fakes, llmrouter.Config{Model: "openai"}, staticCredentials(allTestAPIKeys()))
	flow := &routingFlow{flowType: uniqueName("LLMRoutingThreeProviders")}
	for index, routed := range routedProviders {
		next := ""
		if index+1 < len(routedProviders) {
			next = "GenerateWith" + string(routedProviders[index+1].provider)
		}
		flow.addGenerateStep(connection, "GenerateWith"+string(routed.provider), string(routed.provider)+"/"+routed.dialect.AlternateModel,
			collectRoutingResult{stepType: "Collect" + string(routed.provider), nextStepType: next}, nil)
	}
	harness := newRoutingHarness(t, flow)
	flowID := uniqueName("llm-routing-three")
	result := harness.runFlow(t, flow, flowID)
	require.Equal(t, dex.FlowCompleted, result.Status, "Flow %s: %s", flowID, result.ErrorMessage)
	var results []llm.TextGenerationResult
	require.NoError(t, result.DecodeSingleOutput(&results))
	require.Len(t, results, 3)
	var expectedText strings.Builder
	for index, routed := range routedProviders {
		require.Equal(t, llmrouter.GenerateTextBranchGenerated, results[index].Branch, "failure: %+v", results[index].Failure)
		require.Equal(t, routed.connectorID, results[index].Receipt.Provider)
		require.Equal(t, string(routed.provider)+"/"+routed.dialect.AlternateModel, llmrouter.QualifiedModel(results[index]))
		requests := fakes.providers[routed.provider].Requests()
		require.Len(t, requests, 1, "each provider receives exactly its own Step")
		require.True(t, requests[0].HasCredentialInSlot)
		require.False(t, requests[0].HasCredentialOutsideSlot)
		expectedText.WriteString(routed.connectorID + " text. ")
	}
	encoded, err := json.Marshal(results)
	require.NoError(t, err)
	for _, apiKey := range testAPIKeys {
		require.False(t, strings.Contains(string(encoded), apiKey), "a persisted Result contains an API key")
	}
	require.Eventually(t, func() bool {
		text, err := harness.readText(flowID)
		return err == nil && text == expectedText.String()
	}, 10*time.Second, 100*time.Millisecond, "the text Stream holds each provider's text in Step order")
}

func TestMissingKeysAndRetriesStayOnTheSelectedProviderWithRealDex(t *testing.T) {
	t.Run("a wired defect completes the route without a request", func(t *testing.T) {
		fakes := newProviderFakes(t)
		credentials := routedProviderFor(t, llmrouter.ProviderAnthropic).withAPIKey(allTestAPIKeys(), "")
		connection := newRoutingConnection(t, fakes, llmrouter.Config{Model: "anthropic"}, staticCredentials(credentials))
		flow := &routingFlow{flowType: uniqueName("LLMRoutingWiredDefect")}
		flow.addGenerateStep(connection, "GenerateWithoutClaudeKey", "", collectRoutingResult{stepType: "CollectDefect"}, &routingBranches{isDefectWired: true})
		results := requireCompletedRoutingResults(t, newRoutingHarness(t, flow), flow, uniqueName("llm-wired-defect"))
		require.Equal(t, llmrouter.GenerateTextBranchDefect, results[0].Branch)
		require.Equal(t, "the model selects anthropic, but the connection has no anthropic_api_key", results[0].Failure.Message)
		require.Equal(t, "llm", results[0].Receipt.Provider)
		require.Equal(t, map[llmrouter.Provider]int{
			llmrouter.ProviderOpenAI: 0, llmrouter.ProviderAnthropic: 0, llmrouter.ProviderGemini: 0,
		}, fakes.requestCounts())
	})

	t.Run("an unwired defect fails the Flow without a request", func(t *testing.T) {
		fakes := newProviderFakes(t)
		credentials := routedProviderFor(t, llmrouter.ProviderGemini).withAPIKey(allTestAPIKeys(), "")
		connection := newRoutingConnection(t, fakes, llmrouter.Config{Model: "openai"}, staticCredentials(credentials))
		flow := &routingFlow{flowType: uniqueName("LLMRoutingUnwiredDefect")}
		flow.addGenerateStep(connection, "GenerateWithoutGeminiKey", "gemini/gemini-3.8-flash", collectRoutingResult{stepType: "CollectUnwired"}, nil)
		harness := newRoutingHarness(t, flow)
		started := time.Now()
		result := harness.runFlow(t, flow, uniqueName("llm-unwired-defect"))
		require.Equal(t, dex.FlowFailed, result.Status)
		require.Contains(t, result.ErrorMessage, `connector branch "defect" has no target`)
		require.Less(t, time.Since(started), 30*time.Second, "a defect is not retried")
		require.Equal(t, map[llmrouter.Provider]int{
			llmrouter.ProviderOpenAI: 0, llmrouter.ProviderAnthropic: 0, llmrouter.ProviderGemini: 0,
		}, fakes.requestCounts())
	})

	t.Run("a provider added between runs takes effect without a restart", func(t *testing.T) {
		fakes := newProviderFakes(t)
		claudeRoute := routedProviderFor(t, llmrouter.ProviderAnthropic)
		fakes.providers[llmrouter.ProviderAnthropic].EnqueueReplies(claudeRoute.generatedReply("Claude answered."))
		connectionsPath := filepath.Join(t.TempDir(), "connections.json")
		writeLocalConnectionFile(t, connectionsPath, map[string]any{"model": "anthropic"},
			map[string]any{"auth_methods": []string{"openai"}, "openai_api_key": openAITestKey})
		store, err := localconfig.LoadFile(connectionsPath)
		require.NoError(t, err)
		connection, err := llmrouter.NewLocalConnection(store, testConnection.Name, fakes.options()...)
		require.NoError(t, err)
		flow := &routingFlow{flowType: uniqueName("LLMRoutingProviderAdded")}
		flow.addGenerateStep(connection, "GenerateWithClaude", "", collectRoutingResult{stepType: "CollectProviderAdded"}, &routingBranches{isDefectWired: true})
		harness := newRoutingHarness(t, flow)
		before := requireCompletedRoutingResults(t, harness, flow, uniqueName("llm-provider-missing"))
		require.Equal(t, llmrouter.GenerateTextBranchDefect, before[0].Branch)
		require.Equal(t, "the model selects anthropic, but the connection has not added the Claude provider; add Claude to the connection",
			before[0].Failure.Message)
		writeLocalConnectionFile(t, connectionsPath, map[string]any{"model": "anthropic"}, map[string]any{
			"auth_methods": []string{"openai", "anthropic"}, "openai_api_key": openAITestKey, "anthropic_api_key": anthropicTestKey,
		})
		after := requireCompletedRoutingResults(t, harness, flow, uniqueName("llm-provider-added"))
		require.Equal(t, llmrouter.GenerateTextBranchGenerated, after[0].Branch, "failure: %+v", after[0].Failure)
		require.Equal(t, "Claude answered.", after[0].Value.Text)
		require.Len(t, fakes.providers[llmrouter.ProviderAnthropic].Requests(), 1)
		require.True(t, fakes.providers[llmrouter.ProviderAnthropic].Requests()[0].HasCredentialInSlot)
	})

	t.Run("a blank model follows the first added provider across removal without a restart", func(t *testing.T) {
		fakes := newProviderFakes(t)
		geminiRoute, openAIRoute := routedProviderFor(t, llmrouter.ProviderGemini), routedProviderFor(t, llmrouter.ProviderOpenAI)
		fakes.providers[llmrouter.ProviderGemini].EnqueueReplies(geminiRoute.generatedReply("Gemini answered."))
		fakes.providers[llmrouter.ProviderOpenAI].EnqueueReplies(openAIRoute.generatedReply("OpenAI answered."))
		connectionsPath := filepath.Join(t.TempDir(), "connections.json")
		writeLocalConnectionFile(t, connectionsPath, map[string]any{}, map[string]any{
			"auth_methods": []string{"gemini", "openai"}, "gemini_api_key": geminiTestKey, "openai_api_key": openAITestKey,
		})
		store, err := localconfig.LoadFile(connectionsPath)
		require.NoError(t, err)
		connection, err := llmrouter.NewLocalConnection(store, testConnection.Name, fakes.options()...)
		require.NoError(t, err)
		flow := &routingFlow{flowType: uniqueName("LLMRoutingFirstAddedProvider")}
		flow.addGenerateStep(connection, "GenerateWithFirstProvider", "", collectRoutingResult{stepType: "CollectFirstProvider"}, nil)
		harness := newRoutingHarness(t, flow)
		first := requireCompletedRoutingResults(t, harness, flow, uniqueName("llm-first-gemini"))
		require.Equal(t, "gemini/gemini-3.5-flash-lite", llmrouter.QualifiedModel(first[0]), "failure: %+v", first[0].Failure)
		// Removing Gemini drops its key, as Dex Web does, and OpenAI becomes the first added provider.
		writeLocalConnectionFile(t, connectionsPath, map[string]any{},
			map[string]any{"auth_methods": []string{"openai"}, "openai_api_key": openAITestKey})
		second := requireCompletedRoutingResults(t, harness, flow, uniqueName("llm-first-openai"))
		require.Equal(t, "openai/gpt-6-sol", llmrouter.QualifiedModel(second[0]), "failure: %+v", second[0].Failure)
		require.Equal(t, "OpenAI answered.", second[0].Value.Text)
		require.Len(t, fakes.providers[llmrouter.ProviderGemini].Requests(), 1)
		require.Len(t, fakes.providers[llmrouter.ProviderOpenAI].Requests(), 1)
		require.Empty(t, fakes.providers[llmrouter.ProviderAnthropic].Requests())
	})

	t.Run("a v0.1.0 record without providers selects defect until it is saved again", func(t *testing.T) {
		fakes := newProviderFakes(t)
		connectionsPath := filepath.Join(t.TempDir(), "connections.json")
		writeLocalConnectionFile(t, connectionsPath, map[string]any{"model": "openai"}, map[string]any{"openai_api_key": openAITestKey})
		store, err := localconfig.LoadFile(connectionsPath)
		require.NoError(t, err)
		connection, err := llmrouter.NewLocalConnection(store, testConnection.Name, fakes.options()...)
		require.NoError(t, err, "the Worker starts, because credentials are read per call")
		flow := &routingFlow{flowType: uniqueName("LLMRoutingPreviousRecord")}
		flow.addGenerateStep(connection, "GenerateWithPreviousRecord", "", collectRoutingResult{stepType: "CollectPreviousRecord"},
			&routingBranches{isDefectWired: true})
		results := requireCompletedRoutingResults(t, newRoutingHarness(t, flow), flow, uniqueName("llm-previous-record"))
		require.Equal(t, llmrouter.GenerateTextBranchDefect, results[0].Branch)
		require.Equal(t, sdkgo.FailureAuthentication, results[0].Failure.Kind)
		require.Equal(t, "connection credentials are unavailable", results[0].Failure.Message)
		require.Equal(t, map[llmrouter.Provider]int{
			llmrouter.ProviderOpenAI: 0, llmrouter.ProviderAnthropic: 0, llmrouter.ProviderGemini: 0,
		}, fakes.requestCounts())
	})

	t.Run("a Claude 429 is retried on Claude", func(t *testing.T) {
		fakes := newProviderFakes(t)
		claudeRoute := routedProviderFor(t, llmrouter.ProviderAnthropic)
		rateLimited := claudeRoute.dialect.ErrorReply(http.StatusTooManyRequests, "Slow down.")
		rateLimited.Header = rateLimited.Header.Clone()
		rateLimited.Header.Set("Retry-After", "1")
		fakes.providers[llmrouter.ProviderAnthropic].EnqueueReplies(rateLimited, claudeRoute.generatedReply("After the limit."))
		connection := newRoutingConnection(t, fakes, llmrouter.Config{Model: "anthropic/claude-haiku-4-5"}, staticCredentials(allTestAPIKeys()))
		flow := &routingFlow{flowType: uniqueName("LLMRoutingRateLimited")}
		flow.addGenerateStep(connection, "GenerateAfterRateLimit", "", collectRoutingResult{stepType: "CollectRateLimited"}, &routingBranches{
			stepOptionsOverride: &dex.StepOptions{ExecuteRetry: &dex.RetryPolicy{
				InitialInterval: 50 * time.Millisecond, BackoffCoefficient: 1, MaximumInterval: 50 * time.Millisecond, MaximumAttempts: 3,
			}},
		})
		results := requireCompletedRoutingResults(t, newRoutingHarness(t, flow), flow, uniqueName("llm-rate-limited"))
		require.Equal(t, llmrouter.GenerateTextBranchGenerated, results[0].Branch)
		require.Equal(t, "claude", results[0].Receipt.Provider)
		requests := fakes.providers[llmrouter.ProviderAnthropic].Requests()
		require.Len(t, requests, 2, "the retry returns to Claude")
		require.GreaterOrEqual(t, requests[1].ReceivedAt.Sub(requests[0].ReceivedAt), 950*time.Millisecond)
		require.Empty(t, fakes.providers[llmrouter.ProviderOpenAI].Requests())
		require.Empty(t, fakes.providers[llmrouter.ProviderGemini].Requests())
	})
}

func newRoutingConnection(
	t *testing.T, fakes *providerFakes, config llmrouter.Config, credentials sdkgo.CredentialProvider[llmrouter.Credentials],
) llmrouter.Connection {
	t.Helper()
	connection, err := llmrouter.NewConnection(newRoutedClient(t, fakes, config, credentials), testConnection)
	require.NoError(t, err)
	return connection
}

// routingInput is the start input of every routing Flow.
type routingInput struct {
	Prompt string `json:"prompt"`
}

// routingBranches optionally wires more than the generated branch.
type routingBranches struct {
	isDefectWired       bool
	stepOptionsOverride *dex.StepOptions
}

// routingFlow is a Flow of llm Steps, each followed by a Step that collects its Result.
type routingFlow struct {
	dex.FlowDefaults
	flowType string
	steps    []dex.StepDef
}

func (flow *routingFlow) addGenerateStep(
	connection llmrouter.Connection, stepType string, model string, collect collectRoutingResult, branches *routingBranches,
) {
	branches = cmp.Or(branches, &routingBranches{})
	config := llmrouter.GenerateTextStepConfig[routingInput]{
		StepType: stepType, Connection: connection,
		Annotations: sdkgo.StepAnnotations{GroupID: "routing", GroupLabel: "Routing", Explanation: "Generate with the Step's provider."},
		MapToOperationInput: func(input routingInput) llmrouter.GenerateTextRequest {
			return userRequest(model, input.Prompt)
		},
		Generated: sdkgo.GoTo(collect), TextStream: &routingTextStream, StepOptionsOverride: branches.stepOptionsOverride,
	}
	if branches.isDefectWired {
		config.Defect = sdkgo.GoTo(collect)
	}
	step := llmrouter.NewGenerateTextStep(config)
	if len(flow.steps) == 0 {
		flow.steps = append(flow.steps, dex.DefineStartStep(step))
	} else {
		flow.steps = append(flow.steps, dex.DefineStep(step))
	}
	flow.steps = append(flow.steps, dex.DefineStep(collect))
}

// GetFlowType returns the Flow's unique type.
func (flow *routingFlow) GetFlowType() string { return flow.flowType }

// GetSteps returns the llm and collector Steps.
func (flow *routingFlow) GetSteps() []dex.StepDef { return flow.steps }

// GetPersistenceSchema registers the collected Results and the shared text Stream.
func (*routingFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{routingResults}, Streams: []dex.StreamDef{routingTextStream}}
}

// collectRoutingResult appends a Result and runs the next llm Step, or completes with every Result.
type collectRoutingResult struct {
	dex.StepDefaultsNoWaitFor[llmrouter.GenerateTextResult]
	stepType, nextStepType string
}

// GetStepType returns the collector's Step type.
func (step collectRoutingResult) GetStepType() string { return step.stepType }

// Execute appends result to the collected Results.
func (step collectRoutingResult) Execute(ctx dex.Context, result llmrouter.GenerateTextResult) (*dex.StepDecision, error) {
	collected, err := routingResults.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if err != nil && !errors.As(err, &missing) {
		return nil, err
	}
	collected = append(collected, result)
	if err := routingResults.Set(ctx, collected); err != nil {
		return nil, err
	}
	if step.nextStepType == "" {
		return dex.GracefulComplete(collected), nil
	}
	return dex.GoTo(sdkgo.StepRef[routingInput](step.nextStepType), routingInput{Prompt: "Continue."}), nil
}

// routingHarness owns one Flow's Registry, Worker, and Client.
type routingHarness struct {
	client *dex.Client
}

func newRoutingHarness(t *testing.T, flow dex.Flow) *routingHarness {
	t.Helper()
	serverAddress := cmp.Or(os.Getenv(llmtest.DexFlowServiceAddressEnvironmentVariable), llmtest.DefaultDexFlowServiceAddress)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	workerAddress := listener.Addr().String()
	require.NoError(t, listener.Close())
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{
		BindAddress: workerAddress, FlowServiceAddress: serverAddress, WorkerTarget: dex.WorkerTarget{Address: workerAddress},
	})
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{
		FlowServiceAddress: serverAddress, WorkerTarget: &dex.WorkerTarget{Address: workerAddress},
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(worker.Stop(ctx), <-workerResult, client.Close(), cache.Close()))
	})
	return &routingHarness{client: client}
}

// runFlow starts the Flow and waits, across server long-poll caps, until it closes.
func (harness *routingHarness) runFlow(t *testing.T, flow dex.Flow, flowID string) dex.FlowResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	_, err := harness.client.StartFlow(ctx, flow, flowID, routingInput{Prompt: "Say hello."}, dex.StartFlowOptions{})
	require.NoError(t, err)
	for {
		result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
		var longPollTimeout *dex.LongPollTimeoutError
		if errors.As(err, &longPollTimeout) {
			continue
		}
		require.NoError(t, err, "Flow %s did not close", flowID)
		return result
	}
}

// readText joins the text Stream's messages oldest first.
func (harness *routingHarness) readText(flowID string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var page dex.StreamMessagesPage[string]
	if err := harness.client.ListStreamMessages(ctx, flowID, routingTextStream, 100, "", &page); err != nil {
		return "", err
	}
	var text strings.Builder
	for index := len(page.Messages) - 1; index >= 0; index-- {
		text.WriteString(page.Messages[index].Value)
	}
	return text.String(), nil
}

func requireCompletedRoutingResults(t *testing.T, harness *routingHarness, flow dex.Flow, flowID string) []llm.TextGenerationResult {
	t.Helper()
	result := harness.runFlow(t, flow, flowID)
	require.Equal(t, dex.FlowCompleted, result.Status, "Flow %s: %s", flowID, result.ErrorMessage)
	var results []llm.TextGenerationResult
	require.NoError(t, result.DecodeSingleOutput(&results))
	require.NotEmpty(t, results)
	return results
}

func uniqueName(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}
