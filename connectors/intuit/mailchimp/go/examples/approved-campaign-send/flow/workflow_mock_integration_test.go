//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package approvedcampaignsend

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/intuit/mailchimp"
	"github.com/superdurable/dex-connectors-library/connectors/intuit/mailchimp/mailchimpmock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestApprovedCampaignSendExampleRunsOnMailchimpMockWithRealDex(t *testing.T) {
	mock := mailchimpmock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection())
	harness := &launchHarness{flow: flow, serverAddress: environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")}
	var err error
	harness.registry, err = dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	harness.cache, err = blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	harness.client, err = dex.NewClient(harness.registry, harness.cache, dex.ClientOptions{
		FlowServiceAddress: harness.serverAddress,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(harness.worker.Stop(ctx), <-harness.workerResult, harness.client.Close(), harness.cache.Close()))
	})
	harness.startWorker(t)

	input := Input{
		ListID: integrationListID, CampaignID: integrationCampaignID, Tag: integrationTag,
		StatusIfNew: mailchimp.MemberStatusPending,
		Contacts:    []ContactInput{{EmailAddress: "ben@example.com", FirstName: "Ben"}},
	}
	flowID := harness.startLaunch(t, "mock-default", input)
	waiting := harness.waitForPhase(t, flowID, PhaseAwaitingApproval)
	require.Equal(t, 3, waiting.SubscribedMemberCount)
	require.Equal(t, []ContactOutcome{{
		EmailAddress: "ben@example.com", SubscriberHash: "2c66f6142933ed5a97948f89cb1c7be0",
		Action: ContactEnrolled, PreviousStatus: mailchimp.MemberStatusSubscribed, Status: mailchimp.MemberStatusSubscribed,
	}}, waiting.Contacts)
	require.Empty(t, mock.SendCampaign().Calls(), "the Flow waits for approval before sending")
	require.NoError(t, harness.client.InvokeRPC(integrationContext(t), flowID, flow.ApproveCampaignSend, approval(), nil))
	record := harness.waitForFlowOutput(t, flowID)
	require.Equal(t, PhaseSent, record.Phase)
	require.Equal(t, &SendDecision{DecidedBy: "Grace Hopper", Note: "Copy and audience checked."}, record.Approval)
	require.Equal(t, 1, record.SendExecutions)

	reads := mock.GetMember().Calls()
	require.Len(t, reads, 1)
	require.Equal(t, flowID, reads[0].FlowID)
	require.Equal(t, mailchimp.GetMemberInput{ListID: integrationListID, EmailAddress: "ben@example.com"}, reads[0].Input)
	upserts := mock.UpsertMember().Calls()
	require.Len(t, upserts, 1)
	require.Equal(t, mailchimp.UpsertMemberInput{
		ListID: integrationListID, EmailAddress: "ben@example.com", StatusIfNew: mailchimp.MemberStatusPending,
		MergeFields: map[string]any{"FNAME": "Ben"},
	}, upserts[0].Input)
	tags := mock.UpdateMemberTags().Calls()
	require.Len(t, tags, 1)
	require.Equal(t, mailchimp.UpdateMemberTagsInput{
		ListID: integrationListID, EmailAddress: "ben@example.com", AddTags: []string{integrationTag},
	}, tags[0].Input)
	lists := mock.ListMembers().Calls()
	require.Len(t, lists, 1)
	require.Equal(t, mailchimp.ListMembersInput{
		ListID: integrationListID, Status: mailchimp.MemberStatusSubscribed, PageSize: 1,
	}, lists[0].Input)
	sends := mock.SendCampaign().Calls()
	require.Len(t, sends, 1)
	require.Equal(t, mailchimp.SendCampaignInput{CampaignID: integrationCampaignID, ExpectedListID: integrationListID}, sends[0].Input)
}
