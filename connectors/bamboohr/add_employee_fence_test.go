// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bamboohr_test

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/bamboohr"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

type addEmployeeCredentialErrorProvider struct{ resolutionError error }

func (provider addEmployeeCredentialErrorProvider) Resolve(sdkgo.Call) (bamboohr.Credentials, error) {
	return bamboohr.Credentials{}, provider.resolutionError
}

func TestAddEmployeeFenceRetriesOnlyConfirmedNonApplications(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			provider := newRecordingBambooHR(t, func(response http.ResponseWriter, _ *http.Request, index int) {
				if index == 0 {
					writeBambooHRError(t, response, status, ``)
					return
				}
				writeJSON(t, response, http.StatusCreated, `{"id":"140"}`)
			})
			ctx := singledispatchtest.NewContext("bamboohr-flow", "add-retry")
			client := newBambooHRClient(t, provider.URL)
			_, err := sdkgo.RunMutation(ctx, client.AddEmployee(), bambooHRConnection, validAddEmployeeInput())
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			if status == http.StatusServiceUnavailable {
				require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
			} else {
				require.Equal(t, sdkgo.FailureRateLimit, retry.Failure.Kind)
			}
			ctx.AdvanceAttempt()
			result, err := sdkgo.RunMutation(ctx, client.AddEmployee(), bambooHRConnection, validAddEmployeeInput())
			require.NoError(t, err)
			require.Equal(t, bamboohr.AddEmployeeBranchCreated, result.Branch)
			require.Equal(t, "140", result.Value.EmployeeID)
			require.Equal(t, 2, provider.requestCount())
			require.Equal(t, provider.request(0).body, provider.request(1).body)
			require.Equal(t, result.Receipt.CallID, sdkgo.CallID(result.Receipt.IdempotencyKey))
		})
	}
}

func TestAddEmployeeFenceFailsClosedWithoutCurrentPermission(t *testing.T) {
	provider := newRecordingBambooHR(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no provider request is permitted") })
	for _, scenario := range []string{"missing", "legacy", "foreign"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := singledispatchtest.NewContext("bamboohr-flow", "add-fail-closed")
			switch scenario {
			case "legacy":
				require.NoError(t, ctx.SetPersistedCheckpoint(map[string]any{"bamboohrDispatchedCallId": "old-call"}))
			case "foreign":
				require.NoError(t, ctx.SetPersistedCheckpoint(map[string]any{"callId": "foreign", "attempt": 1, "state": "canResend"}))
			}
			ctx.AdvanceAttempt()
			result, err := sdkgo.RunMutation(ctx, newBambooHRClient(t, provider.URL).AddEmployee(), bambooHRConnection, validAddEmployeeInput())
			require.NoError(t, err)
			require.Equal(t, bamboohr.AddEmployeeBranchUncertain, result.Branch)
			require.Zero(t, provider.requestCount())
		})
	}
}

func TestAddEmployeeFenceRejectsAStaleResendCheckpoint(t *testing.T) {
	provider := newRecordingBambooHR(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		response.WriteHeader(http.StatusServiceUnavailable)
	})
	ctx := singledispatchtest.NewContext("bamboohr-flow", "add-stale")
	client := newBambooHRClient(t, provider.URL)
	_, err := sdkgo.RunMutation(ctx, client.AddEmployee(), bambooHRConnection, validAddEmployeeInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	ctx.AdvanceAttempt()
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.AddEmployee(), bambooHRConnection, validAddEmployeeInput())
	require.NoError(t, err)
	require.Equal(t, bamboohr.AddEmployeeBranchUncertain, result.Branch)
	require.Equal(t, 1, provider.requestCount())
}

func TestAddEmployeeFenceDoesNotResendWhenCanResendCheckpointFails(t *testing.T) {
	ctx := singledispatchtest.NewContext("bamboohr-flow", "add-checkpoint-failure")
	provider := newRecordingBambooHR(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		ctx.SetWriteError(errors.New("heartbeat unavailable"))
		response.WriteHeader(http.StatusServiceUnavailable)
	})
	client := newBambooHRClient(t, provider.URL)
	_, err := sdkgo.RunMutation(ctx, client.AddEmployee(), bambooHRConnection, validAddEmployeeInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	ctx.SetWriteError(nil)
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.AddEmployee(), bambooHRConnection, validAddEmployeeInput())
	require.NoError(t, err)
	require.Equal(t, bamboohr.AddEmployeeBranchUncertain, result.Branch)
	require.Equal(t, 1, provider.requestCount())
}

func TestAddEmployeeFenceRejectsTerminalOrUnusableCredentialsBeforeSending(t *testing.T) {
	provider := newRecordingBambooHR(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no provider request is permitted") })
	for name, credentials := range map[string]sdkgo.CredentialProvider[bamboohr.Credentials]{
		"reauthorization required": addEmployeeCredentialErrorProvider{fmt.Errorf("credential refresh: %w", sdkgo.ErrReauthorizationRequired)},
		"connection not selected":  addEmployeeCredentialErrorProvider{projectconfig.ErrConnectionNotSelected},
		"broker rejected":          addEmployeeCredentialErrorProvider{projectconfig.ErrCredentialBrokerRejected},
		"credential missing":       addEmployeeCredentialErrorProvider{projectconfig.ErrObjectNotFound},
		"missing key":              sdkgo.StaticCredentialProvider[bamboohr.Credentials]{bambooHRConnection: {}},
		"unusable key": sdkgo.StaticCredentialProvider[bamboohr.Credentials]{
			bambooHRConnection: {APIKey: sdkgo.NewSecretString("invalid:key")},
		},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := singledispatchtest.NewContext("bamboohr-flow", "add-terminal-credentials")
			client, err := bamboohr.New(bamboohr.Config{CompanyDomain: testCompanyDomain}, credentials,
				bamboohr.WithAPIBaseURL(provider.URL+"/api/v1"))
			require.NoError(t, err)
			result, err := sdkgo.RunMutation(ctx, client.AddEmployee(), bambooHRConnection, validAddEmployeeInput())
			require.NoError(t, err)
			require.Equal(t, bamboohr.AddEmployeeBranchDefect, result.Branch)
			require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
			require.Equal(t, "connection credentials are unavailable", result.Failure.Message)
			require.Zero(t, provider.requestCount())
			require.Len(t, ctx.Heartbeats(), 1)
			require.Contains(t, string(ctx.Heartbeats()[0]), `"state":"notSent"`)
		})
	}
}

func TestAddEmployeeFenceRecordsTransientCredentialFailureBeforeRetry(t *testing.T) {
	provider := newRecordingBambooHR(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusCreated, `{"id":"140"}`)
	})
	ctx := singledispatchtest.NewContext("bamboohr-flow", "add-credentials")
	transientCredentials, err := bamboohr.New(bamboohr.Config{CompanyDomain: testCompanyDomain},
		addEmployeeCredentialErrorProvider{projectconfig.ErrCredentialBrokerUnavailable}, bamboohr.WithAPIBaseURL(provider.URL+"/api/v1"))
	require.NoError(t, err)
	_, err = sdkgo.RunMutation(ctx, transientCredentials.AddEmployee(), bambooHRConnection, validAddEmployeeInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
	require.Zero(t, provider.requestCount())
	require.Len(t, ctx.Heartbeats(), 1)
	require.Contains(t, string(ctx.Heartbeats()[0]), `"state":"notSent"`)
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, newBambooHRClient(t, provider.URL).AddEmployee(), bambooHRConnection, validAddEmployeeInput())
	require.NoError(t, err)
	require.Equal(t, bamboohr.AddEmployeeBranchCreated, result.Branch)
	require.Equal(t, 1, provider.requestCount())
}

func TestAddEmployeeFenceReadsBackAcceptedIDWithoutResending(t *testing.T) {
	for _, readStatus := range []int{http.StatusOK, http.StatusNotFound, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(readStatus), func(t *testing.T) {
			provider := newRecordingBambooHR(t, func(response http.ResponseWriter, request *http.Request, _ int) {
				if request.Method == http.MethodPost {
					writeJSON(t, response, http.StatusCreated, `{"id":"140"}`)
					return
				}
				writeJSON(t, response, readStatus, `{"id":"140"}`)
			})
			ctx := singledispatchtest.NewContext("bamboohr-flow", "add-readback")
			client := newBambooHRClient(t, provider.URL)
			_, err := sdkgo.RunMutation(ctx, client.AddEmployee(), bambooHRConnection, validAddEmployeeInput())
			require.NoError(t, err)
			ctx.AdvanceAttempt()
			result, err := sdkgo.RunMutation(ctx, client.AddEmployee(), bambooHRConnection, validAddEmployeeInput())
			if readStatus == http.StatusServiceUnavailable {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
			} else {
				require.NoError(t, err)
				if readStatus == http.StatusOK {
					require.Equal(t, bamboohr.AddEmployeeBranchCreated, result.Branch)
				} else {
					require.Equal(t, bamboohr.AddEmployeeBranchUncertain, result.Branch)
				}
			}
			require.Equal(t, 2, provider.requestCount())
			require.Equal(t, http.MethodGet, provider.request(1).method)
		})
	}
}

func TestAddEmployeeFenceTreatsUnusableAcceptedBodyAsAccepted(t *testing.T) {
	for _, test := range []struct {
		name  string
		reply func(http.ResponseWriter)
	}{
		{name: "no ID", reply: func(response http.ResponseWriter) { writeJSON(t, response, http.StatusCreated, `{"firstName":"Ava"}`) }},
		{name: "malformed JSON", reply: func(response http.ResponseWriter) { writeJSON(t, response, http.StatusCreated, `{`) }},
		{name: "oversized", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusCreated, `{"firstName":"Ava Nguyen with an oversized response"}`)
		}},
		{name: "interrupted", reply: func(response http.ResponseWriter) {
			response.Header().Set("Content-Length", "100")
			writeJSON(t, response, http.StatusCreated, `{`)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingBambooHR(t, func(response http.ResponseWriter, _ *http.Request, _ int) { test.reply(response) })
			client, err := bamboohr.New(bamboohr.Config{CompanyDomain: testCompanyDomain, MaxResponseBytes: 30},
				testCredentialProvider(), bamboohr.WithAPIBaseURL(provider.URL+"/api/v1"))
			require.NoError(t, err)
			ctx := singledispatchtest.NewContext("bamboohr-flow", "add-unusable")
			result, err := sdkgo.RunMutation(ctx, client.AddEmployee(), bambooHRConnection, validAddEmployeeInput())
			require.NoError(t, err)
			require.Equal(t, bamboohr.AddEmployeeBranchUncertain, result.Branch)
			ctx.AdvanceAttempt()
			result, err = sdkgo.RunMutation(ctx, client.AddEmployee(), bambooHRConnection, validAddEmployeeInput())
			require.NoError(t, err)
			require.Equal(t, bamboohr.AddEmployeeBranchUncertain, result.Branch)
			require.Equal(t, 1, provider.requestCount())
		})
	}
}

func TestAddEmployeeFenceCutsOffSlowWriteInsideAttempt(t *testing.T) {
	provider := newRecordingBambooHR(t, func(_ http.ResponseWriter, request *http.Request, _ int) {
		<-request.Context().Done()
	})
	ctx := singledispatchtest.NewContext("bamboohr-flow", "add-deadline")
	cancel := ctx.SetDeadline(time.Now().Add(time.Second))
	defer cancel()
	started := time.Now()
	result, err := sdkgo.RunMutation(ctx, newBambooHRClient(t, provider.URL).AddEmployee(), bambooHRConnection, validAddEmployeeInput())
	require.NoError(t, err)
	require.Equal(t, bamboohr.AddEmployeeBranchUncertain, result.Branch)
	require.Less(t, time.Since(started), time.Second)
	require.Equal(t, 1, provider.requestCount())
}

func TestAddEmployeeFenceDoesNotReplayOnReusedConnection(t *testing.T) {
	provider := newRecordingBambooHR(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodGet {
			writeJSON(t, response, http.StatusOK, `{"id":"140","firstName":"Ava"}`)
			return
		}
		dropConnection(t, response)
	})
	client, err := bamboohr.New(bamboohr.Config{CompanyDomain: testCompanyDomain}, testCredentialProvider(),
		bamboohr.WithAPIBaseURL(provider.URL+"/api/v1"), bamboohr.WithHTTPClient(&http.Client{Transport: &keyedReplayProbe{base: http.DefaultTransport, t: t}}))
	require.NoError(t, err)
	_, err = sdkgo.RunQuery(newBambooHRDexContext("prime"), client.GetEmployee(), bambooHRConnection,
		bamboohr.GetEmployeeInput{EmployeeID: "140", Fields: []string{"firstName"}})
	require.NoError(t, err)
	ctx := singledispatchtest.NewContext("bamboohr-flow", "add-reused")
	result, err := sdkgo.RunMutation(ctx, client.AddEmployee(), bambooHRConnection, validAddEmployeeInput())
	require.NoError(t, err)
	require.Equal(t, bamboohr.AddEmployeeBranchUncertain, result.Branch)
	require.Equal(t, 2, provider.requestCount())
	require.Equal(t, http.MethodPost, provider.request(1).method)
}

type keyedReplayProbe struct {
	base http.RoundTripper
	t    *testing.T
}

func (probe *keyedReplayProbe) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Method == http.MethodPost {
		require.Nil(probe.t, request.GetBody, "a keyed POST must not be replayable by net/http")
		request.Header.Set("Idempotency-Key", "probe-call")
	}
	return probe.base.RoundTrip(request)
}

func TestAddEmployeeFenceLostClaimNeverPermitsAnotherPost(t *testing.T) {
	provider := newRecordingBambooHR(t, func(response http.ResponseWriter, _ *http.Request, _ int) { dropConnection(t, response) })
	ctx := singledispatchtest.NewContext("bamboohr-flow", "add-lost-claim")
	ctx.LoseNextHeartbeat()
	client := newBambooHRClient(t, provider.URL)
	result, err := sdkgo.RunMutation(ctx, client.AddEmployee(), bambooHRConnection, validAddEmployeeInput())
	require.NoError(t, err)
	require.Equal(t, bamboohr.AddEmployeeBranchUncertain, result.Branch)
	ctx.AdvanceAttempt()
	result, err = sdkgo.RunMutation(ctx, client.AddEmployee(), bambooHRConnection, validAddEmployeeInput())
	require.NoError(t, err)
	require.Equal(t, bamboohr.AddEmployeeBranchUncertain, result.Branch)
	require.Equal(t, 1, provider.requestCount())
}
