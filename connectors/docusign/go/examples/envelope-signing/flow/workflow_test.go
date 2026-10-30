// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package envelopesigning_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/docusign"
	envelopesigning "github.com/superdurable/dex-connectors-library/connectors/docusign/examples/envelope-signing/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func envelopeEvent(customFields ...docusign.EnvelopeCustomField) sdkgo.TriggerEvent[docusign.EnvelopeEvent] {
	return sdkgo.TriggerEvent[docusign.EnvelopeEvent]{
		ID: "93be49ab-0000-0000-0000-000000000001:envelope-completed", OccurredAt: time.Now(),
		Payload: docusign.EnvelopeEvent{
			Event: docusign.ConnectEventEnvelopeCompleted, EnvelopeID: "93be49ab-0000-0000-0000-000000000001", CustomFields: customFields,
		},
	}
}

func TestEnvelopeEventsReachTheFlowNamedByTheirCorrelationField(t *testing.T) {
	correlated := envelopeEvent(docusign.EnvelopeCustomField{Name: envelopesigning.CorrelationCustomFieldName, Value: "opp-123"})
	require.True(t, envelopesigning.AcceptEnvelopeEvent(correlated))
	require.Equal(t, "docusign-signing-opp-123", envelopesigning.ResolveFlowID(correlated))
	require.Equal(t, envelopesigning.EnvelopeEventInput{
		EventID: correlated.ID, EnvelopeID: correlated.Payload.EnvelopeID, Event: docusign.ConnectEventEnvelopeCompleted,
	}, envelopesigning.MapToEnvelopeEventInput(correlated))

	require.False(t, envelopesigning.AcceptEnvelopeEvent(envelopeEvent()), "an envelope this application did not send")
	require.False(t, envelopesigning.AcceptEnvelopeEvent(envelopeEvent(
		docusign.EnvelopeCustomField{Name: envelopesigning.CorrelationCustomFieldName, Value: "../other flow"},
	)), "a correlation value that cannot be a request ID")
}

func TestCreateEnvelopeInputTagsTheEnvelopeWithTheRequestID(t *testing.T) {
	signers := []docusign.TemplateRole{{RoleName: "Customer", Name: "Priya Raman", Email: "priya@meridian.example.com", RoutingOrder: 2}}
	input := envelopesigning.MapToCreateEnvelopeInput(envelopesigning.SigningRequest{
		RequestID: "opp-123", TemplateID: "8c9f5a8b-1111-2222-3333-4f5e6d7c8b9a", Signers: signers, EmailSubject: "MSA",
	})
	require.Equal(t, docusign.CreateEnvelopeFromTemplateInput{
		TemplateID: "8c9f5a8b-1111-2222-3333-4f5e6d7c8b9a", TemplateRoles: signers, EmailSubject: "MSA",
		CustomFields: []docusign.EnvelopeCustomField{{Name: envelopesigning.CorrelationCustomFieldName, Value: "opp-123"}},
	}, input)
	wait := envelopesigning.EnvelopeWait{EnvelopeID: "93be49ab-0000-0000-0000-000000000001"}
	require.Equal(t, docusign.GetEnvelopeInput{EnvelopeID: wait.EnvelopeID}, envelopesigning.MapToGetEnvelopeInput(wait))
	require.Equal(t, docusign.DownloadCombinedDocumentInput{EnvelopeID: wait.EnvelopeID, IncludeCertificate: true},
		envelopesigning.MapToDownloadCombinedDocumentInput(wait))
}

func TestSigningPolicyMustBePositiveWithAVoidReason(t *testing.T) {
	_, err := envelopesigning.NewEnvelopeSigningFlow(docusign.Connection{}, envelopesigning.DefaultSigningPolicy())
	require.NoError(t, err)
	for _, policy := range []envelopesigning.SigningPolicy{
		{StatusPollInterval: 0, SigningDeadline: time.Hour, VoidReason: "x"},
		{StatusPollInterval: time.Hour, SigningDeadline: -time.Hour, VoidReason: "x"},
		{StatusPollInterval: time.Hour, SigningDeadline: time.Hour},
	} {
		_, err := envelopesigning.NewEnvelopeSigningFlow(docusign.Connection{}, policy)
		require.Error(t, err)
	}
	require.Equal(t, "docusign-signing-opp-123", envelopesigning.FlowIDForRequest("opp-123"))
}
