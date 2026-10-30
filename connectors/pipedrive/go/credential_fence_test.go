// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pipedrive_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/pipedrive/go"
	"github.com/superdurable/dex-connectors-library/connectors/pipedrive/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

type credentialResolutionResult struct {
	credentials pipedrive.Credentials
	err         error
}

func TestFencedCreatesClassifyRejectedOAuthRefresh(t *testing.T) {
	for _, operation := range []string{"create", "upsert"} {
		for _, refresh := range []string{"reauthorization", "temporarily unavailable", "unchanged token"} {
			t.Run(operation+"/"+refresh, func(t *testing.T) {
				provider := newRecordingPipedrive(t, func(response http.ResponseWriter, request recordedRequest) {
					if request.method == http.MethodGet {
						writeJSON(t, response, http.StatusOK, `{"success":true,"data":{"items":[]},"additional_data":{"next_cursor":null}}`)
						return
					}
					writeError(t, response, http.StatusUnauthorized)
				})
				expiresAt := time.Now().Add(time.Hour)
				credentials := testsupport.NewRefreshingCredentialSource(oauthCredentials(provider.URL), &expiresAt)
				client, err := pipedrive.New(pipedrive.Config{}, credentials, pipedrive.WithHTTPClient(&http.Client{Transport: tokenEndpointTransport{tokenHandler: func(response http.ResponseWriter, _ *http.Request) {
					switch refresh {
					case "reauthorization":
						writeJSON(t, response, http.StatusBadRequest, `{"error":"invalid_grant"}`)
					case "temporarily unavailable":
						writeError(t, response, http.StatusServiceUnavailable)
					default:
						writeJSON(t, response, http.StatusOK, `{"access_token":"`+testAccessToken+`","token_type":"Bearer","expires_in":3599,"api_domain":"`+provider.URL+`"}`)
					}
				}}}))
				require.NoError(t, err)
				ctx := singledispatchtest.NewContext("deal-intake", "refresh-failure-"+operation+refresh)
				var branch sdkgo.BranchID
				var failure *sdkgo.Failure
				if operation == "create" {
					result, err := sdkgo.RunMutation(ctx, client.CreateObject(), pipedriveConnection, dealInput())
					require.NoError(t, err)
					branch, failure = result.Branch, result.Failure
					require.Equal(t, pipedrive.CreateObjectBranchProviderRejected, branch)
				} else {
					result, err := sdkgo.RunMutation(ctx, client.UpsertObject(), pipedriveConnection, personUpsert("jane@acme.example.com"))
					require.NoError(t, err)
					branch, failure = result.Branch, result.Failure
					require.Equal(t, pipedrive.UpsertObjectBranchProviderRejected, branch)
				}
				require.NotNil(t, failure)
				if refresh == "temporarily unavailable" {
					require.Contains(t, failure.Message, "temporarily unavailable")
					require.NotContains(t, failure.Message, "reconnect")
				} else {
					require.Contains(t, failure.Message, "reconnect the connection")
				}
				var posts int
				for _, request := range provider.recordedRequests() {
					if request.method == http.MethodPost {
						posts++
					}
				}
				require.Equal(t, 1, posts)
				require.Len(t, ctx.Heartbeats(), 1, "the claim cannot authorize a resend")
			})
		}
	}
}

func TestFencedCreateBoundsCredentialRefreshBeforeStepDeadline(t *testing.T) {
	provider := newRecordingPipedrive(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeError(t, response, http.StatusUnauthorized)
	})
	expiresAt := time.Now().Add(time.Hour)
	credentials := testsupport.NewRefreshingCredentialSource(oauthCredentials(provider.URL), &expiresAt)
	client, err := pipedrive.New(pipedrive.Config{}, credentials, pipedrive.WithHTTPClient(&http.Client{Transport: tokenEndpointTransport{tokenHandler: func(_ http.ResponseWriter, request *http.Request) {
		<-request.Context().Done()
	}}}))
	require.NoError(t, err)
	ctx := singledispatchtest.NewContext("deal-intake", "refresh-deadline")
	cancel := ctx.SetDeadline(time.Now().Add(1500 * time.Millisecond))
	defer cancel()
	result, err := sdkgo.RunMutation(ctx, client.CreateObject(), pipedriveConnection, dealInput())
	require.NoError(t, err)
	require.Equal(t, pipedrive.CreateObjectBranchProviderRejected, result.Branch)
	require.Contains(t, result.Failure.Message, "temporarily unavailable")
	require.NoError(t, ctx.Err())
	require.Len(t, provider.recordedRequests(), 1)
}

func (source credentialResolutionResult) Resolve(sdkgo.Call) (pipedrive.Credentials, error) {
	return source.credentials, source.err
}

func (source credentialResolutionResult) ResolveWithRefresh(context.Context, sdkgo.Call, sdkgo.CredentialRefreshDriver[pipedrive.Credentials]) (pipedrive.Credentials, error) {
	return source.credentials, source.err
}

func TestFencedCreatesClassifyCredentialResolutionAtBothFenceStates(t *testing.T) {
	validToken := pipedrive.Credentials{AuthMethodID: pipedrive.APITokenAuthMethodID, APIToken: sdkgo.NewSecretString(testAPIToken)}
	tests := []struct {
		name       string
		resolution credentialResolutionResult
		isRetry    bool
		isRejected bool
	}{
		{name: "store unavailable", resolution: credentialResolutionResult{err: errors.New("credential store unavailable")}, isRetry: true},
		{name: "deadline", resolution: credentialResolutionResult{err: context.DeadlineExceeded}, isRetry: true},
		{name: "reauthorization", resolution: credentialResolutionResult{err: sdkgo.ErrReauthorizationRequired}, isRejected: true},
		{name: "missing credentials", resolution: credentialResolutionResult{}, isRejected: false},
		{name: "empty token", resolution: credentialResolutionResult{credentials: pipedrive.Credentials{AuthMethodID: pipedrive.APITokenAuthMethodID}}},
		{name: "invalid api domain", resolution: credentialResolutionResult{credentials: oauthCredentials("https://invalid.example.com")}},
		{name: "valid token", resolution: credentialResolutionResult{credentials: validToken}},
	}
	for _, operation := range []string{"create", "upsert"} {
		for _, testCase := range tests {
			if testCase.name == "valid token" {
				continue
			}
			for _, readBack := range []bool{false, true} {
				name := operation + "/" + testCase.name
				if readBack {
					name += "/read-back"
				}
				t.Run(name, func(t *testing.T) {
					provider := newRecordingPipedrive(t, func(response http.ResponseWriter, request recordedRequest) {
						if request.method == http.MethodGet {
							writeJSON(t, response, http.StatusOK, `{"success":true,"data":{"items":[]},"additional_data":{"next_cursor":null}}`)
						} else if operation == "create" {
							writeRecord(t, response, dealRecord)
						} else {
							writeRecord(t, response, `{"id":901,"name":"Jane Smith","emails":[{"value":"jane@acme.example.com","primary":true}]}`)
						}
					})
					ctx := singledispatchtest.NewContext("deal-intake", "credential-matrix-"+name)
					invoke := func(client *pipedrive.Client) (sdkgo.BranchID, *sdkgo.Failure, sdkgo.Receipt, error) {
						if operation == "create" {
							result, err := sdkgo.RunMutation(ctx, client.CreateObject(), pipedriveConnection, dealInput())
							return result.Branch, result.Failure, result.Receipt, err
						}
						result, err := sdkgo.RunMutation(ctx, client.UpsertObject(), pipedriveConnection, personUpsert("jane@acme.example.com"))
						return result.Branch, result.Failure, result.Receipt, err
					}
					providerID := ""
					if readBack {
						branch, _, receipt, err := invoke(newAPITokenClient(t, provider.URL))
						require.NoError(t, err)
						require.NotEmpty(t, branch)
						providerID = receipt.ProviderObjectID
						require.NotEmpty(t, providerID)
						ctx.AdvanceAttempt()
					}
					before := len(provider.recordedRequests())
					client, err := pipedrive.New(pipedrive.Config{Endpoint: provider.URL}, testCase.resolution)
					require.NoError(t, err)
					branch, failure, receipt, err := invoke(client)
					if testCase.isRetry {
						var retry *sdkgo.RetryError
						require.ErrorAs(t, err, &retry)
						require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
					} else {
						require.NoError(t, err)
						require.NotNil(t, failure)
						require.Equal(t, sdkgo.FailureAuthentication, failure.Kind)
						require.Contains(t, failure.Message, "reconnect the connection")
						if readBack {
							require.Equal(t, sdkgo.UncertainBranchID, branch)
							require.Equal(t, providerID, receipt.ProviderObjectID)
						} else if testCase.isRejected {
							if operation == "create" {
								require.Equal(t, pipedrive.CreateObjectBranchProviderRejected, branch)
							} else {
								require.Equal(t, pipedrive.UpsertObjectBranchProviderRejected, branch)
							}
						} else if operation == "create" {
							require.Equal(t, pipedrive.CreateObjectBranchDefect, branch)
						} else {
							require.Equal(t, pipedrive.UpsertObjectBranchDefect, branch)
						}
					}
					require.Len(t, provider.recordedRequests(), before)
					if !readBack {
						require.Len(t, ctx.Heartbeats(), 1)
					}
				})
			}
		}
	}
}
