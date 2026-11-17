// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package resend

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	connection := sdkgo.ConnectionRef{Provider: "resend", Name: "resend"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{connection: {APIKey: sdkgo.NewSecretString("test-secret-token")}}, WithHTTPClient(transport))
	require.NoError(t, err)
	createContext := &resendCreateConformanceContext{attempt: 1}
	priorAttempt := conformancePriorAttempt(t, scenario.PriorHeartbeat, func(value []byte) { createContext.heartbeat = value })
	createContext.attempt = priorAttempt + 1
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 {
			createContext.attempt = priorAttempt + int32(attempt)
		}
	}, actions: map[string]conformanceOperationInvocation{
		"sendEmail":                    newConformanceMutationInvocation(t, scenario.Input, providerTestContext{}, client.SendEmail(), connection, scenario.CallID),
		"getEmail":                     newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.GetEmail(), connection),
		"listEmails":                   newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.ListEmails(), connection),
		"getContact":                   newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.GetContact(), connection),
		"listContacts":                 newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.ListContacts(), connection),
		"getDomain":                    newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.GetDomain(), connection),
		"listDomains":                  newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.ListDomains(), connection),
		"updateContact":                newConformanceMutationInvocation(t, scenario.Input, providerTestContext{}, client.UpdateContact(), connection, scenario.CallID),
		"deleteContact":                newConformanceMutationInvocation(t, scenario.Input, providerTestContext{}, client.DeleteContact(), connection, scenario.CallID),
		"deleteDomain":                 newConformanceMutationInvocation(t, scenario.Input, providerTestContext{}, client.DeleteDomain(), connection, scenario.CallID),
		"updateScheduledEmail":         newConformanceMutationInvocation(t, scenario.Input, providerTestContext{}, client.UpdateScheduledEmail(), connection, scenario.CallID),
		"cancelScheduledEmail":         newConformanceMutationInvocation(t, scenario.Input, providerTestContext{}, client.CancelScheduledEmail(), connection, scenario.CallID),
		"createContact":                newConformanceMutationInvocation(t, scenario.Input, createContext, client.CreateContact(), connection, scenario.CallID),
		"createDomain":                 newConformanceMutationInvocation(t, scenario.Input, createContext, client.CreateDomain(), connection, scenario.CallID),
		"updateDomain":                 newConformanceMutationInvocation(t, scenario.Input, providerTestContext{}, client.UpdateDomain(), connection, scenario.CallID),
		"verifyDomain":                 newConformanceMutationInvocation(t, scenario.Input, providerTestContext{}, client.VerifyDomain(), connection, scenario.CallID),
		"sendBatchEmails":              newConformanceMutationInvocation(t, scenario.Input, providerTestContext{}, client.SendBatchEmails(), connection, scenario.CallID),
		"createTopic":                  newConformanceMutationInvocation(t, scenario.Input, createContext, client.CreateTopic(), connection, scenario.CallID),
		"createSegment":                newConformanceMutationInvocation(t, scenario.Input, createContext, client.CreateSegment(), connection, scenario.CallID),
		"createContactProperty":        newConformanceMutationInvocation(t, scenario.Input, createContext, client.CreateContactProperty(), connection, scenario.CallID),
		"createBroadcast":              newConformanceMutationInvocation(t, scenario.Input, createContext, client.CreateBroadcast(), connection, scenario.CallID),
		"getBroadcast":                 newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.GetBroadcast(), connection),
		"listBroadcasts":               newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.ListBroadcasts(), connection),
		"updateBroadcast":              newConformanceMutationInvocation(t, scenario.Input, providerTestContext{}, client.UpdateBroadcast(), connection, scenario.CallID),
		"deleteBroadcast":              newConformanceMutationInvocation(t, scenario.Input, providerTestContext{}, client.DeleteBroadcast(), connection, scenario.CallID),
		"sendBroadcast":                newConformanceMutationInvocation(t, scenario.Input, providerTestContext{}, client.SendBroadcast(), connection, scenario.CallID),
		"cancelBroadcast":              newConformanceMutationInvocation(t, scenario.Input, providerTestContext{}, client.CancelBroadcast(), connection, scenario.CallID),
		"duplicateBroadcast":           newConformanceMutationInvocation(t, scenario.Input, createContext, client.DuplicateBroadcast(), connection, scenario.CallID),
		"listBroadcastRecipients":      newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.ListBroadcastRecipients(), connection),
		"listBroadcastClickedLinks":    newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.ListBroadcastClickedLinks(), connection),
		"getContactProperty":           newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.GetContactProperty(), connection),
		"listContactProperties":        newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.ListContactProperties(), connection),
		"updateContactProperty":        newConformanceMutationInvocation(t, scenario.Input, providerTestContext{}, client.UpdateContactProperty(), connection, scenario.CallID),
		"deleteContactProperty":        newConformanceMutationInvocation(t, scenario.Input, providerTestContext{}, client.DeleteContactProperty(), connection, scenario.CallID),
		"addContactToSegment":          newConformanceMutationInvocation(t, scenario.Input, providerTestContext{}, client.AddContactToSegment(), connection, scenario.CallID),
		"listContactImports":           newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.ListContactImports(), connection),
		"getContactImport":             newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.GetContactImport(), connection),
		"removeContactFromSegment":     newConformanceMutationInvocation(t, scenario.Input, providerTestContext{}, client.RemoveContactFromSegment(), connection, scenario.CallID),
		"listContactSegments":          newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.ListContactSegments(), connection),
		"getContactTopics":             newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.GetContactTopics(), connection),
		"updateContactTopics":          newConformanceMutationInvocation(t, scenario.Input, providerTestContext{}, client.UpdateContactTopics(), connection, scenario.CallID),
		"getSegment":                   newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.GetSegment(), connection),
		"listSegments":                 newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.ListSegments(), connection),
		"updateSegment":                newConformanceMutationInvocation(t, scenario.Input, providerTestContext{}, client.UpdateSegment(), connection, scenario.CallID),
		"deleteSegment":                newConformanceMutationInvocation(t, scenario.Input, providerTestContext{}, client.DeleteSegment(), connection, scenario.CallID),
		"listSegmentContacts":          newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.ListSegmentContacts(), connection),
		"listEmailAttachments":         newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.ListEmailAttachments(), connection),
		"getEmailAttachment":           newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.GetEmailAttachment(), connection),
		"listReceivedEmails":           newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.ListReceivedEmails(), connection),
		"getReceivedEmail":             newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.GetReceivedEmail(), connection),
		"listReceivedEmailAttachments": newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.ListReceivedEmailAttachments(), connection),
		"getReceivedEmailAttachment":   newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.GetReceivedEmailAttachment(), connection),
		"getEmailMetrics":              newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.GetEmailMetrics(), connection),
		"getUsage":                     newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.GetUsage(), connection),
		"listLogs":                     newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.ListLogs(), connection),
		"getLog":                       newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.GetLog(), connection),
		"getTopic":                     newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.GetTopic(), connection),
		"listTopics":                   newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.ListTopics(), connection),
		"updateTopic":                  newConformanceMutationInvocation(t, scenario.Input, providerTestContext{}, client.UpdateTopic(), connection, scenario.CallID),
		"deleteTopic":                  newConformanceMutationInvocation(t, scenario.Input, providerTestContext{}, client.DeleteTopic(), connection, scenario.CallID),
	}}
}

type resendCreateConformanceContext struct {
	providerTestContext
	attempt   int32
	heartbeat json.RawMessage
}

func (context *resendCreateConformanceContext) Attempt() int32 { return context.attempt }
func (context *resendCreateConformanceContext) RecordHeartbeat(value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	context.heartbeat = encoded
	return nil
}
func (context *resendCreateConformanceContext) GetLastHeartbeatValue(target any) (bool, error) {
	if len(context.heartbeat) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(context.heartbeat, target)
}
