// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llmtest

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
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

// DexFlowServiceAddressEnvironmentVariable names the Dex Server address the
// real-Dex scenarios use. When it is unset they use DefaultDexFlowServiceAddress.
const DexFlowServiceAddressEnvironmentVariable = "DEX_FLOW_SERVICE_ADDRESS"

// DefaultDexFlowServiceAddress is the local dexcli dev stack's Dex Server address.
const DefaultDexFlowServiceAddress = "127.0.0.1:8801"

const (
	scenarioPrompt = "Say hello to the llmtest scenario."
	// silentProviderDelay is twice silentProviderHeartbeatTimeout, Dex's minimum, so only the timer heartbeat keeps the attempt alive.
	silentProviderDelay            = 20 * time.Second
	silentProviderHeartbeatTimeout = 10 * time.Second
	// lostWorkerProviderDelay keeps the first attempt in flight until the scenario stops its Worker.
	lostWorkerProviderDelay = time.Minute
	pickedModelStepType     = "GenerateWithPickedModel"
	inheritedModelStepType  = "GenerateWithInheritedModel"
)

var (
	scenarioTextStream        = dex.DefineStream[string]("llmtest-generated-text", 1<<20)
	scenarioPickedModelResult = dex.DefineAttribute[llm.TextGenerationResult]("llmtest-picked-model-result")
)

// DexScenarioInput is the start input of every scenario Flow.
type DexScenarioInput struct {
	// Prompt is the user message the scenario's generateText Step sends.
	Prompt string `json:"prompt"`
}

// DexScenarioStepConfig holds the values a scenario sets on the connector's
// generated GenerateTextStepConfig. The connector's closure copies each field
// to the field with the same name and adds its own Connection.
type DexScenarioStepConfig struct {
	// StepType is the stable Step type.
	StepType string
	// Annotations are the Step's group and explanation.
	Annotations sdkgo.StepAnnotations
	// MapToOperationInput builds the request from the scenario input.
	MapToOperationInput func(DexScenarioInput) llm.TextGenerationRequest
	// Generated is the required happy-path target.
	Generated sdkgo.Target[llm.TextGenerationResult]
	// Truncated is an optional target; scenarios leave it empty to prove an unwired branch fails the Flow.
	Truncated sdkgo.Target[llm.TextGenerationResult]
	// Blocked is an optional target.
	Blocked sdkgo.Target[llm.TextGenerationResult]
	// ProviderRejected is an optional target.
	ProviderRejected sdkgo.Target[llm.TextGenerationResult]
	// InvalidResponse is an optional target.
	InvalidResponse sdkgo.Target[llm.TextGenerationResult]
	// Defect is an optional target.
	Defect sdkgo.Target[llm.TextGenerationResult]
	// TextStream receives generated text when non-nil.
	TextStream *dex.Stream[string]
	// StepOptionsOverride overrides the operation's Step defaults when non-nil.
	StepOptionsOverride *dex.StepOptions
}

// TextGenerationDexScenarioSuite configures RunTextGenerationDexScenarios for one connector.
type TextGenerationDexScenarioSuite struct {
	// Dialect describes the provider API. It is required.
	Dialect ProviderDialect
	// ConnectorID is the connector's manifest ID, used for the scenario's
	// local configuration files. It is required.
	ConnectorID string
	// NewGenerateTextStep builds the connector's generateText Step with its
	// generated factory: a Connection for connection, and config's fields. The
	// connection's request timeout must exceed 20 seconds. It is required.
	NewGenerateTextStep func(t testing.TB, connection FakeConnection, config DexScenarioStepConfig) sdkgo.QueryStep[DexScenarioInput, llm.TextGenerationRequest, llm.TextGenerationResponse]
}

// RunTextGenerationDexScenarios runs one-Step Flows that use the connector's
// generated generateText Step through a real Dex Worker and a FakeProvider.
//
// It uses the Dex Server at DEX_FLOW_SERVICE_ADDRESS, or at
// DefaultDexFlowServiceAddress when the variable is unset, and fails when that
// server is unreachable or suite is nil or incomplete. Call it from a test
// file with the integration build tag. Each scenario registers a unique Flow
// type and Flow ID, starts its own Worker, and waits for the Flow to close.
// The scenarios prove that:
//
//   - a generated result completes the Flow and its text reaches the text Stream in order;
//   - a 429 with Retry-After: 1 is retried after at least one second and then completes;
//   - selecting the unwired optional truncated branch fails the Flow;
//   - a Step's model pick loaded with localconfig.LoadOperationConfiguration
//     overrides the connection model, and a Step without a pick, reported by
//     localconfig.ErrConfigurationNotFound, inherits it;
//   - a provider that stays silent for 20 seconds under Dex's minimum
//     10-second heartbeat timeout completes in one attempt, because the
//     pipeline heartbeats;
//   - an attempt whose Worker is lost mid-exchange is retried on a new Worker,
//     so the provider receives the request twice and the Flow completes once;
//   - with ProviderDialect.InterruptedStreamReply, an interrupted stream is
//     retried and the text Stream holds the interrupted attempt's text before
//     the whole text of the retry, while the Result holds only the retry's text.
//
// The scenarios take about 25 seconds, most of it the silent provider.
func RunTextGenerationDexScenarios(t *testing.T, suite *TextGenerationDexScenarioSuite) {
	t.Helper()
	if suite == nil {
		t.Fatal("llmtest Dex scenario suite is required")
	}
	validateProviderDialect(t, &suite.Dialect)
	if suite.ConnectorID == "" || suite.NewGenerateTextStep == nil {
		t.Fatal("llmtest Dex scenarios require ConnectorID and NewGenerateTextStep")
	}
	serverAddress := cmp.Or(os.Getenv(DexFlowServiceAddressEnvironmentVariable), DefaultDexFlowServiceAddress)
	run := dexScenarioRun{suite: suite, serverAddress: serverAddress}
	t.Run("generated result completes the Flow", run.testGenerated)
	t.Run("retry after rate limit then generated", run.testRetryThenGenerated)
	t.Run("unwired optional branch fails the Flow", run.testUnwiredOptionalBranch)
	t.Run("step model pick overrides the connection model", run.testModelPrecedence)
	t.Run("silent provider completes in one attempt", run.testSilentProvider)
	t.Run("lost Worker is retried on a new Worker", run.testLostWorker)
	if suite.Dialect.InterruptedStreamReply != nil {
		t.Run("interrupted stream repeats text on the text Stream", run.testInterruptedStreamRepeatsText)
	}
}

type dexScenarioRun struct {
	suite         *TextGenerationDexScenarioSuite
	serverAddress string
}

func (run dexScenarioRun) testGenerated(t *testing.T) {
	provider, connection := run.newProvider(t)
	provider.EnqueueReplies(run.generatedReply(0))
	generate := run.suite.NewGenerateTextStep(t, connection, DexScenarioStepConfig{
		StepType: "GenerateScenarioText", Annotations: scenarioAnnotations("Generate the scenario text."),
		MapToOperationInput: requestWithModel(""), Generated: sdkgo.GoTo(completeWithGenerationStep{}),
		TextStream: &scenarioTextStream,
	})
	flow := newScenarioFlow(run.flowType("Generated"), dex.DefineStartStep(generate), dex.DefineStep(completeWithGenerationStep{}))
	harness := newScenarioHarness(t, run.serverAddress, flow)
	flowID := uniqueFlowID("llmtest-generated")
	results := harness.requireCompletedResults(t, flow, flowID)
	require.Len(t, results, 1)
	require.Equal(t, llm.GeneratedBranchID, results[0].Branch)
	require.Equal(t, generatedText, results[0].Value.Text)
	require.Equal(t, canonicalDialectModel(run.suite.Dialect.ConnectionModel), results[0].Value.RequestedModel)
	require.NoError(t, results[0].Receipt.CallID.Validate())
	require.Len(t, provider.Requests(), 1)
	// Stream frames are best-effort and outside the Step commit, so poll until they converge.
	require.Eventually(t, func() bool {
		text, err := harness.readText(flowID)
		return err == nil && text == generatedText
	}, 10*time.Second, 100*time.Millisecond, "the generated text must reach the text Stream")
}

func (run dexScenarioRun) testRetryThenGenerated(t *testing.T) {
	provider, connection := run.newProvider(t)
	rateLimited := run.suite.Dialect.ErrorReply(http.StatusTooManyRequests, providerMessageCanary)
	rateLimited.Header = cloneHeaderWith(rateLimited.Header, "Retry-After", "1")
	provider.EnqueueReplies(rateLimited, run.generatedReply(0))
	generate := run.suite.NewGenerateTextStep(t, connection, DexScenarioStepConfig{
		StepType: "GenerateAfterRateLimit", Annotations: scenarioAnnotations("Generate after a rate limit."),
		MapToOperationInput: requestWithModel(""), Generated: sdkgo.GoTo(completeWithGenerationStep{}),
		StepOptionsOverride: &dex.StepOptions{ExecuteRetry: &dex.RetryPolicy{
			InitialInterval: 50 * time.Millisecond, BackoffCoefficient: 1, MaximumInterval: 50 * time.Millisecond, MaximumAttempts: 3,
		}},
	})
	flow := newScenarioFlow(run.flowType("RetryThenGenerated"), dex.DefineStartStep(generate), dex.DefineStep(completeWithGenerationStep{}))
	harness := newScenarioHarness(t, run.serverAddress, flow)
	results := harness.requireCompletedResults(t, flow, uniqueFlowID("llmtest-retry"))
	require.Equal(t, llm.GeneratedBranchID, results[0].Branch)
	requests := provider.Requests()
	require.Len(t, requests, 2)
	// Elapsed time is the behavior: Dex must honor the provider's Retry-After.
	require.GreaterOrEqual(t, requests[1].ReceivedAt.Sub(requests[0].ReceivedAt), 950*time.Millisecond)
}

func (run dexScenarioRun) testUnwiredOptionalBranch(t *testing.T) {
	provider, connection := run.newProvider(t)
	provider.EnqueueReplies(run.suite.Dialect.TruncatedReply(GeneratedReply{
		Text: partialText, ServedModel: servedModel, ResponseID: responseID, Usage: reportedUsage,
	}))
	generate := run.suite.NewGenerateTextStep(t, connection, DexScenarioStepConfig{
		StepType: "GenerateWithoutTruncatedTarget", Annotations: scenarioAnnotations("Generate without a truncated target."),
		MapToOperationInput: requestWithModel(""), Generated: sdkgo.GoTo(completeWithGenerationStep{}),
	})
	flow := newScenarioFlow(run.flowType("UnwiredOptionalBranch"), dex.DefineStartStep(generate), dex.DefineStep(completeWithGenerationStep{}))
	harness := newScenarioHarness(t, run.serverAddress, flow)
	result := harness.runFlow(t, flow, uniqueFlowID("llmtest-unwired"))
	require.Equal(t, dex.FlowFailed, result.Status)
	require.Contains(t, result.ErrorMessage, `connector branch "truncated" has no target`)
	require.Len(t, provider.Requests(), 1)
}

func (run dexScenarioRun) testModelPrecedence(t *testing.T) {
	provider, connection := run.newProvider(t)
	provider.EnqueueReplies(run.generatedReply(0), run.generatedReply(0))
	flowType := run.flowType("ModelPrecedence")
	store := run.writeLocalConfiguration(t, connection, flowType)
	pickedModel, err := loadStepModelPick(store, run.configurationReference(connection, flowType, pickedModelStepType))
	require.NoError(t, err)
	inheritedModel, err := loadStepModelPick(store, run.configurationReference(connection, flowType, inheritedModelStepType))
	require.NoError(t, err)
	require.Equal(t, run.suite.Dialect.AlternateModel, pickedModel)
	require.Empty(t, inheritedModel, "a Step without a saved pick inherits the connection model")

	generateWithPick := run.suite.NewGenerateTextStep(t, connection, DexScenarioStepConfig{
		StepType: pickedModelStepType, Annotations: scenarioAnnotations("Generate with the Step's picked model."),
		MapToOperationInput: requestWithModel(pickedModel), Generated: sdkgo.GoTo(rememberPickedModelResultStep{}),
	})
	generateWithInheritedModel := run.suite.NewGenerateTextStep(t, connection, DexScenarioStepConfig{
		StepType: inheritedModelStepType, Annotations: scenarioAnnotations("Generate with the connection model."),
		MapToOperationInput: requestWithModel(inheritedModel), Generated: sdkgo.GoTo(completeModelPrecedenceStep{}),
	})
	flow := newScenarioFlow(flowType,
		dex.DefineStartStep(generateWithPick), dex.DefineStep(rememberPickedModelResultStep{}),
		dex.DefineStep(generateWithInheritedModel), dex.DefineStep(completeModelPrecedenceStep{}),
	)
	harness := newScenarioHarness(t, run.serverAddress, flow)
	results := harness.requireCompletedResults(t, flow, uniqueFlowID("llmtest-precedence"))
	require.Len(t, results, 2)
	alternateModel := canonicalDialectModel(run.suite.Dialect.AlternateModel)
	connectionModel := canonicalDialectModel(run.suite.Dialect.ConnectionModel)
	requests := provider.Requests()
	require.Len(t, requests, 2)
	require.Equal(t, alternateModel, results[0].Value.RequestedModel)
	require.Equal(t, alternateModel, run.requestModel(t, requests[0]))
	require.Equal(t, connectionModel, results[1].Value.RequestedModel)
	require.Equal(t, connectionModel, run.requestModel(t, requests[1]))
}

func (run dexScenarioRun) testSilentProvider(t *testing.T) {
	provider, connection := run.newProvider(t)
	provider.EnqueueReplies(run.generatedReply(silentProviderDelay))
	generate := run.suite.NewGenerateTextStep(t, connection, DexScenarioStepConfig{
		StepType: "GenerateFromSilentProvider", Annotations: scenarioAnnotations("Generate from a silent provider."),
		MapToOperationInput: requestWithModel(""), Generated: sdkgo.GoTo(completeWithGenerationStep{}),
		StepOptionsOverride: &dex.StepOptions{
			ExecuteMethodTimeout: 2 * time.Minute, HeartbeatTimeout: silentProviderHeartbeatTimeout,
			ExecuteRetry: &dex.RetryPolicy{MaximumAttempts: 1},
		},
	})
	flow := newScenarioFlow(run.flowType("SilentProvider"), dex.DefineStartStep(generate), dex.DefineStep(completeWithGenerationStep{}))
	harness := newScenarioHarness(t, run.serverAddress, flow)
	// With one attempt allowed, a heartbeat timeout fails the Flow, so completion is the assertion.
	results := harness.requireCompletedResults(t, flow, uniqueFlowID("llmtest-silent"))
	require.Equal(t, llm.GeneratedBranchID, results[0].Branch)
	require.Len(t, provider.Requests(), 1)
}

// testLostWorker force-stops the Worker while the provider holds the first request, like a crashed Worker.
func (run dexScenarioRun) testLostWorker(t *testing.T) {
	provider, connection := run.newProvider(t)
	provider.EnqueueReplies(run.generatedReply(lostWorkerProviderDelay), run.generatedReply(0))
	generate := run.suite.NewGenerateTextStep(t, connection, DexScenarioStepConfig{
		StepType: "GenerateAcrossLostWorker", Annotations: scenarioAnnotations("Generate across a lost Worker."),
		MapToOperationInput: requestWithModel(""), Generated: sdkgo.GoTo(completeWithGenerationStep{}),
		StepOptionsOverride: &dex.StepOptions{
			HeartbeatTimeout: silentProviderHeartbeatTimeout,
			ExecuteRetry: &dex.RetryPolicy{
				InitialInterval: 50 * time.Millisecond, BackoffCoefficient: 1, MaximumInterval: 50 * time.Millisecond, MaximumAttempts: 3,
			},
		},
	})
	flow := newScenarioFlow(run.flowType("LostWorker"), dex.DefineStartStep(generate), dex.DefineStep(completeWithGenerationStep{}))
	harness := newScenarioHarness(t, run.serverAddress, flow)
	flowID := uniqueFlowID("llmtest-lost-worker")
	harness.startFlow(t, flow, flowID)
	require.Eventually(t, func() bool { return len(provider.Requests()) == 1 }, 30*time.Second, 50*time.Millisecond,
		"the first attempt must reach the provider")
	harness.replaceWorker(t)
	result := harness.waitForFlow(t, flowID)
	require.Equal(t, dex.FlowCompleted, result.Status, "Flow %s: %s", flowID, result.ErrorMessage)
	var results []llm.TextGenerationResult
	require.NoError(t, result.DecodeSingleOutput(&results))
	require.Len(t, results, 1)
	require.Equal(t, llm.GeneratedBranchID, results[0].Branch)
	require.Equal(t, generatedText, results[0].Value.Text)
	require.Len(t, provider.Requests(), 2, "generateText repeats a call whose outcome the lost Worker never reported")
}

// testInterruptedStreamRepeatsText pins that Dex keeps text an interrupted attempt already streamed.
func (run dexScenarioRun) testInterruptedStreamRepeatsText(t *testing.T) {
	provider, connection := run.newProvider(t)
	provider.EnqueueReplies(run.suite.Dialect.InterruptedStreamReply(GeneratedReply{
		Text: partialText, ServedModel: servedModel, ResponseID: responseID, Usage: reportedUsage,
	}), run.generatedReply(0))
	generate := run.suite.NewGenerateTextStep(t, connection, DexScenarioStepConfig{
		StepType: "GenerateAfterInterruptedStream", Annotations: scenarioAnnotations("Generate after an interrupted stream."),
		MapToOperationInput: requestWithModel(""), Generated: sdkgo.GoTo(completeWithGenerationStep{}),
		TextStream: &scenarioTextStream,
		StepOptionsOverride: &dex.StepOptions{ExecuteRetry: &dex.RetryPolicy{
			InitialInterval: 50 * time.Millisecond, BackoffCoefficient: 1, MaximumInterval: 50 * time.Millisecond, MaximumAttempts: 3,
		}},
	})
	flow := newScenarioFlow(run.flowType("InterruptedStream"), dex.DefineStartStep(generate), dex.DefineStep(completeWithGenerationStep{}))
	harness := newScenarioHarness(t, run.serverAddress, flow)
	flowID := uniqueFlowID("llmtest-interrupted")
	results := harness.requireCompletedResults(t, flow, flowID)
	require.Equal(t, llm.GeneratedBranchID, results[0].Branch)
	require.Equal(t, generatedText, results[0].Value.Text, "the Result holds only the retry's text")
	require.Len(t, provider.Requests(), 2)
	var text string
	require.Eventually(t, func() bool {
		var err error
		text, err = harness.readText(flowID)
		return err == nil && strings.HasSuffix(text, generatedText) && len(text) > len(generatedText)
	}, 10*time.Second, 100*time.Millisecond, "the retry's text must follow the interrupted attempt's text")
	require.Equal(t, partialText+generatedText, text, "the text Stream keeps the interrupted attempt's text")
}

func (run dexScenarioRun) newProvider(t *testing.T) (*FakeProvider, FakeConnection) {
	t.Helper()
	provider := NewFakeProvider(t, run.suite.Dialect.CredentialHeader, credentialCanary)
	return provider, FakeConnection{
		Reference: sdkgo.ConnectionRef{Provider: "llmtest", Name: "scenario"},
		BaseURL:   provider.BaseURL(), Model: run.suite.Dialect.ConnectionModel,
		APIKey: sdkgo.NewSecretString(credentialCanary), MaxResponseBytes: 1 << 20,
	}
}

func (run dexScenarioRun) generatedReply(delay time.Duration) FakeReply {
	reply := run.suite.Dialect.GeneratedReply(GeneratedReply{
		Text: generatedText, ServedModel: servedModel, ResponseID: responseID, Usage: reportedUsage,
	})
	reply.Delay = delay
	return reply
}

// flowType is unique per run, so no stale Worker from another run or connector receives the Flow.
func (run dexScenarioRun) flowType(scenario string) string {
	return fmt.Sprintf("llmtest.%s.%s.%d", run.suite.ConnectorID, scenario, time.Now().UnixNano())
}

func (run dexScenarioRun) configurationReference(connection FakeConnection, flowType string, stepType string) sdkgo.ConnectorConfigurationRef {
	return sdkgo.ConnectorConfigurationRef{
		ConnectorID: run.suite.ConnectorID, ConnectionName: connection.Reference.Name,
		OperationID: llm.TextGenerationOperationID, FlowType: flowType, StepType: stepType,
	}
}

// writeLocalConfiguration saves a model pick for the picked-model Step only, as Dex Web's model picker would.
func (run dexScenarioRun) writeLocalConfiguration(t *testing.T, connection FakeConnection, flowType string) *localconfig.Store {
	t.Helper()
	directory := t.TempDir()
	connectionsPath := filepath.Join(directory, "connections.json")
	writeJSONFile(t, connectionsPath, map[string]any{
		"schemaVersion": localconfig.SchemaVersion,
		"connections": []any{map[string]any{
			"connectorId": run.suite.ConnectorID, "connectionName": connection.Reference.Name,
			"modulePath": "example.test/llmtest/" + run.suite.ConnectorID, "moduleVersion": "v0.0.0",
			"provider": connection.Reference.Provider, "configuration": map[string]any{"model": connection.Model},
			"credentials": map[string]any{},
		}},
	})
	picked := run.configurationReference(connection, flowType, pickedModelStepType)
	writeJSONFile(t, filepath.Join(directory, localconfig.UseConfigurationsFileName), map[string]any{
		"schemaVersion": localconfig.UseConfigurationsSchemaVersion,
		"operationConfigurations": []any{map[string]any{
			"connectorId": picked.ConnectorID, "connectionName": picked.ConnectionName, "operationId": picked.OperationID,
			"flowType": picked.FlowType, "stepType": picked.StepType,
			"configuration": map[string]any{"model": run.suite.Dialect.AlternateModel},
		}},
	})
	store, err := localconfig.LoadFile(connectionsPath)
	require.NoError(t, err)
	return store
}

func (run dexScenarioRun) requestModel(t *testing.T, request RecordedRequest) string {
	t.Helper()
	model, err := run.suite.Dialect.ReadRequestModel(request)
	require.NoError(t, err)
	return model
}

// stepModelPick is the configuration a model picker unit saves for one Step.
type stepModelPick struct {
	Model string `json:"model"`
}

// loadStepModelPick loads a Step's pick as an application does; no saved pick inherits the connection model.
func loadStepModelPick(store *localconfig.Store, reference sdkgo.ConnectorConfigurationRef) (string, error) {
	loaded, err := localconfig.LoadOperationConfiguration[stepModelPick](store, reference)
	if errors.Is(err, localconfig.ErrConfigurationNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return loaded.Value.Model, nil
}

func requestWithModel(model string) func(DexScenarioInput) llm.TextGenerationRequest {
	return func(input DexScenarioInput) llm.TextGenerationRequest {
		return llm.TextGenerationRequest{
			Model: model, Messages: []llm.Message{{Role: llm.MessageRoleUser, Text: input.Prompt}},
		}
	}
}

func scenarioAnnotations(explanation string) sdkgo.StepAnnotations {
	return sdkgo.StepAnnotations{GroupID: "llmtest", GroupLabel: "llmtest scenarios", Explanation: explanation}
}

func uniqueFlowID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

// canonicalDialectModel trims the model the way both model-ID rules do; dialect models carry no "models/" prefix.
func canonicalDialectModel(model string) string {
	return strings.TrimSpace(model)
}

func writeJSONFile(t *testing.T, path string, value any) {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, encoded, 0o600))
}

// scenarioFlow is one scenario's Flow with a unique Flow type.
type scenarioFlow struct {
	dex.FlowDefaults
	flowType string
	steps    []dex.StepDef
}

func newScenarioFlow(flowType string, steps ...dex.StepDef) *scenarioFlow {
	return &scenarioFlow{flowType: flowType, steps: steps}
}

// GetFlowType returns the scenario's unique Flow type.
func (flow *scenarioFlow) GetFlowType() string { return flow.flowType }

// GetSteps returns the scenario's Steps.
func (flow *scenarioFlow) GetSteps() []dex.StepDef { return flow.steps }

// GetPersistenceSchema registers the scenario Attribute and text Stream.
func (*scenarioFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{
		Attributes: []dex.AttributeDef{scenarioPickedModelResult},
		Streams:    []dex.StreamDef{scenarioTextStream},
	}
}

type completeWithGenerationStep struct {
	dex.StepDefaultsNoWaitFor[llm.TextGenerationResult]
}

// Execute completes the Flow with the generateText Result.
func (completeWithGenerationStep) Execute(_ dex.Context, result llm.TextGenerationResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete([]llm.TextGenerationResult{result}), nil
}

type rememberPickedModelResultStep struct {
	dex.StepDefaultsNoWaitFor[llm.TextGenerationResult]
}

// Execute keeps the first Result and runs the inherited-model Step.
func (rememberPickedModelResultStep) Execute(ctx dex.Context, result llm.TextGenerationResult) (*dex.StepDecision, error) {
	if err := scenarioPickedModelResult.Set(ctx, result); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[DexScenarioInput](inheritedModelStepType), DexScenarioInput{Prompt: scenarioPrompt}), nil
}

type completeModelPrecedenceStep struct {
	dex.StepDefaultsNoWaitFor[llm.TextGenerationResult]
}

// Execute completes the Flow with both Results in Step order.
func (completeModelPrecedenceStep) Execute(ctx dex.Context, inherited llm.TextGenerationResult) (*dex.StepDecision, error) {
	picked, err := scenarioPickedModelResult.Get(ctx)
	if err != nil {
		return nil, err
	}
	return dex.GracefulComplete([]llm.TextGenerationResult{picked, inherited}), nil
}

// scenarioHarness owns one scenario's Registry, Worker, and Client.
type scenarioHarness struct {
	registry      *dex.Registry
	cache         *blobcache.Cache
	serverAddress string
	workerAddress string
	worker        *dex.Worker
	workerResult  chan error
	client        *dex.Client
}

func newScenarioHarness(t *testing.T, serverAddress string, flow dex.Flow) *scenarioHarness {
	t.Helper()
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	workerAddress := listener.Addr().String()
	require.NoError(t, listener.Close())
	harness := &scenarioHarness{registry: registry, cache: cache, serverAddress: serverAddress, workerAddress: workerAddress}
	harness.client, err = dex.NewClient(registry, cache, dex.ClientOptions{
		FlowServiceAddress: serverAddress, WorkerTarget: &dex.WorkerTarget{Address: workerAddress},
	})
	require.NoError(t, err)
	harness.startWorker(t)
	t.Cleanup(func() { harness.stop(t) })
	return harness
}

func (harness *scenarioHarness) startWorker(t *testing.T) {
	t.Helper()
	worker, err := dex.NewWorker(harness.registry, harness.cache, dex.WorkerOptions{
		BindAddress: harness.workerAddress, FlowServiceAddress: harness.serverAddress,
		WorkerTarget: dex.WorkerTarget{Address: harness.workerAddress},
	})
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	harness.worker, harness.workerResult = worker, workerResult
}

// replaceWorker force-stops the Worker without draining its handlers, then starts a new one at the same address.
func (harness *scenarioHarness) replaceWorker(t *testing.T) {
	t.Helper()
	expired, cancel := context.WithCancel(context.Background())
	cancel()
	// A forced stop returns the expired context's error; the lost handler is the point.
	_ = harness.worker.Stop(expired)
	require.NoError(t, <-harness.workerResult)
	harness.startWorker(t)
}

func (harness *scenarioHarness) stop(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	require.NoError(t, errors.Join(harness.worker.Stop(ctx), <-harness.workerResult))
	require.NoError(t, errors.Join(harness.client.Close(), harness.cache.Close()))
}

// runFlow starts the Flow and waits, across server long-poll caps, until it closes.
func (harness *scenarioHarness) runFlow(t *testing.T, flow dex.Flow, flowID string) dex.FlowResult {
	t.Helper()
	harness.startFlow(t, flow, flowID)
	return harness.waitForFlow(t, flowID)
}

func (harness *scenarioHarness) startFlow(t *testing.T, flow dex.Flow, flowID string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := harness.client.StartFlow(ctx, flow, flowID, DexScenarioInput{Prompt: scenarioPrompt}, dex.StartFlowOptions{})
	require.NoError(t, err, "start Flow %s on the Dex Server at %s", flowID, harness.serverAddress)
}

func (harness *scenarioHarness) waitForFlow(t *testing.T, flowID string) dex.FlowResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
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

func (harness *scenarioHarness) requireCompletedResults(t *testing.T, flow dex.Flow, flowID string) []llm.TextGenerationResult {
	t.Helper()
	result := harness.runFlow(t, flow, flowID)
	require.Equal(t, dex.FlowCompleted, result.Status, "Flow %s: %s", flowID, result.ErrorMessage)
	var results []llm.TextGenerationResult
	require.NoError(t, result.DecodeSingleOutput(&results))
	require.NotEmpty(t, results)
	return results
}

// readText joins the text Stream's messages oldest first.
func (harness *scenarioHarness) readText(flowID string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var newestFirst []string
	pageToken := ""
	for {
		var page dex.StreamMessagesPage[string]
		if err := harness.client.ListStreamMessages(ctx, flowID, scenarioTextStream, 100, pageToken, &page); err != nil {
			return "", err
		}
		for _, message := range page.Messages {
			newestFirst = append(newestFirst, message.Value)
		}
		if page.NextPageToken == "" {
			break
		}
		pageToken = page.NextPageToken
	}
	var text strings.Builder
	for index := len(newestFirst) - 1; index >= 0; index-- {
		text.WriteString(newestFirst[index])
	}
	return text.String(), nil
}
