// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//go:build darwin || linux

package projectconfig_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

const (
	// refreshHelperEnvironment makes the test binary run TestDexWebRefreshHelperProcess as a helper process.
	refreshHelperEnvironment = "PROJECTCONFIG_REFRESH_HELPER"
	// tokenEndpointEnvironment holds the URL of the parent's fake OAuth token endpoint.
	tokenEndpointEnvironment = "PROJECTCONFIG_REFRESH_TOKEN_ENDPOINT"
	// initialRefreshToken is the refresh token that writeDexWebFiles stores.
	initialRefreshToken = "refresh-1"
	helperReportTimeout = 60 * time.Second
)

// TestDexWebRefreshHelperProcess runs a helper process for the multi-process tests; otherwise it does nothing.
func TestDexWebRefreshHelperProcess(t *testing.T) {
	if os.Getenv(refreshHelperEnvironment) != "1" {
		return
	}
	// Exiting here keeps the testing package's summary out of the report stream.
	os.Exit(runRefreshHelper(os.Stdin, os.Stdout))
}

// helperCommand asks a helper to resolve credentials of at least TargetGeneration.
type helperCommand struct {
	TargetGeneration int `json:"targetGeneration"`
}

// helperReport is one line a helper writes: readiness, or the outcome of one command.
type helperReport struct {
	IsReady                   bool   `json:"isReady,omitempty"`
	AccessToken               string `json:"accessToken,omitempty"`
	RefreshToken              string `json:"refreshToken,omitempty"`
	IsRefreshCalled           bool   `json:"isRefreshCalled,omitempty"`
	IsReauthorizationRequired bool   `json:"isReauthorizationRequired,omitempty"`
	Error                     string `json:"error,omitempty"`
}

// runRefreshHelper loads DEX_CONNECTOR_CONFIG_FILE as an application does and resolves credentials once per command.
func runRefreshHelper(commands io.Reader, reports io.Writer) int {
	encoder := json.NewEncoder(reports)
	project, err := projectconfig.LoadFromEnvironment(context.Background())
	if err != nil {
		_ = encoder.Encode(helperReport{Error: err.Error()})
		return 2
	}
	resolver, err := projectconfig.NewCredentialResolver(project.Connections, localKey, decodeLocalCredentials, encodeLocalCredentials)
	if err != nil {
		_ = encoder.Encode(helperReport{Error: err.Error()})
		return 2
	}
	driver := &tokenEndpointDriver{endpoint: os.Getenv(tokenEndpointEnvironment)}
	if err = encoder.Encode(helperReport{IsReady: true}); err != nil {
		return 2
	}
	scanner := bufio.NewScanner(commands)
	for scanner.Scan() {
		var command helperCommand
		if err = json.Unmarshal(scanner.Bytes(), &command); err != nil {
			return 2
		}
		driver.targetGeneration, driver.isRefreshCalled = command.TargetGeneration, false
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		credentials, err := resolver.ResolveWithRefresh(ctx, driver)
		cancel()
		report := helperReport{AccessToken: credentials.AccessToken, RefreshToken: credentials.RefreshToken, IsRefreshCalled: driver.isRefreshCalled}
		if err != nil {
			report.Error = err.Error()
			report.IsReauthorizationRequired = errors.Is(err, projectconfig.ErrReauthorizationRequired)
		}
		if err = encoder.Encode(report); err != nil {
			return 2
		}
	}
	return 0
}

// tokenEndpointDriver refreshes through the fake endpoint until access-<generation> reaches the commanded generation.
type tokenEndpointDriver struct {
	endpoint         string
	targetGeneration int
	isRefreshCalled  bool
}

func (driver *tokenEndpointDriver) RefreshRequired(state projectconfig.RefreshState[localCredentials]) bool {
	return accessTokenGeneration(state.Credentials.AccessToken) < driver.targetGeneration
}

func (driver *tokenEndpointDriver) Refresh(ctx context.Context, state projectconfig.RefreshState[localCredentials]) (projectconfig.RefreshResult[localCredentials], error) {
	driver.isRefreshCalled = true
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {state.Credentials.RefreshToken}}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, driver.endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return projectconfig.RefreshResult[localCredentials]{}, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return projectconfig.RefreshResult[localCredentials]{}, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusBadRequest {
		return projectconfig.RefreshResult[localCredentials]{}, fmt.Errorf("%w: the token endpoint answered invalid_grant", projectconfig.ErrReauthorizationRequired)
	}
	var token fakeTokenResponse
	if response.StatusCode != http.StatusOK || json.NewDecoder(response.Body).Decode(&token) != nil {
		return projectconfig.RefreshResult[localCredentials]{}, fmt.Errorf("token endpoint status %d", response.StatusCode)
	}
	return projectconfig.RefreshResult[localCredentials]{
		Credentials: localCredentials{AccessToken: token.AccessToken, RefreshToken: token.RefreshToken},
		ExpiresAt:   state.Now.Add(time.Duration(token.ExpiresIn) * time.Second),
	}, nil
}

func accessTokenGeneration(accessToken string) int {
	generation, err := strconv.Atoi(strings.TrimPrefix(accessToken, "access-"))
	if err != nil {
		return -1
	}
	return generation
}

type fakeTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

// firstRequestHold makes the fake endpoint keep the first request open until its client dies.
type firstRequestHold int

const (
	holdNoRequest firstRequestHold = iota
	// holdBeforeRotation keeps the first request without processing it.
	holdBeforeRotation
	// holdAfterRotation rotates the refresh token for the first request and then loses the response.
	holdAfterRotation
)

// fakeTokenEndpoint rotates refresh tokens and answers invalid_grant for an earlier one, like a rotating provider.
type fakeTokenEndpoint struct {
	latency             time.Duration
	hold                firstRequestHold
	firstRequestArrived chan struct{}
	stopped             chan struct{}

	mu                  sync.Mutex
	currentRefreshToken string
	generation          int
	arrivedRequests     int
	acceptedRequests    int
	rejectedRequests    int
}

func newFakeTokenEndpoint(t *testing.T, latency time.Duration, hold firstRequestHold) (*fakeTokenEndpoint, string) {
	t.Helper()
	endpoint := &fakeTokenEndpoint{
		latency: latency, hold: hold, firstRequestArrived: make(chan struct{}, 1), stopped: make(chan struct{}),
		currentRefreshToken: initialRefreshToken,
	}
	server := httptest.NewServer(endpoint)
	t.Cleanup(server.Close)
	// Cleanups run last-in first-out, so a held request ends before the server closes.
	t.Cleanup(func() { close(endpoint.stopped) })
	return endpoint, server.URL
}

func (endpoint *fakeTokenEndpoint) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	presented := request.PostFormValue("refresh_token")
	endpoint.mu.Lock()
	isFirstRequest := endpoint.arrivedRequests == 0
	endpoint.arrivedRequests++
	if presented != endpoint.currentRefreshToken {
		endpoint.rejectedRequests++
		endpoint.mu.Unlock()
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = writer.Write([]byte(`{"error":"invalid_grant"}`))
		return
	}
	isHeld := isFirstRequest && endpoint.hold != holdNoRequest
	if !isHeld || endpoint.hold == holdAfterRotation {
		endpoint.generation++
		endpoint.acceptedRequests++
		endpoint.currentRefreshToken = fmt.Sprintf("rotated-refresh-%d", endpoint.generation)
	}
	response := fakeTokenResponse{AccessToken: fmt.Sprintf("access-%d", endpoint.generation), RefreshToken: endpoint.currentRefreshToken, ExpiresIn: 3600}
	endpoint.mu.Unlock()
	if isHeld {
		endpoint.firstRequestArrived <- struct{}{}
		select {
		case <-request.Context().Done():
		case <-endpoint.stopped:
		}
		return
	}
	// Provider latency widens the window in which uncoordinated processes would all send a request.
	time.Sleep(endpoint.latency)
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(response)
}

// requestCounts returns the arrived, accepted and rejected request counts.
func (endpoint *fakeTokenEndpoint) requestCounts() (arrived, accepted, rejected int) {
	endpoint.mu.Lock()
	defer endpoint.mu.Unlock()
	return endpoint.arrivedRequests, endpoint.acceptedRequests, endpoint.rejectedRequests
}

// refreshHelper is one helper process: an application that shares connections.json with the others.
type refreshHelper struct {
	command *exec.Cmd
	stdin   io.WriteCloser
	reports chan helperReport
}

func startRefreshHelper(t *testing.T, connectionsPath, endpointURL string) *refreshHelper {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestDexWebRefreshHelperProcess$", "-test.count=1")
	for _, variable := range os.Environ() {
		if !strings.HasPrefix(variable, "DEX_PROJECT_") && !strings.HasPrefix(variable, projectconfig.LocalConnectionsFileEnvironment+"=") {
			command.Env = append(command.Env, variable)
		}
	}
	command.Env = append(command.Env, refreshHelperEnvironment+"=1", projectconfig.LocalConnectionsFileEnvironment+"="+connectionsPath, tokenEndpointEnvironment+"="+endpointURL)
	command.Stderr = os.Stderr
	stdin, err := command.StdinPipe()
	require.NoError(t, err)
	stdout, err := command.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, command.Start())
	helper := &refreshHelper{command: command, stdin: stdin, reports: make(chan helperReport, 8)}
	go helper.readReports(stdout)
	t.Cleanup(helper.stop)
	ready := helper.nextReport(t)
	require.True(t, ready.IsReady, "helper failed to start: %s", ready.Error)
	return helper
}

func (helper *refreshHelper) readReports(stdout io.Reader) {
	defer close(helper.reports)
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		var report helperReport
		if json.Unmarshal(scanner.Bytes(), &report) == nil {
			helper.reports <- report
		}
	}
}

func (helper *refreshHelper) resolve(t *testing.T, targetGeneration int) {
	t.Helper()
	line, err := json.Marshal(helperCommand{TargetGeneration: targetGeneration})
	require.NoError(t, err)
	_, err = helper.stdin.Write(append(line, '\n'))
	require.NoError(t, err)
}

func (helper *refreshHelper) nextReport(t *testing.T) helperReport {
	t.Helper()
	select {
	case report, isOpen := <-helper.reports:
		require.True(t, isOpen, "helper process exited before reporting")
		return report
	case <-time.After(helperReportTimeout):
		require.FailNow(t, "helper process did not report")
		return helperReport{}
	}
}

// kill ends the helper with SIGKILL, so it runs no deferred code and releases nothing itself.
func (helper *refreshHelper) kill(t *testing.T) {
	t.Helper()
	require.NoError(t, helper.command.Process.Kill())
	_ = helper.command.Wait() // A killed process reports its signal as an error.
}

func (helper *refreshHelper) stop() {
	_ = helper.stdin.Close() // The helper exits when its command stream ends.
	done := make(chan struct{})
	go func() {
		_ = helper.command.Wait() // The exit status of a stopped helper is irrelevant.
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		_ = helper.command.Process.Kill() // A helper that ignores end of input is killed.
		<-done
	}
}

func readFileCredentials(t *testing.T, path string) localCredentials {
	t.Helper()
	var file struct {
		Connections []struct {
			Credentials localCredentials `json:"credentials"`
		} `json:"connections"`
	}
	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(contents, &file))
	require.Len(t, file.Connections, 1)
	return file.Connections[0].Credentials
}

func TestDexWebRefreshAcrossProcessesSendsOneProviderRequest(t *testing.T) {
	const helperCount = 4
	future := time.Now().Add(time.Hour)
	path := writeDexWebFiles(t, t.TempDir(), "access-0", &future)
	endpoint, endpointURL := newFakeTokenEndpoint(t, 200*time.Millisecond, holdNoRequest)
	helpers := make([]*refreshHelper, helperCount)
	for index := range helpers {
		helpers[index] = startRefreshHelper(t, path, endpointURL)
	}

	// Every process needs generation 1 at once; only one may send the rotating refresh request.
	for _, helper := range helpers {
		helper.resolve(t, 1)
	}
	var adopter *refreshHelper
	refreshers := 0
	for _, helper := range helpers {
		report := helper.nextReport(t)
		require.Empty(t, report.Error)
		require.Equal(t, localCredentials{AccessToken: "access-1", RefreshToken: "rotated-refresh-1"}, localCredentials{AccessToken: report.AccessToken, RefreshToken: report.RefreshToken})
		if report.IsRefreshCalled {
			refreshers++
		} else {
			adopter = helper
		}
	}
	arrived, accepted, rejected := endpoint.requestCounts()
	require.Equal(t, [3]int{1, 1, 0}, [3]int{arrived, accepted, rejected})
	require.Equal(t, 1, refreshers)
	require.Equal(t, localCredentials{AccessToken: "access-1", RefreshToken: "rotated-refresh-1"}, readFileCredentials(t, path))

	// A process that adopted the rotated refresh token from the file refreshes next with it, which the provider accepts.
	adopter.resolve(t, 2)
	report := adopter.nextReport(t)
	require.Empty(t, report.Error)
	require.True(t, report.IsRefreshCalled)
	require.Equal(t, "access-2", report.AccessToken)
	arrived, accepted, rejected = endpoint.requestCounts()
	require.Equal(t, [3]int{2, 2, 0}, [3]int{arrived, accepted, rejected})

	for _, helper := range helpers {
		if helper != adopter {
			helper.resolve(t, 2)
		}
	}
	for _, helper := range helpers {
		if helper == adopter {
			continue
		}
		report = helper.nextReport(t)
		require.Empty(t, report.Error)
		require.False(t, report.IsRefreshCalled)
		require.Equal(t, localCredentials{AccessToken: "access-2", RefreshToken: "rotated-refresh-2"}, localCredentials{AccessToken: report.AccessToken, RefreshToken: report.RefreshToken})
	}
	arrived, accepted, rejected = endpoint.requestCounts()
	require.Equal(t, [3]int{2, 2, 0}, [3]int{arrived, accepted, rejected})
	require.Equal(t, localCredentials{AccessToken: "access-2", RefreshToken: "rotated-refresh-2"}, readFileCredentials(t, path))
}

func TestDexWebRefreshLockIsReleasedWhenItsHolderCrashes(t *testing.T) {
	future := time.Now().Add(time.Hour)
	path := writeDexWebFiles(t, t.TempDir(), "access-0", &future)
	endpoint, endpointURL := newFakeTokenEndpoint(t, 0, holdBeforeRotation)
	crashing := startRefreshHelper(t, path, endpointURL)
	survivor := startRefreshHelper(t, path, endpointURL)

	crashing.resolve(t, 1)
	select {
	case <-endpoint.firstRequestArrived:
	case <-time.After(helperReportTimeout):
		require.FailNow(t, "the first refresh request did not arrive")
	}
	survivor.resolve(t, 1)
	// The elapsed time is the behavior: while the holder lives, the survivor sends nothing.
	time.Sleep(300 * time.Millisecond)
	arrived, _, _ := endpoint.requestCounts()
	require.Equal(t, 1, arrived)

	crashing.kill(t)
	report := survivor.nextReport(t)
	require.Empty(t, report.Error)
	require.True(t, report.IsRefreshCalled)
	require.Equal(t, localCredentials{AccessToken: "access-1", RefreshToken: "rotated-refresh-1"}, localCredentials{AccessToken: report.AccessToken, RefreshToken: report.RefreshToken})
	arrived, accepted, rejected := endpoint.requestCounts()
	require.Equal(t, [3]int{2, 1, 0}, [3]int{arrived, accepted, rejected})
	require.Equal(t, localCredentials{AccessToken: "access-1", RefreshToken: "rotated-refresh-1"}, readFileCredentials(t, path))
}

// A token rotated for a holder that died is lost; the next refresh promptly requires reauthorization.
func TestDexWebRefreshAfterHolderCrashLostRotatedTokenRequiresReauthorization(t *testing.T) {
	future := time.Now().Add(time.Hour)
	path := writeDexWebFiles(t, t.TempDir(), "access-0", &future)
	endpoint, endpointURL := newFakeTokenEndpoint(t, 0, holdAfterRotation)
	crashing := startRefreshHelper(t, path, endpointURL)
	survivor := startRefreshHelper(t, path, endpointURL)

	crashing.resolve(t, 1)
	select {
	case <-endpoint.firstRequestArrived:
	case <-time.After(helperReportTimeout):
		require.FailNow(t, "the first refresh request did not arrive")
	}
	crashing.kill(t)

	survivor.resolve(t, 1)
	report := survivor.nextReport(t)
	require.True(t, report.IsRefreshCalled)
	require.True(t, report.IsReauthorizationRequired, report.Error)
	arrived, accepted, rejected := endpoint.requestCounts()
	require.Equal(t, [3]int{2, 1, 1}, [3]int{arrived, accepted, rejected})
	require.Equal(t, localCredentials{AccessToken: "access-0", RefreshToken: initialRefreshToken}, readFileCredentials(t, path))
}

// countingRotatingDriver refreshes in process, counting provider requests across every store that shares it.
type countingRotatingDriver struct {
	requests atomic.Int32
}

func (*countingRotatingDriver) RefreshRequired(state projectconfig.RefreshState[localCredentials]) bool {
	return accessTokenGeneration(state.Credentials.AccessToken) < 1
}

func (driver *countingRotatingDriver) Refresh(_ context.Context, state projectconfig.RefreshState[localCredentials]) (projectconfig.RefreshResult[localCredentials], error) {
	driver.requests.Add(1)
	// Provider latency keeps the first refresh in flight while the other store asks.
	time.Sleep(100 * time.Millisecond)
	return projectconfig.RefreshResult[localCredentials]{
		Credentials: localCredentials{AccessToken: "access-1", RefreshToken: "rotated-refresh-1"},
		ExpiresAt:   state.Now.Add(time.Hour),
	}, nil
}

// Two projects loaded from one file in one process, as during a reload, also refresh once.
func TestDexWebRefreshAcrossStoresInOneProcessSendsOneProviderRequest(t *testing.T) {
	future := time.Now().Add(time.Hour)
	path := writeDexWebFiles(t, t.TempDir(), "access-0", &future)
	resolvers := []*projectconfig.CredentialResolver[localCredentials]{localResolver(t, loadLocal(t, path)), localResolver(t, loadLocal(t, path))}
	driver := &countingRotatingDriver{}

	results := make([]localCredentials, len(resolvers))
	errs := make([]error, len(resolvers))
	var group sync.WaitGroup
	for index, resolver := range resolvers {
		group.Add(1)
		go func() {
			defer group.Done()
			results[index], errs[index] = resolver.ResolveWithRefresh(context.Background(), driver)
		}()
	}
	group.Wait()
	for index := range resolvers {
		require.NoError(t, errs[index])
		require.Equal(t, localCredentials{AccessToken: "access-1", RefreshToken: "rotated-refresh-1"}, results[index])
	}
	require.Equal(t, int32(1), driver.requests.Load())
}
