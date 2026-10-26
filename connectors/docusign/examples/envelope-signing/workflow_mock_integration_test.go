//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/docusign"
	"github.com/superdurable/dex-connectors-library/connectors/docusign/docusignmock"
	envelopesigning "github.com/superdurable/dex-connectors-library/connectors/docusign/examples/envelope-signing/flow"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestEnvelopeSigningPollsMockAndCompletesWithRealDex(t *testing.T) {
	mock := docusignmock.New(t, envelopesigning.ConnectionName)
	harness := startSigningHarnessWithConnection(t, mock.Connection(), envelopesigning.SigningPolicy{
		StatusPollInterval: time.Second, SigningDeadline: time.Minute, VoidReason: "The signing deadline passed.",
	})
	requestID := uniqueRequestID("mock")
	request := envelopesigning.SigningRequest{
		RequestID: requestID, TemplateID: exampleTemplateID, EmailSubject: "Meridian Corp: MSA",
		Signers: []docusign.TemplateRole{{RoleName: "Customer", Name: "Priya Raman", Email: "priya@meridian.example.com"}},
	}
	flowID := envelopesigning.FlowIDForRequest(requestID)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	_, err := harness.client.StartFlow(ctx, harness.flow, flowID, request, dex.StartFlowOptions{RequestID: &flowID})
	require.NoError(t, err)
	outcome := harness.waitForOutcome(t, requestID)
	require.Equal(t, envelopesigning.StatusCompleted, outcome.Status)
	require.Equal(t, "statusPoll", outcome.ResumedBy)
	require.Equal(t, 1, outcome.StatusPollCount)
	require.Equal(t, request, outcome.Request)
	require.NotNil(t, outcome.Document)
	require.Equal(t, outcome.EnvelopeID, outcome.Document.EnvelopeID)

	createCalls := mock.CreateEnvelopeFromTemplate().Calls()
	require.Len(t, createCalls, 1)
	require.Equal(t, docusign.CreateEnvelopeFromTemplateInput{
		TemplateID: request.TemplateID, TemplateRoles: request.Signers, EmailSubject: request.EmailSubject,
		CustomFields: []docusign.EnvelopeCustomField{{Name: envelopesigning.CorrelationCustomFieldName, Value: requestID}},
	}, createCalls[0].Input)
	statusCalls := mock.GetEnvelope().Calls()
	require.Len(t, statusCalls, 1)
	require.Equal(t, docusign.GetEnvelopeInput{EnvelopeID: outcome.EnvelopeID}, statusCalls[0].Input)
	downloadCalls := mock.DownloadCombinedDocument().Calls()
	require.Len(t, downloadCalls, 1)
	require.Equal(t, docusign.DownloadCombinedDocumentInput{EnvelopeID: outcome.EnvelopeID, IncludeCertificate: true}, downloadCalls[0].Input)
	require.Empty(t, mock.VoidEnvelope().Calls(), "a completed envelope must not be voided")
	require.Empty(t, mock.ListEnvelopeRecipients().Calls(), "the Flow does not list recipients")
}
