// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mailchimp_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/intuit/mailchimp"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type completeTarget[IN any] struct {
	dex.StepDefaultsNoWaitFor[IN]
}

func (completeTarget[IN]) Execute(dex.Context, IN) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

func TestFactoriesRequireOnlyTheHappyPathBranch(t *testing.T) {
	connection := newTestConnection(t)
	annotations := sdkgo.StepAnnotations{GroupID: "mailchimp", GroupLabel: "Mailchimp", Explanation: "Call Mailchimp."}
	require.NotPanics(t, func() {
		mailchimp.NewGetMemberStep(mailchimp.GetMemberStepConfig[string]{
			StepType: "ReadContact", Annotations: annotations, Connection: connection, ConnectionName: mailchimpConnection.Name,
			MapToOperationInput: func(email string) mailchimp.GetMemberInput {
				return mailchimp.GetMemberInput{ListID: testListID, EmailAddress: email}
			},
			Found: sdkgo.GoTo(completeTarget[mailchimp.GetMemberResult]{}),
		})
		mailchimp.NewListMembersStep(mailchimp.ListMembersStepConfig[string]{
			StepType: "ListContacts", Annotations: annotations, Connection: connection,
			MapToOperationInput: func(listID string) mailchimp.ListMembersInput { return mailchimp.ListMembersInput{ListID: listID} },
			Listed:              sdkgo.GoTo(completeTarget[mailchimp.ListMembersResult]{}),
		})
		mailchimp.NewUpsertMemberStep(mailchimp.UpsertMemberStepConfig[string]{
			StepType: "UpsertContact", Annotations: annotations, Connection: connection,
			MapToOperationInput: func(email string) mailchimp.UpsertMemberInput {
				return mailchimp.UpsertMemberInput{ListID: testListID, EmailAddress: email, StatusIfNew: mailchimp.MemberStatusPending}
			},
			Upserted: sdkgo.GoTo(completeTarget[mailchimp.UpsertMemberResult]{}),
		})
		mailchimp.NewUpdateMemberTagsStep(mailchimp.UpdateMemberTagsStepConfig[string]{
			StepType: "TagContact", Annotations: annotations, Connection: connection,
			MapToOperationInput: func(email string) mailchimp.UpdateMemberTagsInput {
				return mailchimp.UpdateMemberTagsInput{ListID: testListID, EmailAddress: email, AddTags: []string{"VIP"}}
			},
			Updated: sdkgo.GoTo(completeTarget[mailchimp.UpdateMemberTagsResult]{}),
		})
		mailchimp.NewSendCampaignStep(mailchimp.SendCampaignStepConfig[string]{
			StepType: "SendCampaign", Annotations: annotations, Connection: connection,
			MapToOperationInput: func(campaignID string) mailchimp.SendCampaignInput {
				return mailchimp.SendCampaignInput{CampaignID: campaignID}
			},
			Sent: sdkgo.GoTo(completeTarget[mailchimp.SendCampaignResult]{}),
		})
	})
	require.Panics(t, func() {
		mailchimp.NewSendCampaignStep(mailchimp.SendCampaignStepConfig[string]{
			StepType: "SendCampaign", Connection: connection,
			MapToOperationInput: func(campaignID string) mailchimp.SendCampaignInput {
				return mailchimp.SendCampaignInput{CampaignID: campaignID}
			},
			Uncertain: sdkgo.GoTo(completeTarget[mailchimp.SendCampaignResult]{}),
		})
	}, "sent is the required branch")
	require.Panics(t, func() {
		mailchimp.NewGetMemberStep(mailchimp.GetMemberStepConfig[string]{
			StepType: "ReadContact", Connection: connection, ConnectionName: "another-connection",
			MapToOperationInput: func(email string) mailchimp.GetMemberInput {
				return mailchimp.GetMemberInput{ListID: testListID, EmailAddress: email}
			},
			Found: sdkgo.GoTo(completeTarget[mailchimp.GetMemberResult]{}),
		})
	}, "the static ConnectionName must match the runtime connection")
}

func TestDefinitionsDeclareOptionalBranchesAndDurability(t *testing.T) {
	require.Equal(t, map[sdkgo.BranchID]bool{"found": false, "notFound": true, "providerRejected": true, "invalidResponse": true, "defect": true},
		branchOptionality(mailchimp.GetMemberDefinition.Branches))
	require.Equal(t, map[sdkgo.BranchID]bool{"listed": false, "notFound": true, "providerRejected": true, "invalidResponse": true, "defect": true},
		branchOptionality(mailchimp.ListMembersDefinition.Branches))
	require.Equal(t, map[sdkgo.BranchID]bool{"upserted": false, "providerRejected": true, "invalidResponse": true, "defect": true},
		branchOptionality(mailchimp.UpsertMemberDefinition.Branches), "a PUT keyed by the address is safe to repeat, so it has no uncertain branch")
	require.Equal(t, map[sdkgo.BranchID]bool{"updated": false, "notFound": true, "providerRejected": true, "invalidResponse": true, "defect": true},
		branchOptionality(mailchimp.UpdateMemberTagsDefinition.Branches))
	require.Equal(t, map[sdkgo.BranchID]bool{
		"sent": false, "alreadySent": true, "notSendable": true, "notFound": true, "providerRejected": true, "invalidResponse": true, "uncertain": true, "defect": true,
	}, branchOptionality(mailchimp.SendCampaignDefinition.Branches), "a send has no idempotency key, so an unconfirmed send is uncertain")
	for _, defaults := range []sdkgo.StepDefaults{
		mailchimp.GetMemberDefinition.StepDefaults, mailchimp.ListMembersDefinition.StepDefaults,
		mailchimp.UpsertMemberDefinition.StepDefaults, mailchimp.UpdateMemberTagsDefinition.StepDefaults,
	} {
		require.Equal(t, dex.StepDurabilityAsync, defaults.ExecuteDurability, "a duplicate dispatch of a read or an absolute write is harmless")
		require.Equal(t, 30*time.Second, defaults.ExecuteMethodTimeout, "one 25-second request fits inside it")
		require.GreaterOrEqual(t, defaults.ExecuteRetry.TotalDuration, 2*time.Minute)
	}
	send := mailchimp.SendCampaignDefinition.StepDefaults
	require.Equal(t, dex.StepDurabilitySync, send.ExecuteDurability, "async would dispatch a second send after seven seconds")
	require.Equal(t, 60*time.Second, send.ExecuteMethodTimeout, "the read and the send, 25 seconds each, fit inside it")
	require.GreaterOrEqual(t, send.ExecuteRetry.TotalDuration, 2*time.Minute)
}

func TestConnectionAndCredentialsNeverRevealSecrets(t *testing.T) {
	connection := newTestConnection(t)
	_, err := json.Marshal(connection)
	require.Error(t, err)
	require.NotContains(t, fmt.Sprintf("%v %#v", connection, connection), testAPIKey)
	credentials := mailchimp.Credentials{APIKey: sdkgo.NewSecretString(testAPIKey)}
	rendered := fmt.Sprintf("%v %+v %#v", credentials, credentials, credentials)
	require.NotContains(t, rendered, testAPIKey)
	_, err = json.Marshal(credentials)
	require.Error(t, err)
}

func TestStatusHelpersMatchMailchimpsDocumentedStatuses(t *testing.T) {
	for _, status := range []mailchimp.MemberStatus{"subscribed", "unsubscribed", "pending", "transactional"} {
		require.True(t, mailchimp.IsWritableMemberStatus(status), status)
		require.True(t, mailchimp.IsReadableMemberStatus(status), status)
	}
	for _, status := range []mailchimp.MemberStatus{"cleaned", "archived"} {
		require.False(t, mailchimp.IsWritableMemberStatus(status), status)
		require.True(t, mailchimp.IsReadableMemberStatus(status), status)
	}
	require.False(t, mailchimp.IsReadableMemberStatus("deleted"))
	require.True(t, mailchimp.CampaignStatusSending.IsSendingOrSent())
	require.True(t, mailchimp.CampaignStatusSent.IsSendingOrSent())
	require.False(t, mailchimp.CampaignStatusSave.IsSendingOrSent())
}

func branchOptionality(definitions []sdkgo.BranchDefinition) map[sdkgo.BranchID]bool {
	branches := map[sdkgo.BranchID]bool{}
	for _, branch := range definitions {
		branches[branch.ID] = branch.Optional
	}
	return branches
}

func newTestConnection(t *testing.T) mailchimp.Connection {
	t.Helper()
	client, err := mailchimp.New(mailchimp.Config{}, testCredentialProvider(testAPIKey))
	require.NoError(t, err)
	connection, err := mailchimp.NewConnection(client, mailchimpConnection)
	require.NoError(t, err)
	return connection
}
