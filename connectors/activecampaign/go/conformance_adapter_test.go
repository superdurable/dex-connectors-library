// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package activecampaign

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/api/3"
const conformanceFixturePathPrefix = "/api/3"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "activecampaign", Name: "activecampaign"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{reference: {APIKey: sdkgo.NewSecretString("test-token")}}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listContactListMemberships": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListContactListMemberships(), reference),
		"subscribeContactToList":     newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.SubscribeContactToList(), reference, scenario.CallID),
		"unsubscribeContactFromList": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UnsubscribeContactFromList(), reference, scenario.CallID),
		"listContactTags":            newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListContactTags(), reference),
		"applyTagToContact":          newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.ApplyTagToContact(), reference, scenario.CallID),
		"removeTagFromContact":       newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.RemoveTagFromContact(), reference, scenario.CallID),
		"listDealNotes":              newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListDealNotes(), reference),
		"getDealNote":                newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetDealNote(), reference),
		"createDealNote":             newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.CreateDealNote(), reference, scenario.CallID),
		"updateDealNote":             newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateDealNote(), reference, scenario.CallID),
		"listDeals":                  newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListDeals(), reference),
		"getDeal":                    newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetDeal(), reference),
		"createDeal":                 newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.CreateDeal(), reference, scenario.CallID),
		"updateDeal":                 newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateDeal(), reference, scenario.CallID),
		"deleteDeal":                 newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteDeal(), reference, scenario.CallID),
		"listTags":                   newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListTags(), reference),
		"listAccounts":               newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListAccounts(), reference),
		"listAccountContacts":        newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListAccountContacts(), reference),
		"getAccountContact":          newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetAccountContact(), reference),
		"createAccountContact":       newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.CreateAccountContact(), reference, scenario.CallID),
		"updateAccountContact":       newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateAccountContact(), reference, scenario.CallID),
		"deleteAccountContact":       newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteAccountContact(), reference, scenario.CallID),
		"getAccount":                 newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetAccount(), reference),
		"createAccount":              newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.CreateAccount(), reference, scenario.CallID),
		"updateAccount":              newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateAccount(), reference, scenario.CallID),
		"deleteAccount":              newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteAccount(), reference, scenario.CallID),
		"getTag":                     newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetTag(), reference),
		"createTag":                  newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.CreateTag(), reference, scenario.CallID),
		"updateTag":                  newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateTag(), reference, scenario.CallID),
		"deleteTag":                  newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteTag(), reference, scenario.CallID),
		"listLists":                  newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListLists(), reference),
		"getList":                    newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetList(), reference),
		"listContacts":               newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListContacts(), reference),
		"getContact":                 newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetContact(), reference),
		"syncContact":                newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.SyncContact(), reference, scenario.CallID),
		"updateContact":              newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateContact(), reference, scenario.CallID),
	}}
}
