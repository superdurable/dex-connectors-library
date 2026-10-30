// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mailchimp_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/intuit/mailchimp"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestGetMemberReadsTheContactByTheHashOfItsLowercasedAddress(t *testing.T) {
	provider := newRecordingMailchimp(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, memberBody("urist.mcvankab@example.com", "unsubscribed"))
	})
	result, err := sdkgo.RunQuery(newMailchimpDexContext("get"), newMailchimpClient(t, provider.URL).GetMember(), mailchimpConnection,
		mailchimp.GetMemberInput{ListID: testListID, EmailAddress: testEmailAddress})
	require.NoError(t, err)
	require.Equal(t, mailchimp.GetMemberBranchFound, result.Branch, "an unsubscribed contact is found, not missing")
	request := provider.request(0)
	require.Equal(t, http.MethodGet, request.method)
	require.Equal(t, "/3.0/lists/"+testListID+"/members/"+testSubscriberHash, request.path)
	require.Empty(t, request.body)
	signedUp := time.Date(2026, 1, 12, 9, 0, 0, 0, time.UTC)
	changed := time.Date(2026, 1, 28, 8, 45, 0, 0, time.UTC)
	require.Equal(t, mailchimp.Member{
		SubscriberHash: testSubscriberHash, EmailAddress: "urist.mcvankab@example.com", ContactID: "e4b9b6c1a2", FullName: "Urist McVankab",
		Status: mailchimp.MemberStatusUnsubscribed, EmailType: "html",
		MergeFields: map[string]any{"FNAME": "Urist", "LNAME": "McVankab", "AGE": float64(42)},
		Tags:        []string{"Influencer", "Tech"}, TagCount: 2, Language: "en", ListID: testListID, WebID: 123456,
		SignedUpAt: &signedUp, LastChangedAt: &changed,
	}, result.Value)
	require.Equal(t, testSubscriberHash, result.Receipt.ProviderObjectID)
	require.Equal(t, "a1efb240-f8d8-40fe-a680-c3a5619a42e9", result.Receipt.ProviderRequestID)
	requireNoSecretOrProviderText(t, result)
}

func TestGetMemberRejectsInvalidInputBeforeAnyRequest(t *testing.T) {
	provider := newRecordingMailchimp(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	for name, input := range map[string]mailchimp.GetMemberInput{
		"missing list":    {EmailAddress: testEmailAddress},
		"list path":       {ListID: "57afe96172/../campaigns", EmailAddress: testEmailAddress},
		"display address": {ListID: testListID, EmailAddress: "Urist <urist@example.com>"},
		"padded address":  {ListID: testListID, EmailAddress: " urist@example.com"},
		"two addresses":   {ListID: testListID, EmailAddress: "a@example.com,b@example.com"},
	} {
		result, err := sdkgo.RunQuery(newMailchimpDexContext("invalid-get"), newMailchimpClient(t, provider.URL).GetMember(), mailchimpConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, mailchimp.GetMemberBranchDefect, result.Branch, name)
		require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind, name)
	}
}
