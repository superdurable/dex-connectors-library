// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package whatsappbusiness

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

const sentTextBody = `{"messaging_product":"whatsapp","messages":[{"id":"wamid.example-text"}]}`

var testTextInput = SendTextMessageInput{PhoneNumberID: "1906385232743451", Recipient: "16315551234", Text: "Hello"}

// newScriptedSendServer answers each text send with the next status and body, counting requests.
func newScriptedSendServer(t *testing.T, statuses []int, bodies []string) (*httptest.Server, *int) {
	t.Helper()
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/1906385232743451/messages", r.URL.Path)
		require.Equal(t, "Bearer "+testAccessToken, r.Header.Get("Authorization"))
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, "text", body["type"])
		require.Equal(t, "16315551234", body["to"])
		require.Equal(t, "Hello", body["text"].(map[string]any)["body"])
		index := min(requests, len(statuses)-1)
		requests++
		w.WriteHeader(statuses[index])
		_, err := fmt.Fprint(w, bodies[index])
		require.NoError(t, err)
	}))
	t.Cleanup(server.Close)
	return server, &requests
}

func lastSendMarker(t *testing.T, ctx *singledispatchtest.Context) singledispatch.Marker {
	t.Helper()
	heartbeats := ctx.Heartbeats()
	require.NotEmpty(t, heartbeats)
	var marker singledispatch.Marker
	require.NoError(t, json.Unmarshal(heartbeats[len(heartbeats)-1], &marker))
	return marker
}

func TestSendTextMessageCompletesFromCheckpointedMessageID(t *testing.T) {
	server, requests := newScriptedSendServer(t, []int{http.StatusOK}, []string{sentTextBody})
	client := newTestClient(t, server.URL, 0)
	ctx := singledispatchtest.NewContext("whatsapp-text", "send-text")
	sent, err := sdkgo.RunMutation(ctx, client.SendTextMessage(), testConnection, testTextInput)
	require.NoError(t, err)
	require.Equal(t, SendTextMessageBranchSent, sent.Branch)
	require.Equal(t, "wamid.example-text", sent.Value.MessageID)
	ctx.AdvanceAttempt()
	again, err := sdkgo.RunMutation(ctx, client.SendTextMessage(), testConnection, testTextInput)
	require.NoError(t, err)
	require.Equal(t, SendTextMessageBranchSent, again.Branch)
	require.Equal(t, "wamid.example-text", again.Value.MessageID)
	require.Equal(t, 1, *requests)
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
	require.Equal(t, singledispatch.StateCanResend, lastSendMarker(t, ctx).State)
	status = http.StatusOK
	ctx.AdvanceAttempt()
	sent, err := sdkgo.RunMutation(ctx, client.SendTemplateMessage(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, SendTemplateMessageBranchSent, sent.Branch)
	require.Equal(t, 2, requests)
}

func TestSendTextMessageTransientGraphErrorSelectsUncertainAtCap(t *testing.T) {
	server, requests := newScriptedSendServer(t, []int{http.StatusBadRequest}, []string{`{"error":{"code":131000,"is_transient":true}}`})
	client := newTestClient(t, server.URL, 0)
	ctx := singledispatchtest.NewContext("whatsapp-transient", "send-text")
	for attempt := 1; attempt < 3; attempt++ {
		_, err := sdkgo.RunMutation(ctx, client.SendTextMessage(), testConnection, testTextInput)
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry, "attempt %d resends", attempt)
		require.Equal(t, singledispatch.StateClaimed, lastSendMarker(t, ctx).State, "is_transient leaves the outcome unknown")
		ctx.AdvanceAttempt()
	}
	uncertain, err := sdkgo.RunMutation(ctx, client.SendTextMessage(), testConnection, testTextInput)
	require.NoError(t, err)
	require.Equal(t, SendTextMessageBranchUncertain, uncertain.Branch)
	require.Equal(t, 3, *requests)
}

func TestSendTextMessageNeverResendsAcceptedUnreadableResponse(t *testing.T) {
	for _, body := range []string{`{}`, `not JSON`, `{"messaging_product":"whatsapp","messages":[{"id":"wamid.` + strings.Repeat("x", 200) + `"}]}`} {
		t.Run(body, func(t *testing.T) {
			server, requests := newScriptedSendServer(t, []int{http.StatusOK}, []string{body})
			client := newTestClient(t, server.URL, 128)
			ctx := singledispatchtest.NewContext("whatsapp-accepted-unreadable", "send-text")
			uncertain, err := sdkgo.RunMutation(ctx, client.SendTextMessage(), testConnection, testTextInput)
			require.NoError(t, err)
			require.Equal(t, SendTextMessageBranchUncertain, uncertain.Branch)
			require.Equal(t, 1, *requests)
		})
	}
}

func TestSendTextMessageAcceptedBodyBreaksAfterHeadersSelectsUncertain(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(http.StatusOK)
		_, err := fmt.Fprint(w, `{"messages":`)
		require.NoError(t, err)
		w.(http.Flusher).Flush()
		connection, _, err := w.(http.Hijacker).Hijack()
		require.NoError(t, err)
		require.NoError(t, connection.Close())
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, 0)
	ctx := singledispatchtest.NewContext("whatsapp-body-breaks", "send-text")
	uncertain, err := sdkgo.RunMutation(ctx, client.SendTextMessage(), testConnection, testTextInput)
	require.NoError(t, err)
	require.Equal(t, SendTextMessageBranchUncertain, uncertain.Branch)
	require.Equal(t, 1, requests)
}

func TestSendTextMessageUnreadableClientErrorIsUnknown(t *testing.T) {
	longError := `{"error":{"code":100,"message":"` + strings.Repeat("x", 200) + `"}}`
	server, requests := newScriptedSendServer(t, []int{http.StatusBadRequest, http.StatusOK}, []string{longError, sentTextBody})
	client := newTestClient(t, server.URL, 128)
	ctx := singledispatchtest.NewContext("whatsapp-oversized-refusal", "send-text")
	_, err := sdkgo.RunMutation(ctx, client.SendTextMessage(), testConnection, testTextInput)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, singledispatch.StateClaimed, lastSendMarker(t, ctx).State)
	ctx.AdvanceAttempt()
	sent, err := sdkgo.RunMutation(ctx, client.SendTextMessage(), testConnection, testTextInput)
	require.NoError(t, err)
	require.Equal(t, SendTextMessageBranchSent, sent.Branch)
	require.Equal(t, 2, *requests)
}

func TestSendTextMessageUnlistedClientErrorWithMessageIDSelectsUncertain(t *testing.T) {
	server, requests := newScriptedSendServer(t, []int{http.StatusConflict}, []string{sentTextBody})
	client := newTestClient(t, server.URL, 0)
	ctx := singledispatchtest.NewContext("whatsapp-conflict-id", "send-text")
	uncertain, err := sdkgo.RunMutation(ctx, client.SendTextMessage(), testConnection, testTextInput)
	require.NoError(t, err)
	require.Equal(t, SendTextMessageBranchUncertain, uncertain.Branch)
	require.Equal(t, 1, *requests)
}

func TestSendTextMessageRefusalAfterRateLimitedResendSelectsUncertain(t *testing.T) {
	server, requests := newScriptedSendServer(t, []int{http.StatusInternalServerError, http.StatusTooManyRequests, http.StatusBadRequest},
		[]string{``, ``, `{"error":{"code":100}}`})
	client := newTestClient(t, server.URL, 0)
	ctx := singledispatchtest.NewContext("whatsapp-rate-limited-resend", "send-text")
	for attempt := 1; attempt < 3; attempt++ {
		_, err := sdkgo.RunMutation(ctx, client.SendTextMessage(), testConnection, testTextInput)
		require.Error(t, err, "attempt %d retries", attempt)
		ctx.AdvanceAttempt()
	}
	uncertain, err := sdkgo.RunMutation(ctx, client.SendTextMessage(), testConnection, testTextInput)
	require.NoError(t, err)
	require.Equal(t, SendTextMessageBranchUncertain, uncertain.Branch)
	require.Equal(t, 3, *requests)
}

func TestSendTextMessageInvalidCredentialsAfterPossibleSendSelectsUncertain(t *testing.T) {
	server, requests := newScriptedSendServer(t, []int{http.StatusInternalServerError}, []string{``})
	client := newTestClient(t, server.URL, 0)
	ctx := singledispatchtest.NewContext("whatsapp-invalid-credentials", "send-text")
	_, err := sdkgo.RunMutation(ctx, client.SendTextMessage(), testConnection, testTextInput)
	require.Error(t, err)
	ctx.AdvanceAttempt()
	invalidCredentials, err := New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AccessToken: sdkgo.NewSecretString("bad\ntoken")}})
	require.NoError(t, err)
	uncertain, err := sdkgo.RunMutation(ctx, invalidCredentials.SendTextMessage(), testConnection, testTextInput)
	require.NoError(t, err)
	require.Equal(t, SendTextMessageBranchUncertain, uncertain.Branch)
	require.Equal(t, 1, *requests)
}

func TestSendTextMessageResendsUnknownOutcomeBelowCap(t *testing.T) {
	for _, response := range []struct {
		status int
		body   string
	}{{500, ``}, {502, ``}, {408, ``}, {409, ``}, {425, ``}, {418, ``}, {400, `{"error":{"code":100,"is_transient":true}}`}} {
		t.Run(fmt.Sprintf("%d %s", response.status, response.body), func(t *testing.T) {
			server, requests := newScriptedSendServer(t, []int{response.status, http.StatusOK}, []string{response.body, sentTextBody})
			client := newTestClient(t, server.URL, 0)
			ctx := singledispatchtest.NewContext("whatsapp-resend", "send-text")
			_, err := sdkgo.RunMutation(ctx, client.SendTextMessage(), testConnection, testTextInput)
			require.Error(t, err)
			require.Equal(t, singledispatch.StateClaimed, lastSendMarker(t, ctx).State)
			ctx.AdvanceAttempt()
			sent, err := sdkgo.RunMutation(ctx, client.SendTextMessage(), testConnection, testTextInput)
			require.NoError(t, err)
			require.Equal(t, SendTextMessageBranchSent, sent.Branch)
			require.EqualValues(t, 2, lastSendMarker(t, ctx).SendCount)
			require.Equal(t, 2, *requests)
		})
	}
}

func TestSendTextMessageSelectsUncertainAtCap(t *testing.T) {
	server, requests := newScriptedSendServer(t, []int{http.StatusInternalServerError}, []string{``})
	client := newTestClient(t, server.URL, 0)
	ctx := singledispatchtest.NewContext("whatsapp-cap", "send-text")
	for attempt := 1; attempt < 3; attempt++ {
		_, err := sdkgo.RunMutation(ctx, client.SendTextMessage(), testConnection, testTextInput)
		require.Error(t, err, "attempt %d resends", attempt)
		ctx.AdvanceAttempt()
	}
	uncertain, err := sdkgo.RunMutation(ctx, client.SendTextMessage(), testConnection, testTextInput)
	require.NoError(t, err)
	require.Equal(t, SendTextMessageBranchUncertain, uncertain.Branch)
	ctx.AdvanceAttempt()
	refused, err := sdkgo.RunMutation(ctx, client.SendTextMessage(), testConnection, testTextInput)
	require.NoError(t, err)
	require.Equal(t, SendTextMessageBranchUncertain, refused.Branch)
	require.Equal(t, 3, *requests)
}

func TestSendTextMessageHonorsApplicationSendCap(t *testing.T) {
	server, requests := newScriptedSendServer(t, []int{http.StatusBadGateway}, []string{``})
	client := newTestClient(t, server.URL, 0)
	ctx := singledispatchtest.NewContext("whatsapp-override", "send-text")
	uncertain, err := sdkgo.RunMutation(ctx, client.SendTextMessage(), testConnection, testTextInput, sdkgo.WithMaxSends(1))
	require.NoError(t, err)
	require.Equal(t, SendTextMessageBranchUncertain, uncertain.Branch)
	require.Equal(t, 1, *requests)
}

func TestSendTextMessageIncompleteAcceptanceIsNeverResent(t *testing.T) {
	server, requests := newScriptedSendServer(t, []int{http.StatusOK}, []string{`{"messages":[{"id":"wamid.example-text"}]}`})
	client := newTestClient(t, server.URL, 0)
	ctx := singledispatchtest.NewContext("whatsapp-incomplete", "send-text")
	uncertain, err := sdkgo.RunMutation(ctx, client.SendTextMessage(), testConnection, testTextInput)
	require.NoError(t, err)
	require.Equal(t, SendTextMessageBranchUncertain, uncertain.Branch)
	require.Equal(t, 1, *requests)
}

func TestSendTextMessageLostProviderIDCheckpointStillSent(t *testing.T) {
	ctx := singledispatchtest.NewContext("whatsapp-lost-checkpoint", "send-text")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		ctx.SetWriteError(errors.New("heartbeat lost"))
		_, err := fmt.Fprint(w, sentTextBody)
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, 0)
	result, err := sdkgo.RunMutation(ctx, client.SendTextMessage(), testConnection, testTextInput)
	require.NoError(t, err)
	require.Equal(t, SendTextMessageBranchSent, result.Branch)
	require.Equal(t, "wamid.example-text", result.Value.MessageID)
}

func TestSendTextMessageNeverResendsDefiniteRefusal(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server, requests := newScriptedSendServer(t, []int{status}, []string{`{"error":{"code":131026,"message":"customer and secret should not be copied"}}`})
			client := newTestClient(t, server.URL, 0)
			ctx := singledispatchtest.NewContext("whatsapp-rejected", "send-text")
			result, err := sdkgo.RunMutation(ctx, client.SendTextMessage(), testConnection, testTextInput)
			require.NoError(t, err)
			require.Equal(t, SendTextMessageBranchProviderRejected, result.Branch)
			require.NotContains(t, result.Failure.Message, "customer")
			require.NotContains(t, result.Failure.Message, testAccessToken)
			require.Equal(t, singledispatch.StateCanResend, lastSendMarker(t, ctx).State)
			require.Equal(t, 1, *requests)
		})
	}
}

func TestSendTextMessageRefusalOfResendSelectsUncertain(t *testing.T) {
	server, requests := newScriptedSendServer(t, []int{http.StatusInternalServerError, http.StatusBadRequest}, []string{``, `{"error":{"code":100}}`})
	client := newTestClient(t, server.URL, 0)
	ctx := singledispatchtest.NewContext("whatsapp-refused-resend", "send-text")
	_, err := sdkgo.RunMutation(ctx, client.SendTextMessage(), testConnection, testTextInput)
	require.Error(t, err)
	ctx.AdvanceAttempt()
	uncertain, err := sdkgo.RunMutation(ctx, client.SendTextMessage(), testConnection, testTextInput)
	require.NoError(t, err)
	require.Equal(t, SendTextMessageBranchUncertain, uncertain.Branch)
	require.Equal(t, 2, *requests)
}

func TestSendTextMessageFailClosedReturnsRetry(t *testing.T) {
	server, requests := newScriptedSendServer(t, []int{http.StatusOK}, []string{sentTextBody})
	client := newTestClient(t, server.URL, 0)
	ctx := singledispatchtest.NewContext("whatsapp-fail-closed", "send-text")
	ctx.SetReadError(errors.New("heartbeat unavailable"))
	_, err := sdkgo.RunMutation(ctx, client.SendTextMessage(), testConnection, testTextInput)
	require.Error(t, err)
	require.Equal(t, 0, *requests)
	ctx.SetReadError(nil)
	sent, err := sdkgo.RunMutation(ctx, client.SendTextMessage(), testConnection, testTextInput)
	require.NoError(t, err)
	require.Equal(t, SendTextMessageBranchSent, sent.Branch)
}

func TestSendTextMessageCredentialFailureAfterPossibleSendRetries(t *testing.T) {
	server, requests := newScriptedSendServer(t, []int{http.StatusInternalServerError}, []string{``})
	client := newTestClient(t, server.URL, 0)
	ctx := singledispatchtest.NewContext("whatsapp-credentials", "send-text")
	_, err := sdkgo.RunMutation(ctx, client.SendTextMessage(), testConnection, testTextInput)
	require.Error(t, err)
	ctx.AdvanceAttempt()
	noCredentials, err := New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.NoError(t, err)
	_, err = sdkgo.RunMutation(ctx, noCredentials.SendTextMessage(), testConnection, testTextInput)
	require.Error(t, err)
	require.Equal(t, 1, *requests)
}

// failingCredentialSource fails every resolution with one credential error.
type failingCredentialSource struct{ err error }

func (source failingCredentialSource) Resolve(sdkgo.Call) (Credentials, error) {
	return Credentials{}, source.err
}

func TestSendTextMessageTransientCredentialFailureBeforeSendRetries(t *testing.T) {
	for _, credentialErr := range []error{projectconfig.ErrCredentialBrokerUnavailable, errors.New("project store read failed")} {
		t.Run(credentialErr.Error(), func(t *testing.T) {
			server, requests := newScriptedSendServer(t, []int{http.StatusOK}, []string{sentTextBody})
			unavailable, err := New(Config{BaseURL: server.URL}, failingCredentialSource{err: credentialErr})
			require.NoError(t, err)
			ctx := singledispatchtest.NewContext("whatsapp-transient-credentials", "send-text")
			_, err = sdkgo.RunMutation(ctx, unavailable.SendTextMessage(), testConnection, testTextInput)
			var retryErr *sdkgo.RetryError
			require.ErrorAs(t, err, &retryErr)
			require.Equal(t, 0, *requests)
			require.Equal(t, singledispatch.StateNotSent, lastSendMarker(t, ctx).State)
			ctx.AdvanceAttempt()
			sent, err := sdkgo.RunMutation(ctx, newTestClient(t, server.URL, 0).SendTextMessage(), testConnection, testTextInput)
			require.NoError(t, err)
			require.Equal(t, SendTextMessageBranchSent, sent.Branch)
			require.EqualValues(t, 1, lastSendMarker(t, ctx).SendCount)
			require.Equal(t, 1, *requests)
		})
	}
}

func TestSendTextMessageTerminalCredentialFailureBeforeSendSelectsDefect(t *testing.T) {
	terminalErrors := []error{sdkgo.ErrReauthorizationRequired, projectconfig.ErrReauthorizationRequired, projectconfig.ErrObjectNotFound,
		projectconfig.ErrConnectionNotSelected, projectconfig.ErrCredentialBrokerRejected}
	for _, credentialErr := range terminalErrors {
		t.Run(credentialErr.Error(), func(t *testing.T) {
			server, requests := newScriptedSendServer(t, []int{http.StatusOK}, []string{sentTextBody})
			client, err := New(Config{BaseURL: server.URL}, failingCredentialSource{err: fmt.Errorf("resolve: %w", credentialErr)})
			require.NoError(t, err)
			defect, err := sdkgo.RunMutation(singledispatchtest.NewContext("whatsapp-terminal-credentials", "send-text"), client.SendTextMessage(), testConnection, testTextInput)
			require.NoError(t, err)
			require.Equal(t, SendTextMessageBranchDefect, defect.Branch)
			require.Equal(t, 0, *requests)
		})
	}
}

func TestSendTextMessageReadsBodyArrivingAfterHeaders(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		time.Sleep(50 * time.Millisecond)
		_, err := fmt.Fprint(w, sentTextBody)
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, 0)
	ctx := singledispatchtest.NewContext("whatsapp-late-body", "send-text")
	sent, err := sdkgo.RunMutation(ctx, client.SendTextMessage(), testConnection, testTextInput)
	require.NoError(t, err)
	require.Equal(t, SendTextMessageBranchSent, sent.Branch)
	require.Equal(t, 1, requests)
}
