//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gemini_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	gemini "github.com/superdurable/dex-connectors-library/connectors/google/gemini"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	probeConnectionName = "gemini-integration"
	// silentGeneration is longer than the ten-second heartbeat override below
	// and shorter than the connector's 300-second heartbeat default.
	silentGeneration = 15 * time.Second
	// crashRecoveryBound is far below the 300-second heartbeat default, so a recovery that waited for it fails.
	crashRecoveryBound = 60 * time.Second
)

// probeFlow runs one generateContent Query as its start Step.
type probeFlow struct {
	dex.FlowDefaults
	flowType   string
	connection gemini.Connection
	override   *dex.StepOptions
}

func (flow *probeFlow) GetFlowType() string { return flow.flowType }

func (*probeFlow) GetPersistenceSchema() dex.PersistenceSchema { return dex.PersistenceSchema{} }

func (flow *probeFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(gemini.NewGenerateContentStep(gemini.GenerateContentStepConfig[string]{
			StepType: "GenerateProbe", ConnectionName: probeConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "gemini", GroupLabel: "Gemini", Explanation: "Generate probe text with Gemini."},
			Connection:  flow.connection,
			MapToOperationInput: func(prompt string) gemini.GenerateContentRequest {
				return gemini.GenerateContentRequest{Model: "gemini-2.5-flash", Contents: userPrompt(prompt)}
			},
			Generated:           sdkgo.GoTo(probeCompleted{}),
			StepOptionsOverride: flow.override,
		})),
		dex.DefineStep(probeCompleted{}),
	}
}

type probeCompleted struct {
	dex.StepDefaultsNoWaitFor[gemini.GenerateContentResult]
}

func (probeCompleted) Execute(_ dex.Context, result gemini.GenerateContentResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(result.Value.Text), nil
}

// probeProvider answers every prompt's first request according to its prefix and every later request at once.
type probeProvider struct {
	*httptest.Server
	mutex    sync.Mutex
	attempts map[string][]time.Time
}

func newProbeProvider(t *testing.T) *probeProvider {
	t.Helper()
	provider := &probeProvider{attempts: map[string][]time.Time{}}
	provider.Server = httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *probeProvider) serveHTTP(response http.ResponseWriter, request *http.Request) {
	contents, err := io.ReadAll(request.Body)
	var body struct {
		Contents []struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"contents"`
	}
	if err != nil || json.Unmarshal(contents, &body) != nil || len(body.Contents) == 0 || len(body.Contents[0].Parts) == 0 {
		http.Error(response, "invalid", http.StatusBadRequest)
		return
	}
	prompt := body.Contents[0].Parts[0].Text
	provider.mutex.Lock()
	provider.attempts[prompt] = append(provider.attempts[prompt], time.Now())
	attempt := len(provider.attempts[prompt])
	provider.mutex.Unlock()
	response.Header().Set("Content-Type", "application/json")
	if attempt == 1 {
		switch {
		case strings.HasPrefix(prompt, "SILENT "):
			select {
			case <-time.After(silentGeneration):
			case <-request.Context().Done():
				return
			}
		case strings.HasPrefix(prompt, "HANG "):
			<-request.Context().Done()
			return
		case strings.HasPrefix(prompt, "RATE "):
			response.WriteHeader(http.StatusTooManyRequests)
			_, _ = response.Write([]byte(`{"error":{"code":429,"status":"RESOURCE_EXHAUSTED","details":[` +
				`{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"3s"}]}}`))
			return
		}
	}
	_, _ = response.Write([]byte(stopResponse("attempt " + strconv.Itoa(attempt))))
}

func (provider *probeProvider) attemptsFor(prompt string) []time.Time {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return append([]time.Time(nil), provider.attempts[prompt]...)
}

type probeHarness struct {
	registry      *dex.Registry
	cache         *blobcache.Cache
	serverAddress string
	workerAddress string
	worker        *dex.Worker
	workerResult  chan error
	client        *dex.Client
}

func newProbeHarness(t *testing.T, flows ...dex.Flow) *probeHarness {
	t.Helper()
	registry, err := dex.NewRegistry(flows)
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	workerAddress := listener.Addr().String()
	require.NoError(t, listener.Close())
	serverAddress := os.Getenv("DEX_FLOW_SERVICE_ADDRESS")
	if serverAddress == "" {
		serverAddress = "127.0.0.1:8801"
	}
	harness := &probeHarness{registry: registry, cache: cache, serverAddress: serverAddress, workerAddress: workerAddress}
	harness.client, err = dex.NewClient(registry, cache, dex.ClientOptions{
		FlowServiceAddress: serverAddress, WorkerTarget: &dex.WorkerTarget{Address: workerAddress},
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

func (harness *probeHarness) startWorker(t *testing.T) {
	t.Helper()
	worker, err := dex.NewWorker(harness.registry, harness.cache, dex.WorkerOptions{
		BindAddress: harness.workerAddress, FlowServiceAddress: harness.serverAddress,
		WorkerTarget: dex.WorkerTarget{Address: harness.workerAddress},
	})
	require.NoError(t, err)
	harness.worker = worker
	harness.workerResult = make(chan error, 1)
	go func() { harness.workerResult <- worker.Start() }()
}

func (harness *probeHarness) stopWorker(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	require.NoError(t, errors.Join(harness.worker.Stop(ctx), <-harness.workerResult))
	harness.worker = nil
	harness.workerResult = nil
}

func probeConnection(t *testing.T, endpoint string) gemini.Connection {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "google", Name: probeConnectionName}
	client, err := gemini.New(gemini.Config{Endpoint: endpoint}, sdkgo.StaticCredentialProvider[gemini.Credentials]{
		reference: {APIKey: sdkgo.NewSecretString(testAPIKey)},
	})
	require.NoError(t, err)
	connection, err := gemini.NewConnection(client, reference)
	require.NoError(t, err)
	return connection
}

func startProbe(t *testing.T, ctx context.Context, harness *probeHarness, flow dex.Flow, flowID string, prompt string) {
	t.Helper()
	_, err := harness.client.StartFlow(ctx, flow, flowID, prompt, dex.StartFlowOptions{})
	require.NoError(t, err)
}

func waitForProbe(t *testing.T, ctx context.Context, harness *probeHarness, flowID string) string {
	t.Helper()
	result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var text string
	require.NoError(t, result.DecodeSingleOutput(&text))
	return text
}

func TestGenerateContentStepDefaultsWithRealDex(t *testing.T) {
	provider := newProbeProvider(t)
	defaultFlow, shortHeartbeatFlow := probeFlows(probeConnection(t, provider.URL))
	harness := newProbeHarness(t, defaultFlow, shortHeartbeatFlow)
	harness.startWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	testRunID := strconv.FormatInt(time.Now().UnixNano(), 10)

	// Both silent generations run concurrently so the test waits for them once.
	defaultPrompt := "SILENT default " + testRunID
	shortPrompt := "SILENT short-heartbeat " + testRunID
	startProbe(t, ctx, harness, defaultFlow, "gemini-default-heartbeat-"+testRunID, defaultPrompt)
	startProbe(t, ctx, harness, shortHeartbeatFlow, "gemini-short-heartbeat-"+testRunID, shortPrompt)

	t.Run("a silent generation longer than a short heartbeat is retried", func(t *testing.T) {
		require.Equal(t, "attempt 2", waitForProbe(t, ctx, harness, "gemini-short-heartbeat-"+testRunID))
		require.Len(t, provider.attemptsFor(shortPrompt), 2,
			"a non-streaming call sends no heartbeat, so the ten-second override abandons the first attempt")
	})

	t.Run("the connector heartbeat default outlives a silent generation", func(t *testing.T) {
		require.Equal(t, "attempt 1", waitForProbe(t, ctx, harness, "gemini-default-heartbeat-"+testRunID))
		require.Len(t, provider.attemptsFor(defaultPrompt), 1)
	})

	t.Run("a provider RetryInfo delay schedules the Dex retry", func(t *testing.T) {
		prompt := "RATE " + testRunID
		flowID := "gemini-retry-info-" + testRunID
		startProbe(t, ctx, harness, defaultFlow, flowID, prompt)
		require.Equal(t, "attempt 2", waitForProbe(t, ctx, harness, flowID))
		attempts := provider.attemptsFor(prompt)
		require.Len(t, attempts, 2)
		require.GreaterOrEqual(t, attempts[1].Sub(attempts[0]), 2900*time.Millisecond,
			"the 3-second RetryInfo delay replaces the 2-second initial retry interval")
	})

	t.Run("a replacement Worker retries a generation interrupted by a Worker crash", func(t *testing.T) {
		// Worker.Stop drains in-flight calls, so a separate Worker process is killed to model a crash.
		harness.stopWorker(t)
		process := startProbeWorkerProcess(t, harness, provider.URL)
		prompt := "HANG " + testRunID
		flowID := "gemini-worker-crash-" + testRunID
		startProbe(t, ctx, harness, defaultFlow, flowID, prompt)
		require.Eventually(t, func() bool { return len(provider.attemptsFor(prompt)) == 1 }, 30*time.Second, 50*time.Millisecond)
		require.NoError(t, process.Process.Kill())
		_ = process.Wait()
		killedAt := time.Now()
		harness.startWorker(t)
		// The 300-second heartbeat default is safe only because Dex detects a Worker crash without waiting for it.
		require.Less(t, crashRecoveryBound, gemini.GenerateContentDefinition.StepDefaults.HeartbeatTimeout)
		recoveryCtx, cancelRecovery := context.WithTimeout(ctx, crashRecoveryBound)
		defer cancelRecovery()
		require.Equal(t, "attempt 2", waitForProbe(t, recoveryCtx, harness, flowID),
			"a Worker crash must not wait for the %s heartbeat timeout", gemini.GenerateContentDefinition.StepDefaults.HeartbeatTimeout)
		recovery := time.Since(killedAt)
		require.Len(t, provider.attemptsFor(prompt), 2)
		require.Less(t, recovery, crashRecoveryBound, "a Worker crash must not wait for the heartbeat timeout")
		t.Logf("replacement Worker completed the interrupted generation %s after the crash", recovery.Round(time.Second))
	})
}

const (
	probeWorkerAddressVariable  = "GEMINI_PROBE_WORKER_ADDRESS"
	probeProviderURLVariable    = "GEMINI_PROBE_PROVIDER_URL"
	probeServerAddressVariable  = "GEMINI_PROBE_SERVER_ADDRESS"
	probeBlobDirectoryVariable  = "GEMINI_PROBE_BLOB_DIRECTORY"
	probeShortHeartbeatFlowType = "GeminiShortHeartbeatProbe"
)

func TestMain(m *testing.M) {
	if address := os.Getenv(probeWorkerAddressVariable); address != "" {
		os.Exit(runProbeWorkerProcess(address))
	}
	os.Exit(m.Run())
}

func probeFlows(connection gemini.Connection) (*probeFlow, *probeFlow) {
	return &probeFlow{flowType: "GeminiDefaultOptionsProbe", connection: connection},
		&probeFlow{
			flowType: probeShortHeartbeatFlowType, connection: connection,
			override: &dex.StepOptions{
				HeartbeatTimeout: 10 * time.Second,
				ExecuteRetry: &dex.RetryPolicy{
					InitialInterval: time.Second, BackoffCoefficient: 1, MaximumInterval: time.Second,
					MaximumAttempts: 3, TotalDuration: 2 * time.Minute,
				},
			},
		}
}

// runProbeWorkerProcess serves the probe Flows until the parent test kills this process.
func runProbeWorkerProcess(address string) int {
	reference := sdkgo.ConnectionRef{Provider: "google", Name: probeConnectionName}
	client, err := gemini.New(gemini.Config{Endpoint: os.Getenv(probeProviderURLVariable)}, sdkgo.StaticCredentialProvider[gemini.Credentials]{
		reference: {APIKey: sdkgo.NewSecretString(testAPIKey)},
	})
	if err != nil {
		return 2
	}
	connection, err := gemini.NewConnection(client, reference)
	if err != nil {
		return 2
	}
	defaultFlow, shortHeartbeatFlow := probeFlows(connection)
	registry, err := dex.NewRegistry([]dex.Flow{defaultFlow, shortHeartbeatFlow})
	if err != nil {
		return 2
	}
	cache, err := blobcache.New(&blobcache.Config{Dir: os.Getenv(probeBlobDirectoryVariable), MaxBytes: 64 << 20})
	if err != nil {
		return 2
	}
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{
		BindAddress: address, FlowServiceAddress: os.Getenv(probeServerAddressVariable),
		WorkerTarget: dex.WorkerTarget{Address: address},
	})
	if err != nil {
		return 2
	}
	if err := worker.Start(); err != nil {
		return 1
	}
	return 0
}

func startProbeWorkerProcess(t *testing.T, harness *probeHarness, providerURL string) *exec.Cmd {
	t.Helper()
	process := exec.Command(os.Args[0], "-test.run=^$")
	process.Env = append(os.Environ(),
		probeWorkerAddressVariable+"="+harness.workerAddress,
		probeProviderURLVariable+"="+providerURL,
		probeServerAddressVariable+"="+harness.serverAddress,
		probeBlobDirectoryVariable+"="+filepath.Join(t.TempDir(), "worker-blobs"),
	)
	process.Stdout = os.Stderr
	process.Stderr = os.Stderr
	require.NoError(t, process.Start())
	t.Cleanup(func() {
		if process.ProcessState == nil {
			_ = process.Process.Kill()
			_ = process.Wait()
		}
	})
	require.Eventually(t, func() bool {
		connection, err := net.DialTimeout("tcp", harness.workerAddress, 100*time.Millisecond)
		if err != nil {
			return false
		}
		_ = connection.Close()
		return true
	}, 20*time.Second, 50*time.Millisecond, "the Worker process must listen before the Flow starts")
	return process
}
