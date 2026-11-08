// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package whatsappbusiness

import (
	"encoding/json"
	"fmt"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSendTextMessageSingleDispatch(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/1906385232743451/messages", r.URL.Path)
		require.Equal(t, "Bearer "+testAccessToken, r.Header.Get("Authorization"))
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, "text", body["type"])
		require.Equal(t, "16315551234", body["to"])
		require.Equal(t, "Hello", body["text"].(map[string]any)["body"])
		_, err := fmt.Fprint(w, `{"messaging_product":"whatsapp","messages":[{"id":"wamid.example-text"}]}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, 0)
	ctx := singledispatchtest.NewContext("whatsapp-text", "send-text")
	input := SendTextMessageInput{PhoneNumberID: "1906385232743451", Recipient: "16315551234", Text: "Hello"}
	sent, err := sdkgo.RunMutation(ctx, client.SendTextMessage(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, SendTextMessageBranchSent, sent.Branch)
	require.Equal(t, "wamid.example-text", sent.Value.MessageID)
	ctx.AdvanceAttempt()
	uncertain, err := sdkgo.RunMutation(ctx, client.SendTextMessage(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, SendTextMessageBranchUncertain, uncertain.Branch)
	require.Equal(t, 1, requests)
}

func TestSendTemplateMessageRetries429(t *testing.T) {
	status, requests := http.StatusTooManyRequests, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, "template", body["type"])
		template := body["template"].(map[string]any)
		require.Equal(t, "order_update", template["name"])
		components := template["components"].([]any)
		require.Equal(t, "A123", components[0].(map[string]any)["parameters"].([]any)[0].(map[string]any)["text"])
		w.WriteHeader(status)
		if status == http.StatusOK {
			_, err := fmt.Fprint(w, `{"messaging_product":"whatsapp","messages":[{"id":"wamid.example-template"}]}`)
			require.NoError(t, err)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, 0)
	ctx := singledispatchtest.NewContext("whatsapp-template", "send-template")
	input := SendTemplateMessageInput{PhoneNumberID: "1906385232743451", Recipient: "16315551234", TemplateName: "order_update", LanguageCode: "en_US", BodyParameters: []string{"A123"}}
	_, err := sdkgo.RunMutation(ctx, client.SendTemplateMessage(), testConnection, input)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	status = http.StatusOK
	ctx.AdvanceAttempt()
	sent, err := sdkgo.RunMutation(ctx, client.SendTemplateMessage(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, SendTemplateMessageBranchSent, sent.Branch)
	require.Equal(t, 2, requests)
}

func TestSendTextMessageAmbiguousServerFailure(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, 0)
	ctx := singledispatchtest.NewContext("whatsapp-uncertain", "send-text")
	input := SendTextMessageInput{PhoneNumberID: "1906385232743451", Recipient: "16315551234", Text: "Hello"}
	for attempt := 0; attempt < 2; attempt++ {
		result, err := sdkgo.RunMutation(ctx, client.SendTextMessage(), testConnection, input)
		require.NoError(t, err)
		require.Equal(t, SendTextMessageBranchUncertain, result.Branch)
		ctx.AdvanceAttempt()
	}
	require.Equal(t, 1, requests)
}

func TestSendTextMessageDefinitiveRejection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, err := fmt.Fprint(w, `{"error":{"message":"customer and secret should not be copied"}}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, 0)
	ctx := singledispatchtest.NewContext("whatsapp-rejected", "send-text")
	input := SendTextMessageInput{PhoneNumberID: "1906385232743451", Recipient: "16315551234", Text: "Hello"}
	result, err := sdkgo.RunMutation(ctx, client.SendTextMessage(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, SendTextMessageBranchProviderRejected, result.Branch)
	require.NotContains(t, result.Failure.Message, "customer")
	require.NotContains(t, result.Failure.Message, testAccessToken)
}
