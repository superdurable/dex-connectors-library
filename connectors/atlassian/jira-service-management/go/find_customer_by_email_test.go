// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jiraservicemanagement_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	jiraservicemanagement "github.com/superdurable/dex-connectors-library/connectors/atlassian/jira-service-management"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestFindCustomerByEmailKeepsOnlyExactAddressMatches(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"start":0,"limit":50,"size":3,"isLastPage":true,"values":[`+
			`{"accountId":"`+testCustomerAccount+`","displayName":"Jane Smith","emailAddress":"Jane@Acme.example.com","active":true},`+
			`{"accountId":"qm:other","displayName":"Jane Smithers","emailAddress":"jane.smithers@acme.example.com","active":true},`+
			`{"accountId":"qm:hidden","displayName":"Jane S.","emailAddress":null}]}`)
	})
	result, err := sdkgo.RunQuery(newTestDexContext("find"), newTestClient(t, provider.URL).FindCustomerByEmail(), jsmConnection,
		jiraservicemanagement.FindCustomerByEmailInput{ServiceDeskID: "10", Email: "jane@acme.example.com"})
	require.NoError(t, err)
	require.Equal(t, jiraservicemanagement.FindCustomerByEmailBranchFound, result.Branch)
	require.Equal(t, []jiraservicemanagement.Customer{{AccountID: testCustomerAccount, DisplayName: "Jane Smith", IsActive: true}}, result.Value.Customers)
	require.False(t, result.Value.HasMore)
	request := provider.request(0)
	require.Equal(t, testServiceDeskPath+"/servicedesk/10/customer", request.path)
	require.Equal(t, "limit=50&query=jane%40acme.example.com&start=0", request.rawQuery)
	require.Equal(t, "opt-in", request.experimental, "the customer list is an experimental endpoint")
	encodedCustomers := result.Value.Customers
	require.NotContains(t, encodedCustomers[0].DisplayName, "@", "customers carry no email address")
}

func TestFindCustomerByEmailSelectsNotFoundAndRejectsAnUnknownDesk(t *testing.T) {
	empty := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"start":0,"limit":50,"size":0,"isLastPage":false,"values":[]}`)
	})
	result, err := sdkgo.RunQuery(newTestDexContext("find-none"), newTestClient(t, empty.URL).FindCustomerByEmail(), jsmConnection,
		jiraservicemanagement.FindCustomerByEmailInput{ServiceDeskID: "10", Email: "ben@example.com"})
	require.NoError(t, err)
	require.Equal(t, jiraservicemanagement.FindCustomerByEmailBranchNotFound, result.Branch)
	require.Empty(t, result.Value.Customers)
	require.True(t, result.Value.HasMore)

	unknownDesk := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusNotFound, `{"errorMessage":"SENTINEL Service Desk does not exist"}`)
	})
	result, err = sdkgo.RunQuery(newTestDexContext("find-desk"), newTestClient(t, unknownDesk.URL).FindCustomerByEmail(), jsmConnection,
		jiraservicemanagement.FindCustomerByEmailInput{ServiceDeskID: "99", Email: "ben@example.com"})
	require.NoError(t, err)
	require.Equal(t, jiraservicemanagement.FindCustomerByEmailBranchProviderRejected, result.Branch, "a missing desk is not a missing customer")
	requireNoSentinel(t, result)

	for _, input := range []jiraservicemanagement.FindCustomerByEmailInput{
		{ServiceDeskID: "10", Email: "Jane <jane@example.com>"},
		{ServiceDeskID: "IT", Email: "jane@example.com"},
	} {
		result, err = sdkgo.RunQuery(newTestDexContext("find-invalid"), newTestClient(t, empty.URL).FindCustomerByEmail(), jsmConnection, input)
		require.NoError(t, err)
		require.Equal(t, jiraservicemanagement.FindCustomerByEmailBranchDefect, result.Branch)
	}
	require.Equal(t, 1, empty.requestCount())
}
