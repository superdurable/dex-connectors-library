// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package acrobatsign

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestCreateAgreementReminderSendsSelectedParticipantIDs(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/rest/v6/baseUris":
			_, err := fmt.Fprintf(writer, `{"apiAccessPoint":%q}`, server.URL+"/")
			require.NoError(t, err)
		case "/api/rest/v6/agreements/3AAABLblqZNOTREALAGREEMENTID5_BjiH/reminders":
			require.Equal(t, http.MethodPost, request.Method)
			require.Equal(t, "Bearer sample-acrobat-sign-integration-key", request.Header.Get("Authorization"))
			body, err := io.ReadAll(request.Body)
			require.NoError(t, err)
			require.JSONEq(t, `{"recipientParticipantIds":["participant-123"],"status":"ACTIVE"}`, string(body))
			_, err = writer.Write([]byte(`{"id":"reminder-123","email":"private@example.com"}`))
			require.NoError(t, err)
		default:
			t.Fatalf("unexpected path %s", request.URL.Path)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/api/rest/v6", 0)
	result, err := sdkgo.RunMutation(queryContext{}, client.CreateAgreementReminder(), testConnection, CreateAgreementReminderInput{
		AgreementID: "3AAABLblqZNOTREALAGREEMENTID5_BjiH", RecipientParticipantIDs: []string{"participant-123"},
	})
	require.NoError(t, err)
	require.Equal(t, CreateAgreementReminderBranchCreated, result.Branch)
	require.Equal(t, CreatedAgreementReminder{ReminderID: "reminder-123"}, result.Value)
}

func TestCreateAgreementReminderClassifiesOutcomes(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{name: "rejected", status: 403, body: `{"message":"private@example.com"}`, branch: CreateAgreementReminderBranchProviderRejected},
		{name: "uncertain", status: 200, body: `{}`, branch: CreateAgreementReminderBranchUncertain},
		{name: "429", status: 429, body: `{}`, retry: true},
		{name: "503", status: 503, body: `{}`, retry: true},
		{name: "500", status: 500, body: `{}`, retry: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.URL.Path == "/api/rest/v6/baseUris" {
					_, err := fmt.Fprintf(writer, `{"apiAccessPoint":%q}`, server.URL+"/")
					require.NoError(t, err)
					return
				}
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			client := newTestClient(t, server.URL+"/api/rest/v6", 0)
			result, err := sdkgo.RunMutation(queryContext{}, client.CreateAgreementReminder(), testConnection, CreateAgreementReminderInput{
				AgreementID: "3AAABLblqZNOTREALAGREEMENTID5_BjiH", RecipientParticipantIDs: []string{"participant-123"},
			})
			if testCase.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
			require.NotContains(t, result.Failure.Message, "private@example.com")
		})
	}
}
