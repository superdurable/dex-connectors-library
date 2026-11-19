// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package onespansign

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

func TestSendPackageDoesNotConfirmAnOldSentStatus(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		_, _ = writer.Write([]byte(`{"id":"9sKhW-h-qS9m6Ho3zRv3n2a-rkI=","name":"Example","status":"SENT"}`))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/api", 0)
	ctx := singledispatchtest.NewContext("send-package-flow", "send-package")
	require.NoError(t, ctx.SetPersistedCheckpoint(singledispatch.Marker{CallID: "send-package", Attempt: 1, State: singledispatch.StateClaimed, SendCount: 1}))
	ctx.AdvanceAttempt()
	actual := client.SendPackage().Invoke(sdkgo.Call{Context: ctx, ID: "send-package", Connection: testConnection}, SendPackageInput{PackageID: "9sKhW-h-qS9m6Ho3zRv3n2a-rkI="})
	expected := sdkgo.NewMutationUncertain(SentPackage{}, packageOperationFailure("sendPackage", sdkgo.FailureTransport, "delivery may have succeeded"), sdkgo.Receipt{})
	require.Equal(t, expected, actual)
	require.Zero(t, requests)
}

func TestSendPackageFailsClosedBeforeReadingSentStatus(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		_, _ = writer.Write([]byte(`{"id":"9sKhW-h-qS9m6Ho3zRv3n2a-rkI=","name":"Example","status":"SENT"}`))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/api", 0)
	ctx := singledispatchtest.NewContext("send-package-flow", "send-package")
	require.NoError(t, ctx.SetPersistedCheckpoint(map[string]any{"singleDispatchMarkers": map[string]any{
		"send-package": map[string]any{"callId": "send-package", "attempt": 1, "state": "claimed", "sendCount": 0},
	}}))
	ctx.AdvanceAttempt()
	actual := client.SendPackage().Invoke(sdkgo.Call{Context: ctx, ID: "send-package", Connection: testConnection}, SendPackageInput{PackageID: "9sKhW-h-qS9m6Ho3zRv3n2a-rkI="})
	expected := sdkgo.NewMutationRetry[SentPackage](packageOperationFailure("sendPackage", sdkgo.FailureAvailability, "dispatch checkpoint is unreadable"), 0)
	require.Equal(t, expected, actual)
	require.Zero(t, requests)
}
