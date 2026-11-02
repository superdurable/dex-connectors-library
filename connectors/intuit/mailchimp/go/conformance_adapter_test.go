// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mailchimp_test

import (
    "net/http"
    "testing"

    "github.com/stretchr/testify/require"
    "github.com/superdurable/dex-connectors-library/connectors/intuit/mailchimp/go"
)

const conformanceEndpointPath = "/3.0"
const conformanceFixturePathPrefix = "/3.0"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
    t.Helper()
    client, err := mailchimp.New(mailchimp.Config{}, testCredentialProvider("TESTKEY1-us6"),
        mailchimp.WithAPIBaseURL(endpoint), mailchimp.WithHTTPClient(transport))
    require.NoError(t, err)
    context := newMailchimpDexContext("conformance")
    return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
        "getMember": newConformanceQueryInvocation(t, scenario.Input, context, client.GetMember(), mailchimpConnection),
        "listMembers": newConformanceQueryInvocation(t, scenario.Input, context, client.ListMembers(), mailchimpConnection),
        "upsertMember": newConformanceMutationInvocation(t, scenario.Input, context, client.UpsertMember(), mailchimpConnection, scenario.CallID),
        "updateMemberTags": newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateMemberTags(), mailchimpConnection, scenario.CallID),
        "sendCampaign": newConformanceMutationInvocation(t, scenario.Input, context, client.SendCampaign(), mailchimpConnection, scenario.CallID),
    }}
}
