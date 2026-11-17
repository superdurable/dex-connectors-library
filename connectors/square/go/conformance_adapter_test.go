// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package square

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	connection := sdkgo.ConnectionRef{Provider: "square", Name: "square"}
	credentials := Credentials{AccessToken: sdkgo.NewSecretString("test-token")}
	if scenario.Name == "listPayments-defect" || scenario.Name == "listRefunds-defect" || scenario.Name == "listCustomers-defect" || scenario.Name == "listCatalogObjects-defect" || scenario.Name == "listLocations-defect" {
		credentials = Credentials{}
	}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{connection: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listPayments":              newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListPayments(), connection),
		"getPayment":                newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetPayment(), connection),
		"createPayment":             newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.CreatePayment(), connection, scenario.CallID),
		"updatePayment":             newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdatePayment(), connection, scenario.CallID),
		"cancelPayment":             newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.CancelPayment(), connection, scenario.CallID),
		"completePayment":           newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.CompletePayment(), connection, scenario.CallID),
		"listRefunds":               newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListRefunds(), connection),
		"getRefund":                 newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetRefund(), connection),
		"refundPayment":             newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.RefundPayment(), connection, scenario.CallID),
		"listCustomers":             newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListCustomers(), connection),
		"searchCustomers":           newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.SearchCustomers(), connection),
		"getCustomer":               newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetCustomer(), connection),
		"createCustomer":            newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.CreateCustomer(), connection, scenario.CallID),
		"updateCustomer":            newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateCustomer(), connection, scenario.CallID),
		"deleteCustomer":            newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteCustomer(), connection, scenario.CallID),
		"searchOrders":              newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.SearchOrders(), connection),
		"getOrder":                  newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetOrder(), connection),
		"createOrder":               newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.CreateOrder(), connection, scenario.CallID),
		"updateOrder":               newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateOrder(), connection, scenario.CallID),
		"listCatalogObjects":        newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListCatalogObjects(), connection),
		"searchCatalogObjects":      newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.SearchCatalogObjects(), connection),
		"getCatalogObject":          newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetCatalogObject(), connection),
		"upsertCatalogObject":       newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpsertCatalogObject(), connection, scenario.CallID),
		"deleteCatalogObject":       newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteCatalogObject(), connection, scenario.CallID),
		"retrieveInventoryCount":    newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.RetrieveInventoryCount(), connection),
		"setInventoryPhysicalCount": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.SetInventoryPhysicalCount(), connection, scenario.CallID),
		"listInvoices":              newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListInvoices(), connection),
		"getInvoice":                newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetInvoice(), connection),
		"createInvoice":             newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.CreateInvoice(), connection, scenario.CallID),
		"updateInvoice":             newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateInvoice(), connection, scenario.CallID),
		"publishInvoice":            newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.PublishInvoice(), connection, scenario.CallID),
		"cancelInvoice":             newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.CancelInvoice(), connection, scenario.CallID),
		"deleteInvoice":             newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteInvoice(), connection, scenario.CallID),
		"listLocations":             newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListLocations(), connection),
		"getLocation":               newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetLocation(), connection),
	}}
}
