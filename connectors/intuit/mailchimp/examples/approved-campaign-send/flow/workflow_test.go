// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package approvedcampaignsend

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/intuit/mailchimp"
)

func validLaunchInput() Input {
	return Input{
		ListID: "57afe96172", CampaignID: "42694e9e57", Tag: "spring-launch", StatusIfNew: mailchimp.MemberStatusPending,
		Contacts: []ContactInput{{EmailAddress: "ben@example.com", FirstName: "Ben"}, {EmailAddress: "Dana.Diaz@Example.com"}},
	}
}

func TestBuildLaunchRequestTrimsAndValidatesTheLaunch(t *testing.T) {
	input := validLaunchInput()
	input.ListID, input.Tag = " 57afe96172 ", " spring-launch "
	input.Contacts[0] = ContactInput{EmailAddress: " ben@example.com ", FirstName: " Ben ", LastName: " Ng "}
	request, err := BuildLaunchRequest(input)
	require.NoError(t, err)
	require.Equal(t, LaunchRequest{
		ListID: "57afe96172", CampaignID: "42694e9e57", Tag: "spring-launch", StatusIfNew: mailchimp.MemberStatusPending,
		Contacts: []ContactInput{{EmailAddress: "ben@example.com", FirstName: "Ben", LastName: "Ng"}, {EmailAddress: "Dana.Diaz@Example.com"}},
	}, request)

	for name, change := range map[string]func(*Input){
		"missing audience":     func(input *Input) { input.ListID = "" },
		"audience path":        func(input *Input) { input.ListID = "57afe96172/members" },
		"missing campaign":     func(input *Input) { input.CampaignID = "" },
		"missing tag":          func(input *Input) { input.Tag = " " },
		"two-line tag":         func(input *Input) { input.Tag = "a\nb" },
		"long tag":             func(input *Input) { input.Tag = strings.Repeat("t", 101) },
		"unsubscribed if new":  func(input *Input) { input.StatusIfNew = mailchimp.MemberStatusUnsubscribed },
		"missing status":       func(input *Input) { input.StatusIfNew = "" },
		"no contacts":          func(input *Input) { input.Contacts = nil },
		"too many contacts":    func(input *Input) { input.Contacts = make([]ContactInput, MaxContacts+1) },
		"display address":      func(input *Input) { input.Contacts[0].EmailAddress = "Ben <ben@example.com>" },
		"same address twice":   func(input *Input) { input.Contacts[1].EmailAddress = "BEN@example.com" },
		"two-line first name":  func(input *Input) { input.Contacts[0].FirstName = "Ben\nNg" },
		"comma separated list": func(input *Input) { input.Contacts[0].EmailAddress = "a@example.com,b@example.com" },
	} {
		invalid := validLaunchInput()
		change(&invalid)
		_, err := BuildLaunchRequest(invalid)
		require.Error(t, err, name)
	}
}

func TestMappersNeverChangeAnExistingContactsStatus(t *testing.T) {
	upsert := MapToUpsertMemberInput(ContactUpsert{
		ListID: "57afe96172", Contact: ContactInput{EmailAddress: "ben@example.com", FirstName: "Ben"}, StatusIfNew: mailchimp.MemberStatusPending,
	})
	require.Equal(t, mailchimp.UpsertMemberInput{
		ListID: "57afe96172", EmailAddress: "ben@example.com", StatusIfNew: mailchimp.MemberStatusPending, MergeFields: map[string]any{"FNAME": "Ben"},
	}, upsert)
	require.Empty(t, upsert.Status, "only statusIfNew is set, so Mailchimp keeps an existing contact's status")
	require.False(t, upsert.IsResubscribeAllowed)
	require.Nil(t, MapToUpsertMemberInput(ContactUpsert{ListID: "57afe96172", Contact: ContactInput{EmailAddress: "x@example.com"}}).MergeFields)

	require.Equal(t, mailchimp.UpdateMemberTagsInput{ListID: "57afe96172", EmailAddress: "ben@example.com", AddTags: []string{"spring-launch"}},
		MapToUpdateMemberTagsInput(ContactTagging{ListID: "57afe96172", EmailAddress: "ben@example.com", Tag: "spring-launch"}))
	require.Equal(t, mailchimp.ListMembersInput{ListID: "57afe96172", Status: mailchimp.MemberStatusSubscribed, PageSize: 1},
		MapToListMembersInput(AudienceCount{ListID: "57afe96172"}))
	require.Equal(t, mailchimp.SendCampaignInput{CampaignID: "42694e9e57", ExpectedListID: "57afe96172"},
		MapToSendCampaignInput(CampaignSend{CampaignID: "42694e9e57", ListID: "57afe96172"}), "the approved campaign must still target the audience")
	require.Equal(t, mailchimp.GetMemberInput{ListID: "57afe96172", EmailAddress: "ben@example.com"},
		MapToGetMemberInput(ContactLookup{ListID: "57afe96172", EmailAddress: "ben@example.com"}))
}

func TestSuppressedStatusesAndSendDecisions(t *testing.T) {
	for _, status := range []mailchimp.MemberStatus{mailchimp.MemberStatusUnsubscribed, mailchimp.MemberStatusCleaned, mailchimp.MemberStatusArchived} {
		require.True(t, IsSuppressedStatus(status), status)
	}
	for _, status := range []mailchimp.MemberStatus{"", mailchimp.MemberStatusSubscribed, mailchimp.MemberStatusPending, mailchimp.MemberStatusTransactional} {
		require.False(t, IsSuppressedStatus(status), status)
	}
	note := " Checked the copy. "
	decision, err := BuildSendDecision(SendDecisionInput{DecidedBy: " Grace Hopper ", Note: &note})
	require.NoError(t, err)
	require.Equal(t, SendDecision{DecidedBy: "Grace Hopper", Note: "Checked the copy."}, decision)
	for _, input := range []SendDecisionInput{{}, {DecidedBy: " "}, {DecidedBy: "Grace\nHopper"}} {
		_, err := BuildSendDecision(input)
		require.ErrorIs(t, err, errPersonRequired)
	}
}
