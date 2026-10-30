// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hiver_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/hiver"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestRequestsCarryTheBearerKeyAndStayOneIntervalApart(t *testing.T) {
	provider := newRecordingHiver(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"data":{"results":[],"pagination":{"next_page":null}}}`)
	})
	client, err := hiver.New(hiver.Config{RequestIntervalMilliseconds: 200}, testCredentialProvider(), hiver.WithAPIBaseURL(provider.URL+"/v1"))
	require.NoError(t, err)
	var group sync.WaitGroup
	for index := range 3 {
		group.Add(1)
		go func() {
			defer group.Done()
			result, err := sdkgo.RunQuery(newHiverDexContext("list-"+strconv.Itoa(index)), client.ListInboxes(), hiverConnection, hiver.ListInboxesInput{})
			require.NoError(t, err)
			require.Equal(t, hiver.ListInboxesBranchListed, result.Branch)
		}()
	}
	group.Wait()
	require.Equal(t, 3, provider.requestCount())
	var starts []time.Time
	for index := range 3 {
		request := provider.request(index)
		require.Equal(t, "Bearer "+testAPIKey, request.header.Get("Authorization"))
		require.Equal(t, "application/json", request.header.Get("Accept"))
		starts = append(starts, request.at)
	}
	for index := 1; index < len(starts); index++ {
		earliest, latest := starts[index-1], starts[index]
		if latest.Before(earliest) {
			earliest, latest = latest, earliest
		}
		require.GreaterOrEqual(t, latest.Sub(earliest), 150*time.Millisecond, "concurrent Steps share one spacing schedule")
	}
}

func TestRateLimitHoldsTheOtherStepsOfTheClient(t *testing.T) {
	releasesRateLimit := make(chan struct{})
	provider := newRecordingHiver(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		if index == 0 {
			<-releasesRateLimit
			writeJSON(t, response, http.StatusTooManyRequests, `{"Message":"SENTINEL"}`)
			return
		}
		writeJSON(t, response, http.StatusOK, `{"data":{"results":[],"pagination":{"next_page":null}}}`)
	})
	const requestInterval = 200 * time.Millisecond
	client, err := hiver.New(hiver.Config{RequestIntervalMilliseconds: requestInterval.Milliseconds()}, testCredentialProvider(), hiver.WithAPIBaseURL(provider.URL+"/v1"))
	require.NoError(t, err)
	rateLimited := make(chan error, 1)
	go func() {
		_, err := sdkgo.RunQuery(newHiverDexContext("rate-limited"), client.ListInboxes(), hiverConnection, hiver.ListInboxesInput{})
		rateLimited <- err
	}()
	require.Eventually(t, func() bool { return provider.requestCount() == 1 }, 5*time.Second, 5*time.Millisecond)
	held := make(chan error, 1)
	go func() {
		_, err := sdkgo.RunQuery(newHiverDexContext("held"), client.ListInboxes(), hiverConnection, hiver.ListInboxesInput{})
		held <- err
	}()
	close(releasesRateLimit)
	require.Error(t, <-rateLimited, "the rate-limited Step is retried")
	require.NoError(t, <-held)
	require.Equal(t, 2, provider.requestCount())
	require.GreaterOrEqual(t, provider.request(1).at.Sub(provider.request(0).at), 4*requestInterval,
		"the other Step waited out the four-interval penalty instead of its next one-interval slot")
}

func TestCancelledWaitForARequestSlotSendsNothing(t *testing.T) {
	provider := newRecordingHiver(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"data":{"results":[],"pagination":{"next_page":null}}}`)
	})
	client, err := hiver.New(hiver.Config{RequestIntervalMilliseconds: 60000}, testCredentialProvider(), hiver.WithAPIBaseURL(provider.URL+"/v1"))
	require.NoError(t, err)
	_, err = sdkgo.RunQuery(newHiverDexContext("first"), client.ListInboxes(), hiverConnection, hiver.ListInboxesInput{})
	require.NoError(t, err)
	cancelled, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	ctx := newHiverDexContext("second")
	ctx.Context = cancelled
	_, err = sdkgo.RunQuery(ctx, client.ListInboxes(), hiverConnection, hiver.ListInboxesInput{})
	require.Error(t, err, "the Step is retried")
	require.Equal(t, 1, provider.requestCount(), "the second request waited for its slot and was never sent")
}

func TestFailureStatusesMapToBranchesWithoutHiverText(t *testing.T) {
	for _, test := range []struct {
		status     int
		body       string
		wantBranch sdkgo.BranchID
		wantKind   sdkgo.FailureKind
		isRetry    bool
	}{
		{status: http.StatusUnauthorized, body: `{"Message":"SENTINEL invalid token"}`, wantBranch: hiver.GetConversationBranchProviderRejected, wantKind: sdkgo.FailureAuthentication},
		{status: http.StatusForbidden, body: `{"errors":[{"message":"SENTINEL forbidden"}]}`, wantBranch: hiver.GetConversationBranchProviderRejected, wantKind: sdkgo.FailureAuthorization},
		{status: http.StatusBadRequest, body: `{"errors":[{"message":"SENTINEL bad"}]}`, wantBranch: hiver.GetConversationBranchProviderRejected, wantKind: sdkgo.FailureValidation},
		{status: http.StatusNotFound, body: `{"Message":"SENTINEL missing"}`, wantBranch: hiver.GetConversationBranchNotFound, wantKind: sdkgo.FailureNotFound},
		{status: http.StatusFound, body: ``, wantBranch: hiver.GetConversationBranchProviderRejected, wantKind: sdkgo.FailureProtocol},
		{status: http.StatusTooManyRequests, body: `{"Message":"SENTINEL slow down"}`, isRetry: true, wantKind: sdkgo.FailureRateLimit},
		{status: http.StatusServiceUnavailable, body: `SENTINEL`, isRetry: true, wantKind: sdkgo.FailureAvailability},
	} {
		t.Run(strconv.Itoa(test.status), func(t *testing.T) {
			provider := newRecordingHiver(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				if test.status == http.StatusFound {
					response.Header().Set("Location", "https://attacker.example.com/steal")
				}
				writeJSON(t, response, test.status, test.body)
			})
			result, err := sdkgo.RunQuery(newHiverDexContext("read"), newHiverClient(t, provider.URL).GetConversation(), hiverConnection,
				hiver.GetConversationInput{InboxID: "105902", ConversationID: "573741352"})
			require.Equal(t, 1, provider.requestCount(), "redirects are never followed")
			if test.isRetry {
				require.Error(t, err)
				require.NotContains(t, err.Error(), "SENTINEL")
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.wantBranch, result.Branch)
			require.Equal(t, test.wantKind, result.Failure.Kind)
			encoded, err := json.Marshal(result)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "SENTINEL")
			require.NotContains(t, string(encoded), testAPIKey)
		})
	}
}

func TestResponseThatReflectsTheKeyIsInvalid(t *testing.T) {
	provider := newRecordingHiver(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"data":{"results":[{"id":"101","display_name":"`+testAPIKey+`"}],"pagination":{"next_page":null}}}`)
	})
	result, err := sdkgo.RunQuery(newHiverDexContext("list"), newHiverClient(t, provider.URL).ListInboxes(), hiverConnection, hiver.ListInboxesInput{})
	require.NoError(t, err)
	require.Equal(t, hiver.ListInboxesBranchInvalidResponse, result.Branch)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), testAPIKey)
}

func TestOversizedResponseIsInvalid(t *testing.T) {
	provider := newRecordingHiver(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"data":{"results":[],"pagination":{"next_page":null}},"padding":"0123456789"}`)
	})
	client, err := hiver.New(hiver.Config{MaxResponseBytes: 32, RequestIntervalMilliseconds: 1}, testCredentialProvider(), hiver.WithAPIBaseURL(provider.URL+"/v1"))
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(newHiverDexContext("list"), client.ListInboxes(), hiverConnection, hiver.ListInboxesInput{})
	require.NoError(t, err)
	require.Equal(t, hiver.ListInboxesBranchInvalidResponse, result.Branch)
	require.Equal(t, sdkgo.FailureResponseTooLarge, result.Failure.Kind)
}

func TestUnreachableHiverIsRetried(t *testing.T) {
	client, err := hiver.New(hiver.Config{RequestIntervalMilliseconds: 1}, testCredentialProvider(), hiver.WithAPIBaseURL(closedLoopbackURL(t)+"/v1"))
	require.NoError(t, err)
	_, err = sdkgo.RunQuery(newHiverDexContext("list"), client.ListInboxes(), hiverConnection, hiver.ListInboxesInput{})
	require.Error(t, err)
}
