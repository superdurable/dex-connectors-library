// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pandadoc

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

func TestSendDocumentDoesNotConfirmAnOldSentStatus(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		_, _ = writer.Write([]byte(`{"id":"fANp5wW5EwY7AkiTkTGekZ","name":"Example","status":"document.sent"}`))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, 0)
	ctx := singledispatchtest.NewContext("send-document-flow", "send-document")
	require.NoError(t, ctx.SetPersistedCheckpoint(singledispatch.Marker{CallID: "send-document", Attempt: 1, State: singledispatch.StateClaimed, SendCount: 1}))
	ctx.AdvanceAttempt()
	actual := client.SendDocument().Invoke(sdkgo.Call{Context: ctx, ID: "send-document", Connection: testConnection}, SendDocumentInput{DocumentID: "fANp5wW5EwY7AkiTkTGekZ"})
	expected := uncertainDocumentMutation[DocumentAction]("sendDocument")
	require.Equal(t, expected, actual)
	require.Zero(t, requests)
}
