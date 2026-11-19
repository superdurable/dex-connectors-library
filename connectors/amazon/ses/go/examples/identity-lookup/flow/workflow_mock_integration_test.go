//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package identitylookup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/ses/go"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/ses/go/sesmock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestIdentityLookupUsesDefaultMockWithRealDex(t *testing.T) {
	mock := sesmock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection())
	sendFlow := NewSendFlow(mock.Connection())
	createFlow := NewCreateFlow(mock.Connection())
	templatedSendFlow := NewTemplatedSendFlow(mock.Connection())
	customVerificationFlow := NewCustomVerificationFlow(mock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{flow, sendFlow, createFlow, templatedSendFlow, customVerificationFlow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	serverAddress := os.Getenv("DEX_FLOW_SERVICE_ADDRESS")
	if serverAddress == "" {
		serverAddress = "127.0.0.1:8801"
	}
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{FlowServiceAddress: serverAddress})
	require.NoError(t, err)
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: serverAddress})
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(worker.Stop(stopCtx), <-workerResult, client.Close(), cache.Close()))
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	require.NoError(t, worker.WaitPullRegistered(ctx))
	flowID := "ses-identity-lookup-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, flow, flowID, ses.GetEmailIdentityInput{EmailIdentity: "example.com"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var output ses.EmailIdentityDetails
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.Equal(t, "example.com", output.Name)
	require.Len(t, mock.ListEmailIdentities().Calls(), 1)
	require.Equal(t, ses.ListEmailIdentitiesInput{PageSize: 10}, mock.ListEmailIdentities().Calls()[0].Input)
	require.Len(t, mock.GetEmailIdentity().Calls(), 1)
	sendFlowID := "ses-email-send-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, sendFlow, sendFlowID, ses.SendEmailInput{
		FromEmailAddress: "sender@example.com", ToEmailAddress: "reader@example.net", Subject: "Report ready", TextBody: "The report is ready.",
	}, dex.StartFlowOptions{})
	require.NoError(t, err)
	sendResult, err := client.WaitForFlow(ctx, sendFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, sendResult.Status, sendResult.ErrorMessage)
	require.Len(t, mock.SendEmail().Calls(), 1)
	createID := "ses-create-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, createFlow, createID, ses.CreateEmailIdentityInput{EmailIdentity: "example.com"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	createResult, err := client.WaitForFlow(ctx, createID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, createResult.Status, createResult.ErrorMessage)
	var created ses.CreatedEmailIdentity
	require.NoError(t, createResult.DecodeSingleOutput(&created))
	require.Equal(t, "example.com", created.Name)
	require.Len(t, mock.CreateEmailIdentity().Calls(), 1)
	templatedSendID := "ses-template-send-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, templatedSendFlow, templatedSendID, ses.SendTemplatedEmailInput{
		FromEmailAddress: "sender@example.com", ToEmailAddress: "reader@example.net",
		TemplateName: "weekly-report", TemplateData: map[string]string{"name": "Ada"},
	}, dex.StartFlowOptions{})
	require.NoError(t, err)
	templatedResult, err := client.WaitForFlow(ctx, templatedSendID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, templatedResult.Status, templatedResult.ErrorMessage)
	require.Len(t, mock.SendTemplatedEmail().Calls(), 1)
	customSendID := "ses-custom-verification-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, customVerificationFlow, customSendID, ses.SendCustomVerificationEmailInput{
		EmailAddress: "reader@example.net", TemplateName: "verify-customer",
	}, dex.StartFlowOptions{})
	require.NoError(t, err)
	customResult, err := client.WaitForFlow(ctx, customSendID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, customResult.Status, customResult.ErrorMessage)
	require.Len(t, mock.SendCustomVerificationEmail().Calls(), 1)
}
