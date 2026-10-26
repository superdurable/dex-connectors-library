// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pipedrive_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/pipedrive"
	"github.com/superdurable/dex-connectors-library/connectors/pipedrive/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

func TestCreateObjectFenceRetries503ThenReadsBackAcceptedID(t *testing.T) {
	attempts := 0
	provider := newRecordingPipedrive(t, func(response http.ResponseWriter, request recordedRequest) {
		if request.method == http.MethodGet {
			writeRecord(t, response, dealRecord)
			return
		}
		attempts++
		if attempts == 1 {
			writeError(t, response, http.StatusServiceUnavailable)
			return
		}
		writeRecord(t, response, dealRecord)
	})
	client := newAPITokenClient(t, provider.URL)
	ctx := singledispatchtest.NewContext("deal-intake", "create-deal")
	_, err := sdkgo.RunMutation(ctx, client.CreateObject(), pipedriveConnection, dealInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.CreateObject(), pipedriveConnection, dealInput())
	require.NoError(t, err)
	require.Equal(t, pipedrive.CreateObjectBranchCreated, result.Branch)
	ctx.AdvanceAttempt()
	confirmed, err := sdkgo.RunMutation(ctx, client.CreateObject(), pipedriveConnection, dealInput())
	require.NoError(t, err)
	require.Equal(t, pipedrive.CreateObjectBranchCreated, confirmed.Branch)
	require.Equal(t, []string{http.MethodPost, http.MethodPost, http.MethodGet}, []string{
		provider.recordedRequests()[0].method, provider.recordedRequests()[1].method, provider.recordedRequests()[2].method,
	})
	require.Equal(t, 2, attempts)
}

func TestCreateObjectFenceRequiresImmediatelyPreviousCanResend(t *testing.T) {
	provider := newRecordingPipedrive(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeError(t, response, http.StatusServiceUnavailable)
	})
	ctx := singledispatchtest.NewContext("deal-intake", "stale-create")
	client := newAPITokenClient(t, provider.URL)
	_, err := sdkgo.RunMutation(ctx, client.CreateObject(), pipedriveConnection, dealInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	ctx.AdvanceAttempt()
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.CreateObject(), pipedriveConnection, dealInput())
	require.NoError(t, err)
	require.Equal(t, sdkgo.UncertainBranchID, result.Branch)
	require.Len(t, provider.recordedRequests(), 1)
}

func TestCreateObjectFenceRecordsCredentialFailureBeforeSend(t *testing.T) {
	provider := newRecordingPipedrive(t, func(response http.ResponseWriter, _ recordedRequest) { writeRecord(t, response, dealRecord) })
	expired := time.Now().Add(-time.Minute)
	credentials := testsupport.NewRefreshingCredentialSource(oauthCredentials(provider.URL), &expired)
	refreshes := 0
	client, err := pipedrive.New(pipedrive.Config{}, credentials, pipedrive.WithHTTPClient(&http.Client{Transport: tokenEndpointTransport{tokenHandler: func(response http.ResponseWriter, _ *http.Request) {
		refreshes++
		if refreshes == 1 {
			writeError(t, response, http.StatusServiceUnavailable)
			return
		}
		writeJSON(t, response, http.StatusOK, `{"access_token":"refreshed-access-token","token_type":"Bearer","expires_in":3599,"api_domain":"`+provider.URL+`"}`)
	}}}))
	require.NoError(t, err)
	ctx := singledispatchtest.NewContext("deal-intake", "credential-create")
	_, err = sdkgo.RunMutation(ctx, client.CreateObject(), pipedriveConnection, dealInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Empty(t, provider.recordedRequests())
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.CreateObject(), pipedriveConnection, dealInput())
	require.NoError(t, err)
	require.Equal(t, pipedrive.CreateObjectBranchCreated, result.Branch)
	require.Len(t, provider.recordedRequests(), 1)
}

func TestFencedCreatesRefreshRejectedOAuthBeforeTheNextAttempt(t *testing.T) {
	for _, operation := range []string{"create", "upsert"} {
		t.Run(operation, func(t *testing.T) {
			provider := newRecordingPipedrive(t, func(response http.ResponseWriter, request recordedRequest) {
				if request.method == http.MethodGet {
					writeJSON(t, response, http.StatusOK, `{"success":true,"data":{"items":[]},"additional_data":{"next_cursor":null}}`)
					return
				}
				if request.authorization != "Bearer refreshed-access-token" {
					writeError(t, response, http.StatusUnauthorized)
					return
				}
				if operation == "create" {
					writeRecord(t, response, dealRecord)
				} else {
					writeRecord(t, response, `{"id":901,"name":"Jane Smith","emails":[{"value":"jane@acme.example.com","primary":true}]}`)
				}
			})
			expiresAt := time.Now().Add(time.Hour)
			credentials := testsupport.NewRefreshingCredentialSource(oauthCredentials(provider.URL), &expiresAt)
			refreshes := 0
			client, err := pipedrive.New(pipedrive.Config{}, credentials, pipedrive.WithHTTPClient(&http.Client{Transport: tokenEndpointTransport{tokenHandler: func(response http.ResponseWriter, _ *http.Request) {
				refreshes++
				writeJSON(t, response, http.StatusOK, `{"access_token":"refreshed-access-token","token_type":"Bearer","expires_in":3599,"api_domain":"`+provider.URL+`"}`)
			}}}))
			require.NoError(t, err)
			ctx := singledispatchtest.NewContext("deal-intake", "oauth-401-"+operation)
			invoke := func() (sdkgo.BranchID, error) {
				if operation == "create" {
					result, err := sdkgo.RunMutation(ctx, client.CreateObject(), pipedriveConnection, dealInput())
					return result.Branch, err
				}
				result, err := sdkgo.RunMutation(ctx, client.UpsertObject(), pipedriveConnection, personUpsert("jane@acme.example.com"))
				return result.Branch, err
			}
			_, err = invoke()
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			require.Equal(t, 1, refreshes)
			ctx.AdvanceAttempt()
			branch, err := invoke()
			require.NoError(t, err)
			if operation == "create" {
				require.Equal(t, pipedrive.CreateObjectBranchCreated, branch)
			} else {
				require.Equal(t, pipedrive.UpsertObjectBranchUpserted, branch)
			}
			var posts []recordedRequest
			for _, request := range provider.recordedRequests() {
				if request.method == http.MethodPost {
					posts = append(posts, request)
				}
			}
			require.Len(t, posts, 2)
			require.Equal(t, "Bearer "+testAccessToken, posts[0].authorization)
			require.Equal(t, "Bearer refreshed-access-token", posts[1].authorization)
		})
	}
}

func TestFencedCreatesSelectProviderRejectedForRevokedOAuthBeforeSending(t *testing.T) {
	for _, operation := range []string{"create", "upsert"} {
		t.Run(operation, func(t *testing.T) {
			provider := newRecordingPipedrive(t, func(response http.ResponseWriter, _ recordedRequest) {
				writeError(t, response, http.StatusUnauthorized)
			})
			expired := time.Now().Add(-time.Minute)
			credentials := testsupport.NewRefreshingCredentialSource(oauthCredentials(provider.URL), &expired)
			client, err := pipedrive.New(pipedrive.Config{}, credentials, pipedrive.WithHTTPClient(&http.Client{Transport: tokenEndpointTransport{tokenHandler: func(response http.ResponseWriter, _ *http.Request) {
				writeJSON(t, response, http.StatusBadRequest, `{"error":"invalid_grant"}`)
			}}}))
			require.NoError(t, err)
			ctx := singledispatchtest.NewContext("deal-intake", "revoked-"+operation)
			if operation == "create" {
				result, err := sdkgo.RunMutation(ctx, client.CreateObject(), pipedriveConnection, dealInput())
				require.NoError(t, err)
				require.Equal(t, pipedrive.CreateObjectBranchProviderRejected, result.Branch)
				require.Contains(t, result.Failure.Message, "renewed")
			} else {
				result, err := sdkgo.RunMutation(ctx, client.UpsertObject(), pipedriveConnection, personUpsert("jane@acme.example.com"))
				require.NoError(t, err)
				require.Equal(t, pipedrive.UpsertObjectBranchProviderRejected, result.Branch)
				require.Contains(t, result.Failure.Message, "renewed")
			}
			require.Empty(t, provider.recordedRequests())
			require.Len(t, ctx.Heartbeats(), 1, "the not-sent checkpoint precedes the branch")
		})
	}
}

func TestFencedCreatesSelectDefectForInvalidCredentialsBeforeSending(t *testing.T) {
	provider := newRecordingPipedrive(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeError(t, response, http.StatusUnauthorized)
	})
	client, err := pipedrive.New(pipedrive.Config{Endpoint: provider.URL}, sdkgo.StaticCredentialProvider[pipedrive.Credentials]{})
	require.NoError(t, err)
	for _, operation := range []string{"create", "upsert"} {
		ctx := singledispatchtest.NewContext("deal-intake", "invalid-credential-"+operation)
		if operation == "create" {
			result, err := sdkgo.RunMutation(ctx, client.CreateObject(), pipedriveConnection, dealInput())
			require.NoError(t, err)
			require.Equal(t, pipedrive.CreateObjectBranchDefect, result.Branch)
		} else {
			result, err := sdkgo.RunMutation(ctx, client.UpsertObject(), pipedriveConnection, personUpsert("jane@acme.example.com"))
			require.NoError(t, err)
			require.Equal(t, pipedrive.UpsertObjectBranchDefect, result.Branch)
		}
		require.Len(t, ctx.Heartbeats(), 1)
	}
	require.Empty(t, provider.recordedRequests())
}

func TestFencedCreateRejectsAnUnrefreshableOAuth401WithoutResending(t *testing.T) {
	provider := newRecordingPipedrive(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeError(t, response, http.StatusUnauthorized)
	})
	expiresAt := time.Now().Add(time.Hour)
	credentials := testsupport.NewRefreshingCredentialSource(oauthCredentials(provider.URL), &expiresAt)
	client, err := pipedrive.New(pipedrive.Config{}, credentials, pipedrive.WithHTTPClient(&http.Client{Transport: tokenEndpointTransport{tokenHandler: func(response http.ResponseWriter, _ *http.Request) {
		writeJSON(t, response, http.StatusBadRequest, `{"error":"invalid_grant"}`)
	}}}))
	require.NoError(t, err)
	result, err := sdkgo.RunMutation(singledispatchtest.NewContext("deal-intake", "unrefreshable-401"), client.CreateObject(), pipedriveConnection, dealInput())
	require.NoError(t, err)
	require.Equal(t, pipedrive.CreateObjectBranchProviderRejected, result.Branch)
	require.Len(t, provider.recordedRequests(), 1)
}

func TestCreateObjectFenceFailsClosedWhenClaimIsLost(t *testing.T) {
	provider := newRecordingPipedrive(t, func(response http.ResponseWriter, _ recordedRequest) {
		hijacker, ok := response.(http.Hijacker)
		require.True(t, ok)
		connection, _, err := hijacker.Hijack()
		require.NoError(t, err)
		require.NoError(t, connection.Close())
	})
	ctx := singledispatchtest.NewContext("deal-intake", "lost-claim")
	ctx.LoseNextHeartbeat()
	client := newAPITokenClient(t, provider.URL)
	first, err := sdkgo.RunMutation(ctx, client.CreateObject(), pipedriveConnection, dealInput())
	require.NoError(t, err)
	require.Equal(t, sdkgo.UncertainBranchID, first.Branch)
	ctx.AdvanceAttempt()
	second, err := sdkgo.RunMutation(ctx, client.CreateObject(), pipedriveConnection, dealInput())
	require.NoError(t, err)
	require.Equal(t, sdkgo.UncertainBranchID, second.Branch)
	require.Len(t, provider.recordedRequests(), 1)
}

func TestCreateObjectFenceCutsMidRequestAtAttemptMargin(t *testing.T) {
	provider := newRecordingPipedrive(t, func(_ http.ResponseWriter, request recordedRequest) {
		<-request.context.Done()
	})
	ctx := singledispatchtest.NewContext("deal-intake", "deadline-cut")
	cancel := ctx.SetDeadline(time.Now().Add(time.Second))
	defer cancel()
	result, err := sdkgo.RunMutation(ctx, newAPITokenClient(t, provider.URL).CreateObject(), pipedriveConnection, dealInput())
	require.NoError(t, err)
	require.Equal(t, sdkgo.UncertainBranchID, result.Branch)
	require.Len(t, provider.recordedRequests(), 1)
	require.False(t, ctx.Err() == context.DeadlineExceeded, "the request ended before the Step deadline")
}

func TestCreateObjectFenceDoesNotResendWhenAcceptedIDCannotBeReadBack(t *testing.T) {
	accepted := newRecordingPipedrive(t, func(response http.ResponseWriter, _ recordedRequest) { writeRecord(t, response, dealRecord) })
	unreadable := newRecordingPipedrive(t, func(response http.ResponseWriter, _ recordedRequest) { writeError(t, response, http.StatusNotFound) })
	ctx := singledispatchtest.NewContext("deal-intake", "lost-readback")
	_, err := sdkgo.RunMutation(ctx, newAPITokenClient(t, accepted.URL).CreateObject(), pipedriveConnection, dealInput())
	require.NoError(t, err)
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, newAPITokenClient(t, unreadable.URL).CreateObject(), pipedriveConnection, dealInput())
	require.NoError(t, err)
	require.Equal(t, sdkgo.UncertainBranchID, result.Branch)
	require.Len(t, accepted.recordedRequests(), 1)
	require.Len(t, unreadable.recordedRequests(), 1)
	require.Equal(t, http.MethodGet, unreadable.recordedRequests()[0].method)
}

func TestCreateObjectFenceRetriesTransientReadBackFailuresWithoutResending(t *testing.T) {
	for _, failure := range []string{"credential", "provider"} {
		t.Run(failure, func(t *testing.T) {
			accepted := newRecordingPipedrive(t, func(response http.ResponseWriter, _ recordedRequest) { writeRecord(t, response, dealRecord) })
			ctx := singledispatchtest.NewContext("deal-intake", "transient-readback-"+failure)
			_, err := sdkgo.RunMutation(ctx, newAPITokenClient(t, accepted.URL).CreateObject(), pipedriveConnection, dealInput())
			require.NoError(t, err)
			ctx.AdvanceAttempt()
			var client *pipedrive.Client
			var readBack *recordingPipedrive
			if failure == "credential" {
				expired := time.Now().Add(-time.Minute)
				credentials := testsupport.NewRefreshingCredentialSource(oauthCredentials(accepted.URL), &expired)
				client, err = pipedrive.New(pipedrive.Config{}, credentials, pipedrive.WithHTTPClient(&http.Client{Transport: tokenEndpointTransport{tokenHandler: func(response http.ResponseWriter, _ *http.Request) {
					writeError(t, response, http.StatusServiceUnavailable)
				}}}))
				require.NoError(t, err)
			} else {
				readBack = newRecordingPipedrive(t, func(response http.ResponseWriter, _ recordedRequest) {
					writeError(t, response, http.StatusServiceUnavailable)
				})
				client = newAPITokenClient(t, readBack.URL)
			}
			_, err = sdkgo.RunMutation(ctx, client.CreateObject(), pipedriveConnection, dealInput())
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			require.Len(t, accepted.recordedRequests(), 1)
			if readBack != nil {
				require.Len(t, readBack.recordedRequests(), 1)
				require.Equal(t, http.MethodGet, readBack.recordedRequests()[0].method)
			}
		})
	}
}

type idempotencyHeaderTransport struct{ base *http.Transport }

func (transport idempotencyHeaderTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Method == http.MethodPost {
		request.Header.Set("Idempotency-Key", "call-1")
	}
	return transport.base.RoundTrip(request)
}

func TestCreateObjectFenceDisablesHTTPTransportReplayOnReusedConnection(t *testing.T) {
	provider := newRecordingPipedrive(t, func(response http.ResponseWriter, request recordedRequest) {
		if request.method == http.MethodGet {
			writeRecord(t, response, dealRecord)
			return
		}
		hijacker, ok := response.(http.Hijacker)
		require.True(t, ok)
		connection, _, err := hijacker.Hijack()
		require.NoError(t, err)
		require.NoError(t, connection.Close())
	})
	base := http.DefaultTransport.(*http.Transport).Clone()
	defer base.CloseIdleConnections()
	client, err := pipedrive.New(pipedrive.Config{Endpoint: provider.URL}, sdkgo.StaticCredentialProvider[pipedrive.Credentials]{
		pipedriveConnection: {AuthMethodID: pipedrive.APITokenAuthMethodID, APIToken: sdkgo.NewSecretString(testAPIToken)},
	}, pipedrive.WithHTTPClient(&http.Client{Transport: idempotencyHeaderTransport{base: base}}))
	require.NoError(t, err)
	_, err = sdkgo.RunQuery(newStepDexContext("prime-reused-connection"), client.GetObject(), pipedriveConnection,
		pipedrive.GetObjectInput{ObjectType: pipedrive.ObjectTypeDeals, ObjectID: "42"})
	require.NoError(t, err)
	result, err := sdkgo.RunMutation(singledispatchtest.NewContext("deal-intake", "reused-create"), client.CreateObject(), pipedriveConnection, dealInput())
	require.NoError(t, err)
	require.Equal(t, sdkgo.UncertainBranchID, result.Branch)
	requests := provider.recordedRequests()
	require.Len(t, requests, 2)
	require.Equal(t, http.MethodPost, requests[1].method)
	require.Equal(t, requests[0].remoteAddress, requests[1].remoteAddress, "the write used the primed connection")
}
