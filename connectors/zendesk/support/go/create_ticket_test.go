// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package support_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zendesk/support/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func validCreateTicketInput() support.CreateTicketInput {
	return support.CreateTicketInput{
		Subject: "Double charge on order 88213", Comment: support.TicketCommentInput{Body: "I was charged twice."},
		Requester: &support.TicketRequesterInput{Email: "jane@acme.example.com", Name: "Jane Smith"},
		Priority:  support.TicketPriorityHigh, Type: support.TicketTypeProblem, Tags: []string{"billing", "refund"},
		GroupID: 98738, AssigneeID: 235323, BrandID: 1234, ExternalID: "ERP-88213",
	}
}

func TestCreateTicketSendsTheStepIdempotencyKeyAndADeterministicBody(t *testing.T) {
	provider := newRecordingZendesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		response.Header().Set("X-Idempotency-Lookup", "miss")
		writeJSON(t, response, http.StatusCreated, `{"ticket":`+mustJSON(t, ticketJSON(35436, "new", []string{"billing", "refund"}))+`,"audit":{"id":1}}`)
	})
	client := newZendeskClient(t, provider.URL)
	result, err := sdkgo.RunMutation(newZendeskDexContext("create-ticket"), client.CreateTicket(), zendeskConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, support.CreateTicketBranchCreated, result.Branch)
	require.Equal(t, int64(35436), result.Value.Ticket.ID)
	require.False(t, result.Value.WasIdempotentReplay)
	require.Equal(t, "35436", result.Receipt.ProviderObjectID)
	require.Equal(t, map[string]string{"idempotencyLookup": "miss"}, result.Receipt.Metadata)
	require.NotEmpty(t, result.Receipt.IdempotencyKey)

	request := provider.request(0)
	require.Equal(t, http.MethodPost, request.method)
	require.Equal(t, "/api/v2/tickets", request.path)
	require.Equal(t, "application/json", request.header.Get("Content-Type"))
	require.Equal(t, string(result.Receipt.IdempotencyKey), request.header.Get("Idempotency-Key"))
	require.JSONEq(t, `{"ticket":{
		"subject":"Double charge on order 88213","comment":{"body":"I was charged twice.","public":true},
		"requester":{"email":"jane@acme.example.com","name":"Jane Smith"},"priority":"high","type":"problem",
		"tags":["billing","refund"],"group_id":98738,"assignee_id":235323,"brand_id":1234,"external_id":"ERP-88213",
		"metadata":{"dex_idempotency_key":"`+string(result.Receipt.IdempotencyKey)+`"}}}`, request.body)

	_, err = sdkgo.RunMutation(newZendeskDexContext("create-ticket"), client.CreateTicket(), zendeskConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, request.header.Get("Idempotency-Key"), provider.request(1).header.Get("Idempotency-Key"), "one Step execution keeps one key")
	require.Equal(t, request.body, provider.request(1).body, "Zendesk rejects a repeated key with a different body")

	_, err = sdkgo.RunMutation(newZendeskDexContext("another-step"), client.CreateTicket(), zendeskConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.NotEqual(t, request.header.Get("Idempotency-Key"), provider.request(2).header.Get("Idempotency-Key"), "a new Step execution is a new ticket")
}

func TestCreateTicketReportsAReplayedResponseAndInternalFirstComments(t *testing.T) {
	provider := newRecordingZendesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		response.Header().Set("X-Idempotency-Lookup", "hit")
		writeJSON(t, response, http.StatusCreated, ticketEnvelopeJSON(35436, "new", nil))
	})
	input := support.CreateTicketInput{Subject: "Internal escalation", Comment: support.TicketCommentInput{Body: "Agent-only context.", IsInternalNote: true}}
	result, err := sdkgo.RunMutation(newZendeskDexContext("create-replay"), newZendeskClient(t, provider.URL).CreateTicket(), zendeskConnection, input)
	require.NoError(t, err)
	require.Equal(t, support.CreateTicketBranchCreated, result.Branch)
	require.True(t, result.Value.WasIdempotentReplay)
	var body struct {
		Ticket map[string]json.RawMessage `json:"ticket"`
	}
	require.NoError(t, json.Unmarshal([]byte(provider.request(0).body), &body))
	require.JSONEq(t, `{"body":"Agent-only context.","public":false}`, string(body.Ticket["comment"]))
	require.NotContains(t, body.Ticket, "requester", "a nil requester lets Zendesk use the connection's agent")
	require.NotContains(t, body.Ticket, "tags")
}

func TestCreateTicketRetriesEveryUnknownOutcomeUnderTheSameKey(t *testing.T) {
	for _, status := range []int{
		http.StatusFound, http.StatusRequestTimeout, http.StatusConflict, http.StatusGone, http.StatusTeapot,
		http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusServiceUnavailable,
	} {
		provider := newRecordingZendesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
			writeJSON(t, response, status, `{"error":"SENTINEL"}`)
		})
		_, err := sdkgo.RunMutation(newZendeskDexContext("create-retry"), newZendeskClient(t, provider.URL).CreateTicket(), zendeskConnection, validCreateTicketInput())
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry, "HTTP %d", status)
		require.NotEmpty(t, provider.request(0).header.Get("Idempotency-Key"))
	}
}

func TestCreateTicketRetriesAnUnusableCreatedResponse(t *testing.T) {
	for name, body := range map[string]string{
		"created ticket missing": `{"audit":{"id":1}}`,
		"reflects credential":    `{"ticket":{"id":35436,"subject":"` + testAPIToken + `"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			provider := newRecordingZendesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, http.StatusCreated, body)
			})
			_, err := sdkgo.RunMutation(newZendeskDexContext("create-unusable"), newZendeskClient(t, provider.URL).CreateTicket(), zendeskConnection, validCreateTicketInput())
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry, "the next attempt repeats the keyed request")
			require.Equal(t, sdkgo.FailureProtocol, retry.Failure.Kind)
			require.NotContains(t, retry.Failure.Message, testAPIToken)
		})
	}
}

func TestCreateTicketRejections(t *testing.T) {
	for _, test := range []struct {
		name    string
		status  int
		body    string
		branch  sdkgo.BranchID
		kind    sdkgo.FailureKind
		message string
	}{
		{
			name: "key reused with another body", status: http.StatusBadRequest,
			body:   `{"error":"IdempotentRequestError","description":"Request parameters don't match the given idempotency key"}`,
			branch: support.CreateTicketBranchProviderRejected, kind: sdkgo.FailureConflict,
			message: "Zendesk rejected the request (HTTP 400) [IdempotentRequestError]",
		},
		{
			name: "invalid requester", status: http.StatusUnprocessableEntity,
			body:   `{"error":"RecordInvalid","description":"SENTINEL","details":{"requester":[{"description":"SENTINEL Requester: Email is invalid","error":"InvalidValue"}]}}`,
			branch: support.CreateTicketBranchProviderRejected, kind: sdkgo.FailureValidation,
			message: "Zendesk rejected the request (HTTP 422) [RecordInvalid; details: requester=InvalidValue]",
		},
		{
			name: "missing permission", status: http.StatusForbidden, body: `{"error":"Forbidden"}`,
			branch: support.CreateTicketBranchProviderRejected, kind: sdkgo.FailureAuthorization,
			message: "Zendesk rejected the request (HTTP 403) [Forbidden]",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingZendesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, test.status, test.body)
			})
			result, err := sdkgo.RunMutation(newZendeskDexContext("create-"+test.name), newZendeskClient(t, provider.URL).CreateTicket(), zendeskConnection, validCreateTicketInput())
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.Equal(t, test.kind, result.Failure.Kind)
			require.Equal(t, test.message, result.Failure.Message)
			require.NotContains(t, mustJSON(t, result), "SENTINEL")
		})
	}
}

func TestCreateTicketValidatesInputBeforeAnyRequest(t *testing.T) {
	for name, mutate := range map[string]func(*support.CreateTicketInput){
		"blank subject":     func(input *support.CreateTicketInput) { input.Subject = " " },
		"blank comment":     func(input *support.CreateTicketInput) { input.Comment.Body = "" },
		"requester address": func(input *support.CreateTicketInput) { input.Requester.Email = "jane" },
		"priority":          func(input *support.CreateTicketInput) { input.Priority = "critical" },
		"type":              func(input *support.CreateTicketInput) { input.Type = "bug" },
		"tag":               func(input *support.CreateTicketInput) { input.Tags = []string{"Needs Review"} },
		"group":             func(input *support.CreateTicketInput) { input.GroupID = -1 },
	} {
		t.Run(name, func(t *testing.T) {
			provider := newRecordingZendesk(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
			input := validCreateTicketInput()
			mutate(&input)
			result, err := sdkgo.RunMutation(newZendeskDexContext("create-defect-"+name), newZendeskClient(t, provider.URL).CreateTicket(), zendeskConnection, input)
			require.NoError(t, err)
			require.Equal(t, support.CreateTicketBranchDefect, result.Branch)
			require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
		})
	}
}

func TestCreateTicketCredentialFailureSelectsDefectOnlyWhenTerminalOnTheFirstAttempt(t *testing.T) {
	for _, test := range []struct {
		name     string
		err      error
		attempt  int32
		isDefect bool
	}{
		{name: "terminal first attempt", err: sdkgo.ErrReauthorizationRequired, attempt: 1, isDefect: true},
		{name: "transient first attempt", err: errors.New("credential broker unavailable"), attempt: 1},
		{name: "terminal later attempt", err: sdkgo.ErrReauthorizationRequired, attempt: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingZendesk(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
			client := newSwitchableZendeskClient(t, provider.URL, &switchableCredentials{err: test.err})
			dexContext := newZendeskDexContext("create-credentials-" + test.name)
			dexContext.attempt = test.attempt
			result, err := sdkgo.RunMutation(dexContext, client.CreateTicket(), zendeskConnection, validCreateTicketInput())
			if test.isDefect {
				require.NoError(t, err)
				require.Equal(t, support.CreateTicketBranchDefect, result.Branch)
				return
			}
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
		})
	}
}

func TestCreateTicketRefusalWithAnUnreadableOrContradictoryBodyRetries(t *testing.T) {
	for name, body := range map[string]string{
		"oversized":        `{"error":"RecordInvalid","description":"` + strings.Repeat("x", 4096) + `"}`,
		"carries a ticket": `{"error":"RecordInvalid","ticket":{"id":35437}}`,
	} {
		t.Run(name, func(t *testing.T) {
			provider := newRecordingZendesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, http.StatusUnprocessableEntity, body)
			})
			client, err := support.New(support.Config{Subdomain: testSubdomain, MaxResponseBytes: 2048}, testCredentialProvider(), support.WithAPIBaseURL(provider.URL+"/api/v2"))
			require.NoError(t, err)
			_, err = sdkgo.RunMutation(newZendeskDexContext("create-unknown-refusal"), client.CreateTicket(), zendeskConnection, validCreateTicketInput())
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry, "the keyed request is repeated")
		})
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	return string(encoded)
}
