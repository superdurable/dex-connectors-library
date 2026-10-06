// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package docusign_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/docusign"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// recipientsJSON follows DocuSign's example: string routing orders and an access code the connector drops.
const recipientsJSON = `{"currentRoutingOrder":"2","recipientCount":"4",` +
	`"signers":[` +
	`{"recipientId":"2","name":"Priya Raman","email":"priya@meridian.example.com","roleName":"Customer","routingOrder":"2","status":"sent","sentDateTime":"2026-10-04T12:05:00.0000000Z","accessCode":"SENTINEL-DOCUSIGN-ACCESS-CODE"},` +
	`{"recipientId":"1","name":"Dana Iwu","email":"dana@example.com","roleName":"Internal Legal","routingOrder":"1","status":"completed","signedDateTime":"2026-10-04T12:04:00.5000000Z"}],` +
	`"carbonCopies":[{"recipientId":"3","name":"Records","email":"records@example.com","routingOrder":"3","status":"created"}],` +
	`"inPersonSigners":[{"recipientId":"10","hostName":"Host","routingOrder":"2","status":"declined","declinedReason":"Wrong address"}]}`

func TestListEnvelopeRecipientsFlattensEveryTypeInRoutingOrder(t *testing.T) {
	fake := newFakeDocuSign(t, map[string]http.HandlerFunc{"GET " + envelopePath + "/recipients": respondJSON(http.StatusOK, recipientsJSON)})
	client := newTestClient(t, fake, staticCredentials(productionCredentials()), docusign.Config{})
	result, err := sdkgo.RunQuery(newTestDexContext("list"), client.ListEnvelopeRecipients(), testConnection, docusign.ListEnvelopeRecipientsInput{EnvelopeID: testEnvelopeID})
	require.NoError(t, err)
	require.Equal(t, docusign.ListEnvelopeRecipientsBranchListed, result.Branch)
	recipients := result.Value
	require.Equal(t, 2, recipients.CurrentRoutingOrder)
	require.Equal(t, testEnvelopeID, recipients.EnvelopeID)
	var order []string
	for _, recipient := range recipients.Recipients {
		order = append(order, string(recipient.Type)+":"+recipient.RecipientID)
	}
	require.Equal(t, []string{"signer:1", "signer:2", "inPersonSigner:10", "carbonCopy:3"}, order)
	require.Equal(t, time.Date(2026, time.October, 4, 12, 4, 0, 500000000, time.UTC), *recipients.Recipients[0].SignedAt)
	require.Equal(t, "Customer", recipients.Recipients[1].RoleName)
	require.Equal(t, "Wrong address", recipients.Recipients[2].DeclinedReason)
	requireNoSecrets(t, result)
	require.NotContains(t, sdkgoJSON(t, result), "SENTINEL-DOCUSIGN-ACCESS-CODE", "access codes never cross the boundary")
}

func TestListEnvelopeRecipientsRejectsMalformedRecipients(t *testing.T) {
	for name, body := range map[string]string{
		"routing order not a number": `{"signers":[{"recipientId":"1","routingOrder":"first","status":"sent"}]}`,
		"recipient without status":   `{"signers":[{"recipientId":"1","routingOrder":"1"}]}`,
		"not JSON":                   `[`,
	} {
		t.Run(name, func(t *testing.T) {
			fake := newFakeDocuSign(t, map[string]http.HandlerFunc{"GET " + envelopePath + "/recipients": respondJSON(http.StatusOK, body)})
			client := newTestClient(t, fake, staticCredentials(productionCredentials()), docusign.Config{})
			result, err := sdkgo.RunQuery(newTestDexContext("list"), client.ListEnvelopeRecipients(), testConnection, docusign.ListEnvelopeRecipientsInput{EnvelopeID: testEnvelopeID})
			require.NoError(t, err)
			require.Equal(t, docusign.ListEnvelopeRecipientsBranchInvalidResponse, result.Branch)
		})
	}
}

func TestListEnvelopeRecipientsReportsAMissingEnvelope(t *testing.T) {
	fake := newFakeDocuSign(t, map[string]http.HandlerFunc{"GET " + envelopePath + "/recipients": respondJSON(http.StatusBadRequest, docusignError("ENVELOPE_DOES_NOT_EXIST"))})
	client := newTestClient(t, fake, staticCredentials(productionCredentials()), docusign.Config{})
	result, err := sdkgo.RunQuery(newTestDexContext("list"), client.ListEnvelopeRecipients(), testConnection, docusign.ListEnvelopeRecipientsInput{EnvelopeID: testEnvelopeID})
	require.NoError(t, err)
	require.Equal(t, docusign.ListEnvelopeRecipientsBranchNotFound, result.Branch)
	require.Equal(t, "ENVELOPE_DOES_NOT_EXIST", result.Receipt.Metadata["errorCode"])
}
