// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package notion_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/notion"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func validUpdateInput() notion.UpdatePagePropertiesInput {
	return notion.UpdatePagePropertiesInput{
		PageID: testPageID,
		Properties: map[string]notion.PropertyValue{
			"Status": notion.SelectValue("Reviewed"), "Tags": notion.MultiSelectValue("web"), "Due": notion.ClearedValue(notion.PropertyTypeDate),
		},
	}
}

func TestUpdatePagePropertiesSendsAbsoluteValuesAndReturnsThePage(t *testing.T) {
	provider := newRecordingNotion(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, pageJSON(testPageID, "Ada Lovelace"))
	})
	client := newNotionClient(t, provider.URL)
	result, err := sdkgo.RunMutation(newNotionDexContext("update"), client.UpdatePageProperties(), notionConnection, validUpdateInput())
	require.NoError(t, err)
	require.Equal(t, notion.UpdatePagePropertiesBranchUpdated, result.Branch)
	require.Equal(t, testPageID, result.Value.Page.ID)
	require.Equal(t, testPageID, result.Receipt.ProviderObjectID)
	request := provider.request(0)
	require.Equal(t, http.MethodPatch, request.method)
	require.Equal(t, "/v1/pages/"+testPageID, request.path)
	require.JSONEq(t, `{"properties":{"Status":{"select":{"name":"Reviewed"}},"Tags":{"multi_select":[{"name":"web"}]},"Due":{"date":null}}}`, request.body)
}

// TestUpdatePagePropertiesRetriesEveryAmbiguousOutcome holds because resending absolute values leaves the same state.
func TestUpdatePagePropertiesRetriesEveryAmbiguousOutcome(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		kind   sdkgo.FailureKind
	}{
		{"503 after Notion saved", http.StatusServiceUnavailable, `{"object":"error","status":503,"code":"service_unavailable","message":"SENTINEL","additional_data":{"retry_guidance":["Do not repeat the write."]}}`, sdkgo.FailureAvailability},
		{"gateway timeout", http.StatusGatewayTimeout, notionError(504, "gateway_timeout"), sdkgo.FailureAvailability},
		{"conflict", http.StatusConflict, notionError(409, "conflict_error"), sdkgo.FailureConflict},
		{"rate limited", http.StatusTooManyRequests, notionError(429, "rate_limited"), sdkgo.FailureRateLimit},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingNotion(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, test.status, test.body)
			})
			client := newNotionClient(t, provider.URL)
			_, err := sdkgo.RunMutation(newNotionDexContext("update-"+test.name), client.UpdatePageProperties(), notionConnection, validUpdateInput())
			requireRetry(t, err, test.kind)
		})
	}
	release := make(chan struct{})
	slow := newRecordingNotion(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		<-release
		writeJSON(t, response, http.StatusOK, pageJSON(testPageID, "Ada"))
	})
	t.Cleanup(func() { close(release) })
	client := newNotionClient(t, slow.URL, notion.WithHTTPClient(&http.Client{Timeout: 200 * time.Millisecond}))
	_, err := sdkgo.RunMutation(newNotionDexContext("update-timeout"), client.UpdatePageProperties(), notionConnection, validUpdateInput())
	requireRetry(t, err, sdkgo.FailureTransport)
}

func TestUpdatePagePropertiesMapsConclusiveOutcomes(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
	}{
		{"unshared page", http.StatusNotFound, notionError(404, "object_not_found"), notion.UpdatePagePropertiesBranchNotFound},
		{"wrong property type", http.StatusBadRequest, notionError(400, "validation_error"), notion.UpdatePagePropertiesBranchProviderRejected},
		{"missing capability", http.StatusForbidden, notionError(403, "restricted_resource"), notion.UpdatePagePropertiesBranchProviderRejected},
		{"another page returned", http.StatusOK, pageJSON("6e8a0324-5f70-4192-a3b4-556677889900", "Other"), notion.UpdatePagePropertiesBranchInvalidResponse},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingNotion(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, test.status, test.body)
			})
			client := newNotionClient(t, provider.URL)
			result, err := sdkgo.RunMutation(newNotionDexContext("update-"+test.name), client.UpdatePageProperties(), notionConnection, validUpdateInput())
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			requireNoSentinel(t, result)
		})
	}
}

func TestUpdatePagePropertiesRejectsInvalidInputWithoutARequest(t *testing.T) {
	provider := newRecordingNotion(t, func(http.ResponseWriter, *http.Request, int) {})
	client := newNotionClient(t, provider.URL)
	for name, input := range map[string]notion.UpdatePagePropertiesInput{
		"no page":       {Properties: map[string]notion.PropertyValue{"Status": notion.SelectValue("New")}},
		"no properties": {PageID: testPageID},
		"a read-only":   {PageID: testPageID, Properties: map[string]notion.PropertyValue{"Total": {Type: notion.PropertyTypeFormula}}},
	} {
		result, err := sdkgo.RunMutation(newNotionDexContext("invalid"), client.UpdatePageProperties(), notionConnection, input)
		require.NoError(t, err)
		require.Equal(t, notion.UpdatePagePropertiesBranchDefect, result.Branch, name)
	}
	require.Zero(t, provider.requestCount())
}
