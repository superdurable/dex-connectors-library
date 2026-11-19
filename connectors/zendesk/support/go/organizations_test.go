// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package support_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zendesk/support/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestOrganizationQueriesReadProviderResources(t *testing.T) {
	provider := newRecordingZendesk(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		switch request.URL.Path {
		case "/api/v2/organizations":
			writeJSON(t, response, http.StatusOK, `{"organizations":[{"id":1201,"name":"Acme","external_id":"acme-1","notes":"SENTINEL"}],"meta":{"has_more":true,"after_cursor":"next"}}`)
		case "/api/v2/organizations/count":
			writeJSON(t, response, http.StatusOK, `{"count":{"value":2,"refreshed_at":"2026-01-01T00:00:00Z"}}`)
		case "/api/v2/organizations/1201":
			writeJSON(t, response, http.StatusOK, `{"organization":{"id":1201,"name":"Acme","external_id":"acme-1"}}`)
		case "/api/v2/organizations/1201/related":
			writeJSON(t, response, http.StatusOK, `{"organization_related":{"tickets_count":12,"users_count":4}}`)
		case "/api/v2/users/7701/organizations":
			writeJSON(t, response, http.StatusOK, `{"organizations":[{"id":1201,"name":"Acme","external_id":"acme-1"}],"meta":{"has_more":false}}`)
		case "/api/v2/users/7701/related":
			writeJSON(t, response, http.StatusOK, `{"user_related":{"assigned_tickets":5,"requested_tickets":10,"ccd_tickets":3,"organization_subscriptions":1}}`)
		default:
			t.Fatalf("unexpected path %s", request.URL.Path)
		}
	})
	client := newZendeskClient(t, provider.URL)
	listed, err := sdkgo.RunQuery(newZendeskDexContext("list-organizations"), client.ListOrganizations(), zendeskConnection, support.ListOrganizationsInput{PageSize: 2})
	require.NoError(t, err)
	require.Equal(t, "next", listed.Value.NextCursor)
	encoded, err := json.Marshal(listed.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "SENTINEL")
	counted, err := sdkgo.RunQuery(newZendeskDexContext("count-organizations"), client.CountOrganizations(), zendeskConnection, support.CountOrganizationsInput{})
	require.NoError(t, err)
	require.Equal(t, int64(2), counted.Value.Count)
	found, err := sdkgo.RunQuery(newZendeskDexContext("get-organization"), client.GetOrganization(), zendeskConnection, support.GetOrganizationInput{OrganizationID: 1201})
	require.NoError(t, err)
	require.Equal(t, "Acme", found.Value.Organization.Name)
	related, err := sdkgo.RunQuery(newZendeskDexContext("organization-related"), client.GetOrganizationRelated(), zendeskConnection, support.GetOrganizationRelatedInput{OrganizationID: 1201})
	require.NoError(t, err)
	require.Equal(t, int64(12), related.Value.TicketsCount)
	userOrganizations, err := sdkgo.RunQuery(newZendeskDexContext("user-organizations"), client.ListUserOrganizations(), zendeskConnection, support.ListUserOrganizationsInput{UserID: 7701, PageSize: 2})
	require.NoError(t, err)
	require.Len(t, userOrganizations.Value.Organizations, 1)
	userRelated, err := sdkgo.RunQuery(newZendeskDexContext("user-related"), client.GetUserRelated(), zendeskConnection, support.GetUserRelatedInput{UserID: 7701})
	require.NoError(t, err)
	require.Equal(t, int64(10), userRelated.Value.RequestedTickets)
}

func TestOrganizationWritesClassifyProviderResponses(t *testing.T) {
	provider := newRecordingZendesk(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		switch request.Method {
		case http.MethodPost:
			require.Equal(t, "/api/v2/organizations", request.URL.Path)
			writeJSON(t, response, http.StatusCreated, `{"organization":{"id":1201,"name":"Acme","external_id":"acme-1"}}`)
		case http.MethodPut:
			require.Equal(t, "/api/v2/organizations/1201", request.URL.Path)
			writeJSON(t, response, http.StatusOK, `{"organization":{"id":1201,"name":"Acme","external_id":"acme-1"}}`)
		case http.MethodDelete:
			require.Equal(t, "/api/v2/organizations/1201", request.URL.Path)
			response.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected method %s", request.Method)
		}
	})
	client := newZendeskClient(t, provider.URL)
	created, err := sdkgo.RunMutation(newZendeskDexContext("create-organization"), client.CreateOrganization(), zendeskConnection, support.CreateOrganizationInput{Name: "Acme", ExternalID: "acme-1"})
	require.NoError(t, err)
	require.Equal(t, support.CreateOrganizationBranchCreated, created.Branch)
	name := "Acme"
	updated, err := sdkgo.RunMutation(newZendeskDexContext("update-organization"), client.UpdateOrganization(), zendeskConnection, support.UpdateOrganizationInput{OrganizationID: 1201, Name: &name})
	require.NoError(t, err)
	require.Equal(t, support.UpdateOrganizationBranchUpdated, updated.Branch)
	deleted, err := sdkgo.RunMutation(newZendeskDexContext("delete-organization"), client.DeleteOrganization(), zendeskConnection, support.DeleteOrganizationInput{OrganizationID: 1201})
	require.NoError(t, err)
	require.Equal(t, support.DeleteOrganizationBranchDeleted, deleted.Branch)
}

func TestCreateOrganizationDerivesStableExternalIDWhenBlank(t *testing.T) {
	var provider *recordingZendesk
	provider = newRecordingZendesk(t, func(response http.ResponseWriter, request *http.Request, index int) {
		var envelope struct {
			Organization struct {
				Name       string `json:"name"`
				ExternalID string `json:"external_id"`
			} `json:"organization"`
		}
		require.NoError(t, json.Unmarshal([]byte(provider.request(index).body), &envelope))
		require.True(t, strings.HasPrefix(envelope.Organization.ExternalID, "dex-"))
		require.NotEqual(t, "dex-", envelope.Organization.ExternalID)
		writeJSON(t, response, http.StatusCreated, `{"organization":{"id":1201,"name":"Acme","external_id":"`+envelope.Organization.ExternalID+`"}}`)
	})
	result, err := sdkgo.RunMutation(newZendeskDexContext("create-organization-derived-id"), newZendeskClient(t, provider.URL).CreateOrganization(), zendeskConnection, support.CreateOrganizationInput{Name: "Acme"})
	require.NoError(t, err)
	require.Equal(t, support.CreateOrganizationBranchCreated, result.Branch)
	require.True(t, strings.HasPrefix(result.Value.Organization.ExternalID, "dex-"))
}
