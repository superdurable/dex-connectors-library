// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mailchimp_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/intuit/mailchimp"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestUpdateMemberTagsDeclaresEachTagActiveOrInactive(t *testing.T) {
	provider := newRecordingMailchimp(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		response.WriteHeader(http.StatusNoContent)
	})
	result, err := sdkgo.RunMutation(newMailchimpDexContext("tags"), newMailchimpClient(t, provider.URL).UpdateMemberTags(), mailchimpConnection, mailchimp.UpdateMemberTagsInput{
		ListID: testListID, EmailAddress: testEmailAddress, AddTags: []string{"Influencer", "Spring launch"}, RemoveTags: []string{"Lapsed"},
		ShouldSuppressAutomations: true,
	})
	require.NoError(t, err)
	require.Equal(t, mailchimp.UpdateMemberTagsBranchUpdated, result.Branch)
	require.Equal(t, mailchimp.UpdateMemberTagsOutput{
		SubscriberHash: testSubscriberHash, ActiveTags: []string{"Influencer", "Spring launch"}, InactiveTags: []string{"Lapsed"},
	}, result.Value)
	request := provider.request(0)
	require.Equal(t, http.MethodPost, request.method)
	require.Equal(t, "/3.0/lists/"+testListID+"/members/"+testSubscriberHash+"/tags", request.path)
	require.JSONEq(t, `{"tags":[{"name":"Influencer","status":"active"},{"name":"Spring launch","status":"active"},{"name":"Lapsed","status":"inactive"}],"is_syncing":true}`, request.body)
}

func TestUpdateMemberTagsRetriesUnconfirmedOutcomesAndSelectsNotFound(t *testing.T) {
	lost := newRecordingMailchimp(t, func(response http.ResponseWriter, _ *http.Request, _ int) { dropConnection(t, response) })
	input := mailchimp.UpdateMemberTagsInput{ListID: testListID, EmailAddress: testEmailAddress, AddTags: []string{"Influencer"}}
	_, err := sdkgo.RunMutation(newMailchimpDexContext("tags-lost"), newMailchimpClient(t, lost.URL).UpdateMemberTags(), mailchimpConnection, input)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry, "a repeated declaration leaves the same tags, so a lost response is retried")
	require.JSONEq(t, `{"tags":[{"name":"Influencer","status":"active"}]}`, lost.request(0).body, "is_syncing is omitted unless asked")

	missing := newRecordingMailchimp(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeProblem(t, response, http.StatusNotFound, "Resource Not Found")
	})
	result, err := sdkgo.RunMutation(newMailchimpDexContext("tags-missing"), newMailchimpClient(t, missing.URL).UpdateMemberTags(), mailchimpConnection, input)
	require.NoError(t, err)
	require.Equal(t, mailchimp.UpdateMemberTagsBranchNotFound, result.Branch)

	reflected := newRecordingMailchimp(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"echo":"`+testAPIKey+`"}`)
	})
	result, err = sdkgo.RunMutation(newMailchimpDexContext("tags-reflected"), newMailchimpClient(t, reflected.URL).UpdateMemberTags(), mailchimpConnection, input)
	require.NoError(t, err)
	require.Equal(t, mailchimp.UpdateMemberTagsBranchInvalidResponse, result.Branch)
	requireNoSecretOrProviderText(t, result)
}

func TestUpdateMemberTagsRejectsInvalidInputBeforeAnyRequest(t *testing.T) {
	provider := newRecordingMailchimp(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	for name, input := range map[string]mailchimp.UpdateMemberTagsInput{
		"no tags":            {ListID: testListID, EmailAddress: testEmailAddress},
		"add and remove":     {ListID: testListID, EmailAddress: testEmailAddress, AddTags: []string{"VIP"}, RemoveTags: []string{"vip"}},
		"repeated tag":       {ListID: testListID, EmailAddress: testEmailAddress, AddTags: []string{"VIP", "VIP"}},
		"padded tag":         {ListID: testListID, EmailAddress: testEmailAddress, AddTags: []string{" VIP"}},
		"oversized tag":      {ListID: testListID, EmailAddress: testEmailAddress, AddTags: []string{strings.Repeat("a", 101)}},
		"control character":  {ListID: testListID, EmailAddress: testEmailAddress, AddTags: []string{"VIP\n"}},
		"too many tags":      {ListID: testListID, EmailAddress: testEmailAddress, AddTags: strings.Split(strings.Repeat("t,", 50)+"u", ",")},
		"missing audience":   {EmailAddress: testEmailAddress, AddTags: []string{"VIP"}},
		"invalid email form": {ListID: testListID, EmailAddress: "not-an-address", AddTags: []string{"VIP"}},
	} {
		result, err := sdkgo.RunMutation(newMailchimpDexContext("invalid-tags"), newMailchimpClient(t, provider.URL).UpdateMemberTags(), mailchimpConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, mailchimp.UpdateMemberTagsBranchDefect, result.Branch, name)
	}
}
