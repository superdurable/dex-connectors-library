// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package marketo

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestCreateLeadUsesCreateOnlyAndOmitsEmailFromResult(t *testing.T) {
	firstName, lastName := "Sample", "Lead"
	client := testClient(t, func(request *http.Request) (*http.Response, error) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/rest/v1/leads.json", request.URL.Path)
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		require.JSONEq(t, `{"action":"createOnly","lookupField":"email","input":[{"email":"sample@example.com","firstName":"Sample","lastName":"Lead"}]}`, string(body))
		return response(200, `{"success":true,"result":[{"id":318582,"status":"created","email":"private@example.com"}]}`), nil
	})
	result, err := sdkgo.RunMutation(queryContext{}, client.CreateLead(), testConnection, CreateLeadInput{Email: "sample@example.com", FirstName: &firstName, LastName: &lastName})
	require.NoError(t, err)
	require.Equal(t, CreateLeadBranchCreated, result.Branch)
	require.Equal(t, CreatedLead{ID: 318582, Status: "created"}, result.Value)
}

func TestCreateLeadClassifiesDuplicateRetryAndUncertainty(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		status    int
		body      string
		branch    sdkgo.BranchID
		retry     bool
		uncertain bool
	}{
		{name: "duplicate", status: 200, body: `{"success":true,"result":[{"status":"skipped","reasons":[{"code":"1005","message":"secret"}]}]}`, branch: CreateLeadBranchAlreadyExists},
		{name: "denied", status: 403, body: `{"error":"secret"}`, branch: CreateLeadBranchProviderRejected},
		{name: "rate limited", status: 429, body: `{}`, retry: true},
		{name: "unavailable", status: 503, body: `{}`, retry: true},
		{name: "unknown", status: 500, body: `{}`, retry: true},
		{name: "malformed accepted", status: 200, body: `{}`, uncertain: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			client := testClient(t, func(*http.Request) (*http.Response, error) { return response(testCase.status, testCase.body), nil })
			result, err := sdkgo.RunMutation(queryContext{}, client.CreateLead(), testConnection, CreateLeadInput{Email: "sample@example.com"})
			if testCase.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				return
			}
			if testCase.uncertain {
				require.NoError(t, err)
				require.Equal(t, sdkgo.UncertainBranchID, result.Branch)
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
			require.NotContains(t, result.Failure.Message, "secret")
		})
	}
}

func TestCreateLeadRejectsBadEmailBeforeSending(t *testing.T) {
	client := testClient(t, func(*http.Request) (*http.Response, error) { t.Fatal("unexpected request"); return nil, nil })
	result, err := sdkgo.RunMutation(queryContext{}, client.CreateLead(), testConnection, CreateLeadInput{Email: "invalid"})
	require.NoError(t, err)
	require.Equal(t, CreateLeadBranchDefect, result.Branch)
}

func TestUpsertLeadByEmailCreatesOrUpdatesOneLead(t *testing.T) {
	firstName := "Sample"
	client := testClient(t, func(request *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		require.JSONEq(t, `{"action":"createOrUpdate","lookupField":"email","input":[{"email":"sample@example.com","firstName":"Sample"}]}`, string(body))
		return response(200, `{"success":true,"result":[{"id":318581,"status":"updated","email":"private@example.com"}]}`), nil
	})
	result, err := sdkgo.RunMutation(queryContext{}, client.UpsertLeadByEmail(), testConnection, UpsertLeadByEmailInput{Email: "sample@example.com", FirstName: &firstName})
	require.NoError(t, err)
	require.Equal(t, UpsertLeadByEmailBranchSynced, result.Branch)
	require.Equal(t, SyncedLead{ID: 318581, Status: "updated"}, result.Value)
}

func TestUpsertLeadByEmailClassifiesRecordErrorsAndRetries(t *testing.T) {
	firstName := "Sample"
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{name: "multiple matches", status: 200, body: `{"success":true,"result":[{"status":"skipped","reasons":[{"code":"1007","message":"secret"}]}]}`, branch: UpsertLeadByEmailBranchProviderRejected},
		{name: "malformed", status: 200, body: `{`, branch: UpsertLeadByEmailBranchInvalidResponse},
		{name: "rate limit", status: 429, body: `{}`, retry: true},
		{name: "unavailable", status: 503, body: `{}`, retry: true},
		{name: "unknown", status: 500, body: `{}`, retry: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			client := testClient(t, func(*http.Request) (*http.Response, error) { return response(testCase.status, testCase.body), nil })
			result, err := sdkgo.RunMutation(queryContext{}, client.UpsertLeadByEmail(), testConnection, UpsertLeadByEmailInput{Email: "sample@example.com", FirstName: &firstName})
			if testCase.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
			require.NotContains(t, result.Failure.Message, "secret")
		})
	}
}

func TestLeadWritesRetryDroppedConnections(t *testing.T) {
	firstName := "Sample"
	client := testClient(t, func(*http.Request) (*http.Response, error) { return nil, errors.New("dropped") })
	_, err := sdkgo.RunMutation(queryContext{}, client.CreateLead(), testConnection, CreateLeadInput{Email: "sample@example.com"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	_, err = sdkgo.RunMutation(queryContext{}, client.UpsertLeadByEmail(), testConnection, UpsertLeadByEmailInput{Email: "sample@example.com", FirstName: &firstName})
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, strings.ToLower(err.Error()), "sample@example.com")
}
