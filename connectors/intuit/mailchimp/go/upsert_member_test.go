// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mailchimp_test

import (
	"math"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/intuit/mailchimp"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func validUpsertMemberInput() mailchimp.UpsertMemberInput {
	return mailchimp.UpsertMemberInput{
		ListID: testListID, EmailAddress: testEmailAddress, StatusIfNew: mailchimp.MemberStatusPending,
		MergeFields: map[string]any{
			"FNAME": "Urist", "AGE": 42.0,
			"ADDRESS": map[string]any{"addr1": "123 Freddie Ave", "city": "Atlanta", "state": "GA", "zip": "12345"},
		},
		MarketingPermissions: []mailchimp.MarketingPermissionInput{{MarketingPermissionID: "e8e4f1d6b2", IsEnabled: true}},
	}
}

func TestUpsertMemberPutsStatusIfNewOnlySoAnExistingContactKeepsItsStatus(t *testing.T) {
	provider := newRecordingMailchimp(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, memberBody("urist.mcvankab@example.com", "unsubscribed"))
	})
	ctx := newMailchimpDexContext("upsert")
	result, err := sdkgo.RunMutation(ctx, newMailchimpClient(t, provider.URL).UpsertMember(), mailchimpConnection, validUpsertMemberInput())
	require.NoError(t, err)
	require.Equal(t, mailchimp.UpsertMemberBranchUpserted, result.Branch)
	require.Equal(t, mailchimp.MemberStatusUnsubscribed, result.Value.Member.Status, "Mailchimp kept the existing contact unsubscribed")
	require.Equal(t, testSubscriberHash, result.Receipt.ProviderObjectID)
	require.Equal(t, 1, provider.requestCount())
	request := provider.request(0)
	require.Equal(t, http.MethodPut, request.method)
	require.Equal(t, "/3.0/lists/"+testListID+"/members/"+testSubscriberHash, request.path)
	require.Empty(t, request.query, "merge validation stays on unless asked")
	require.Equal(t, "application/json", request.header.Get("Content-Type"))
	require.Empty(t, request.header.Get("Idempotency-Key"), "Mailchimp documents no key")
	require.JSONEq(t, `{"email_address":"Urist.McVankab@example.com","status_if_new":"pending",
		"merge_fields":{"FNAME":"Urist","AGE":42,"ADDRESS":{"addr1":"123 Freddie Ave","city":"Atlanta","state":"GA","zip":"12345"}},
		"marketing_permissions":[{"marketing_permission_id":"e8e4f1d6b2","enabled":true}]}`, request.body)
	require.NotContains(t, request.body, `"status":`, "without Status the PUT never changes an existing contact's status")
	require.Zero(t, ctx.heartbeatCount, "a repeatable write needs no dispatch checkpoint")
}

func TestUpsertMemberChangesAnExistingStatusOnlyWhenAskedAndResubscribesOnlyWithPermission(t *testing.T) {
	provider := newRecordingMailchimp(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, memberBody(testEmailAddress, "unsubscribed"))
	})
	client := newMailchimpClient(t, provider.URL)

	optOut := validUpsertMemberInput()
	optOut.Status, optOut.MergeFields, optOut.MarketingPermissions, optOut.ShouldSkipMergeValidation = mailchimp.MemberStatusUnsubscribed, nil, nil, true
	_, err := sdkgo.RunMutation(newMailchimpDexContext("opt-out"), client.UpsertMember(), mailchimpConnection, optOut)
	require.NoError(t, err)
	require.JSONEq(t, `{"email_address":"Urist.McVankab@example.com","status_if_new":"pending","status":"unsubscribed"}`, provider.request(0).body,
		"an opt-out needs no permission")
	require.Equal(t, []string{"true"}, provider.request(0).query["skip_merge_validation"])

	for _, status := range []mailchimp.MemberStatus{mailchimp.MemberStatusSubscribed, mailchimp.MemberStatusPending} {
		resubscribe := validUpsertMemberInput()
		resubscribe.Status = status
		result, err := sdkgo.RunMutation(newMailchimpDexContext("resubscribe-refused"), client.UpsertMember(), mailchimpConnection, resubscribe)
		require.NoError(t, err)
		require.Equal(t, mailchimp.UpsertMemberBranchDefect, result.Branch, status)
		require.Contains(t, result.Failure.Message, "isResubscribeAllowed")
	}
	require.Equal(t, 1, provider.requestCount(), "a refused resubscribe sends nothing")

	allowed := validUpsertMemberInput()
	allowed.Status, allowed.IsResubscribeAllowed = mailchimp.MemberStatusPending, true
	_, err = sdkgo.RunMutation(newMailchimpDexContext("resubscribe-allowed"), client.UpsertMember(), mailchimpConnection, allowed)
	require.NoError(t, err)
	require.Contains(t, provider.request(1).body, `"status":"pending"`)
}

func TestUpsertMemberRetriesEveryUnconfirmedOutcomeBecauseThePutRepeats(t *testing.T) {
	for name, reply := range map[string]func(http.ResponseWriter){
		"server error": func(response http.ResponseWriter) {
			writeProblem(t, response, http.StatusInternalServerError, "Internal Server Error")
		},
		"lost response": func(response http.ResponseWriter) { dropConnection(t, response) },
		"too many": func(response http.ResponseWriter) {
			writeProblem(t, response, http.StatusTooManyRequests, "Too Many Requests")
		},
	} {
		provider := newRecordingMailchimp(t, func(response http.ResponseWriter, _ *http.Request, _ int) { reply(response) })
		_, err := sdkgo.RunMutation(newMailchimpDexContext("upsert-"+name), newMailchimpClient(t, provider.URL).UpsertMember(), mailchimpConnection, validUpsertMemberInput())
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry, name)
	}
}

func TestUpsertMemberSelectsProviderRejectedAndInvalidResponseWithoutMailchimpText(t *testing.T) {
	for _, test := range []struct {
		name    string
		status  int
		title   string
		body    string
		branch  sdkgo.BranchID
		message string
	}{
		{name: "compliance state", status: http.StatusBadRequest, title: "Member In Compliance State", branch: mailchimp.UpsertMemberBranchProviderRejected,
			message: "Mailchimp rejected the request (HTTP 400) [Member In Compliance State]"},
		{name: "permanently deleted", status: http.StatusBadRequest, title: "Forgotten Email Not Subscribed", branch: mailchimp.UpsertMemberBranchProviderRejected,
			message: "Mailchimp rejected the request (HTTP 400) [Forgotten Email Not Subscribed]"},
		{name: "missing audience", status: http.StatusNotFound, title: "Resource Not Found", branch: mailchimp.UpsertMemberBranchProviderRejected,
			message: "Mailchimp found no such resource (HTTP 404) [Resource Not Found]"},
		{name: "invalid contact", status: http.StatusOK, body: `{"status":"subscribed"}`, branch: mailchimp.UpsertMemberBranchInvalidResponse,
			message: "Mailchimp accepted the contact but returned an invalid contact: the contact has no subscriber hash"},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingMailchimp(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				if test.title != "" {
					writeProblem(t, response, test.status, test.title)
					return
				}
				writeJSON(t, response, test.status, test.body)
			})
			result, err := sdkgo.RunMutation(newMailchimpDexContext("upsert-"+test.name), newMailchimpClient(t, provider.URL).UpsertMember(), mailchimpConnection, validUpsertMemberInput())
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.Equal(t, test.message, result.Failure.Message)
			requireNoSecretOrProviderText(t, result)
		})
	}
}

func TestUpsertMemberRejectsInvalidInputBeforeAnyRequest(t *testing.T) {
	provider := newRecordingMailchimp(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	for name, change := range map[string]func(*mailchimp.UpsertMemberInput){
		"missing statusIfNew": func(input *mailchimp.UpsertMemberInput) { input.StatusIfNew = "" },
		"cleaned statusIfNew": func(input *mailchimp.UpsertMemberInput) { input.StatusIfNew = mailchimp.MemberStatusCleaned },
		"archived status":     func(input *mailchimp.UpsertMemberInput) { input.Status = mailchimp.MemberStatusArchived },
		"lowercase merge tag": func(input *mailchimp.UpsertMemberInput) { input.MergeFields = map[string]any{"fname": "Urist"} },
		"boolean merge value": func(input *mailchimp.UpsertMemberInput) { input.MergeFields = map[string]any{"VIP": true} },
		"infinite number":     func(input *mailchimp.UpsertMemberInput) { input.MergeFields = map[string]any{"AGE": math.Inf(1)} },
		"oversized text": func(input *mailchimp.UpsertMemberInput) {
			input.MergeFields = map[string]any{"NOTE": strings.Repeat("a", 1025)}
		},
		"control text": func(input *mailchimp.UpsertMemberInput) { input.MergeFields = map[string]any{"NOTE": "a\x00b"} },
		"nested address number": func(input *mailchimp.UpsertMemberInput) {
			input.MergeFields = map[string]any{"ADDRESS": map[string]any{"zip": 12345}}
		},
		"repeated permission": func(input *mailchimp.UpsertMemberInput) {
			input.MarketingPermissions = append(input.MarketingPermissions, input.MarketingPermissions[0])
		},
		"invalid permission ID": func(input *mailchimp.UpsertMemberInput) {
			input.MarketingPermissions = []mailchimp.MarketingPermissionInput{{MarketingPermissionID: "../x"}}
		},
	} {
		input := validUpsertMemberInput()
		change(&input)
		ctx := newMailchimpDexContext("invalid-upsert")
		result, err := sdkgo.RunMutation(ctx, newMailchimpClient(t, provider.URL).UpsertMember(), mailchimpConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, mailchimp.UpsertMemberBranchDefect, result.Branch, name)
		require.Zero(t, ctx.heartbeatCount, name)
	}
}
