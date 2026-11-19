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

func TestAmbiguousAddSignerDoesNotConfirmExistingRole(t *testing.T) {
	serverRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		serverRequests++
		_, _ = writer.Write([]byte(`{"roles":[{"id":"signer-1","signers":[{"email":"same@example.com"}]}]}`))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/api", 0)
	ctx := singledispatchtest.NewContext("ambiguous-flow", "add-signer")
	ctx.AdvanceAttempt()
	require.NoError(t, ctx.SetPersistedCheckpoint(singledispatch.Marker{CallID: "ambiguous-signer", Attempt: 1, State: singledispatch.StateClaimed, SendCount: 1}))
	actual := client.AddSigner().Invoke(sdkgo.Call{Context: ctx, ID: "ambiguous-signer", Connection: testConnection}, AddSignerInput{
		PackageID: "9sKhW-h-qS9m6Ho3zRv3n2a-rkI=", RoleID: "signer-1", Email: "same@example.com", FirstName: "Same", LastName: "Signer",
	})
	expected := sdkgo.NewMutationUncertain(SignerRoleID{}, packageOperationFailure("addSigner", sdkgo.FailureTransport, "signer addition may have succeeded"), sdkgo.Receipt{})
	require.Equal(t, expected, actual)
	require.Zero(t, serverRequests, "an existing role must not confirm this addition")
}
