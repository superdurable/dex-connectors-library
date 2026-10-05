// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jiraservicemanagement_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	jiraservicemanagement "github.com/superdurable/dex-connectors-library/connectors/atlassian/jira-service-management"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const createdRequestJSON = `{"issueId":"10042","issueKey":"ITH-42","requestTypeId":"25","serviceDeskId":"10",` +
	`"createdDate":{"epochMillis":1790000000000,"iso8601":"2026-09-21T14:13:20+0000"},` +
	`"reporter":{"accountId":"` + testCustomerAccount + `","emailAddress":"SENTINEL-jane@example.com"},` +
	`"currentStatus":{"status":"Waiting for support","statusCategory":"NEW"}}`

func validCreateTicketInput() jiraservicemanagement.CreateTicketInput {
	return jiraservicemanagement.CreateTicketInput{
		ServiceDeskID: "10", RequestTypeID: "25", Summary: " Laptop will not boot ",
		Description: "My laptop will not boot.\nError *0x7B*.", RaiseOnBehalfOfAccountID: testCustomerAccount,
		AdditionalFieldValues: map[string]json.RawMessage{"customfield_10050": json.RawMessage(`{"value":"Hardware"}`)},
	}
}

func TestCreateTicketRaisesOneRequestOnTheCustomersBehalf(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusCreated, createdRequestJSON)
	})
	ctx := newTestDexContext("create")
	result, err := sdkgo.RunMutation(ctx, newTestClient(t, provider.URL).CreateTicket(), jsmConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, jiraservicemanagement.CreateTicketBranchCreated, result.Branch)
	require.Equal(t, "ITH-42", result.Value.IssueKey)
	require.Equal(t, "10042", result.Value.IssueID)
	require.Equal(t, &jiraservicemanagement.RequestStatus{Name: "Waiting for support", Category: jiraservicemanagement.RequestStatusCategoryNew}, result.Value.RequestStatus)
	require.Equal(t, time.UnixMilli(1790000000000).UTC(), *result.Value.CreatedAt)
	require.Equal(t, "ITH-42", result.Receipt.ProviderObjectID)
	require.Equal(t, 1, provider.requestCount())
	request := provider.request(0)
	require.Equal(t, http.MethodPost, request.method)
	require.Equal(t, testServiceDeskPath+"/request", request.path)
	require.Empty(t, request.experimental, "request creation is not an experimental endpoint")
	require.JSONEq(t, `{"serviceDeskId":"10","requestTypeId":"25","raiseOnBehalfOf":"`+testCustomerAccount+`",
		"requestFieldValues":{"summary":"Laptop will not boot","description":"My laptop will not boot.\nError *0x7B*.","customfield_10050":{"value":"Hardware"}}}`, request.body)
	require.JSONEq(t, `{"jiraServiceManagementDispatchedCallId":"`+string(result.Receipt.CallID)+`"}`, string(ctx.recordedHeartbeat),
		"the dispatch marker is recorded before the request")
	requireNoSentinel(t, result)
}

func TestCreateTicketNeverResendsAnUnconfirmedRequest(t *testing.T) {
	for _, test := range []struct {
		name  string
		reply func(*testing.T, http.ResponseWriter)
		kind  sdkgo.FailureKind
	}{
		{"server error", func(t *testing.T, response http.ResponseWriter) {
			writeJSON(t, response, http.StatusInternalServerError, `{"errorMessage":"SENTINEL internal"}`)
		}, sdkgo.FailureAvailability},
		{"dropped connection", func(t *testing.T, response http.ResponseWriter) { dropConnection(t, response) }, sdkgo.FailureTransport},
		{"unusable 2xx", func(t *testing.T, response http.ResponseWriter) {
			writeJSON(t, response, http.StatusCreated, `{"issueKey":"SENTINEL"}`)
		}, sdkgo.FailureProtocol},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) { test.reply(t, response) })
			ctx := newTestDexContext("create-" + test.name)
			result, err := sdkgo.RunMutation(ctx, newTestClient(t, provider.URL).CreateTicket(), jsmConnection, validCreateTicketInput())
			require.NoError(t, err)
			require.Equal(t, jiraservicemanagement.CreateTicketBranchUncertain, result.Branch)
			require.Equal(t, test.kind, result.Failure.Kind)
			require.Equal(t, "Laptop will not boot", result.Value.Summary, "the request is echoed for reconciliation")
			require.Empty(t, result.Value.IssueKey)
			require.NotEmpty(t, ctx.recordedHeartbeat, "the marker stays, so a replayed attempt sends nothing")
			requireNoSentinel(t, result)

			replay, err := sdkgo.RunMutation(ctx.nextAttempt(), newTestClient(t, provider.URL).CreateTicket(), jsmConnection, validCreateTicketInput())
			require.NoError(t, err)
			require.Equal(t, jiraservicemanagement.CreateTicketBranchUncertain, replay.Branch)
			require.Equal(t, 1, provider.requestCount(), "a later attempt of the same Step execution never resends")
		})
	}
}

func TestCreateTicketRetriesOnlyWhenNothingWasApplied(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		if index == 0 {
			response.Header().Set("Retry-After", "3")
			writeJSON(t, response, http.StatusTooManyRequests, `{"errorMessage":"SENTINEL slow down"}`)
			return
		}
		writeJSON(t, response, http.StatusCreated, createdRequestJSON)
	})
	ctx := newTestDexContext("create-rate-limited")
	_, err := sdkgo.RunMutation(ctx, newTestClient(t, provider.URL).CreateTicket(), jsmConnection, validCreateTicketInput())
	requireRetry(t, err, sdkgo.FailureRateLimit)
	require.Nil(t, ctx.recordedHeartbeat, "a 429 clears the marker so the retry may send")
	next := ctx.nextAttempt()
	result, err := sdkgo.RunMutation(next, newTestClient(t, provider.URL).CreateTicket(), jsmConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, jiraservicemanagement.CreateTicketBranchCreated, result.Branch)
	require.Equal(t, 2, provider.requestCount())

	refused := newTestDexContext("create-refused")
	_, err = sdkgo.RunMutation(refused, newTestClient(t, closedLoopbackURL(t)).CreateTicket(), jsmConnection, validCreateTicketInput())
	requireRetry(t, err, sdkgo.FailureTransport)
	require.Nil(t, refused.recordedHeartbeat, "a refused connection sent nothing")
}

func TestCreateTicketDoesNotSendWithoutARecordedCheckpoint(t *testing.T) {
	provider := newRecordingProvider(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	unrecordable := newTestDexContext("create-unrecordable")
	unrecordable.rejectsHeartbeat = true
	_, err := sdkgo.RunMutation(unrecordable, newTestClient(t, provider.URL).CreateTicket(), jsmConnection, validCreateTicketInput())
	requireRetry(t, err, sdkgo.FailureAvailability)
	require.Zero(t, provider.requestCount())
}

func TestCreateTicketRejectionNamesFieldsAndErrorKeyButNoProviderText(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusBadRequest, `{"errorMessage":"SENTINEL Field 'customfield_10050' is required",`+
			`"i18nErrorMessage":{"i18nKey":"sd.validation.request.field.required","parameters":["SENTINEL owner@example.com"]},`+
			`"errors":{"customfield_10050":"SENTINEL required","SENTINEL bad key!":"x"}}`)
	})
	result, err := sdkgo.RunMutation(newTestDexContext("create-rejected"), newTestClient(t, provider.URL).CreateTicket(), jsmConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, jiraservicemanagement.CreateTicketBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
	require.Equal(t, "Jira Service Management rejected the customer request with HTTP 400 (fields: customfield_10050) [sd.validation.request.field.required]", result.Failure.Message)
	require.Equal(t, []string{"customfield_10050"}, result.Value.RejectedFieldIDs)
	require.Equal(t, "sd.validation.request.field.required", result.Value.ProviderErrorKey)
	requireNoSentinel(t, result)
}

func TestCreateTicketRejectsInvalidInputWithoutARequest(t *testing.T) {
	provider := newRecordingProvider(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	for name, mutate := range map[string]func(*jiraservicemanagement.CreateTicketInput){
		"service desk":       func(input *jiraservicemanagement.CreateTicketInput) { input.ServiceDeskID = "IT" },
		"request type":       func(input *jiraservicemanagement.CreateTicketInput) { input.RequestTypeID = "" },
		"summary line break": func(input *jiraservicemanagement.CreateTicketInput) { input.Summary = "Laptop\nbroken" },
		"customer": func(input *jiraservicemanagement.CreateTicketInput) {
			input.RaiseOnBehalfOfAccountID = "jane@example.com"
		},
		"summary field": func(input *jiraservicemanagement.CreateTicketInput) {
			input.AdditionalFieldValues = map[string]json.RawMessage{"summary": json.RawMessage(`"x"`)}
		},
		"invalid JSON": func(input *jiraservicemanagement.CreateTicketInput) {
			input.AdditionalFieldValues = map[string]json.RawMessage{"customfield_1": json.RawMessage(`{`)}
		},
	} {
		input := validCreateTicketInput()
		mutate(&input)
		ctx := newTestDexContext("create-invalid-" + name)
		result, err := sdkgo.RunMutation(ctx, newTestClient(t, provider.URL).CreateTicket(), jsmConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, jiraservicemanagement.CreateTicketBranchDefect, result.Branch, name)
		require.Zero(t, ctx.heartbeatCount, name)
	}
}
