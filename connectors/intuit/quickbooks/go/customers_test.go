// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package quickbooks_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/intuit/quickbooks"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestFindCustomerByEmailKeepsExactActiveMatches(t *testing.T) {
	var customers []any
	provider := newRecordingQuickBooks(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeValue(t, response, http.StatusOK, map[string]any{"QueryResponse": map[string]any{"Customer": customers, "startPosition": 1, "maxResults": len(customers)}})
	})
	client := newQuickBooksClient(t, provider.URL)
	run := func(input quickbooks.FindCustomerInput) sdkgo.QueryResult[quickbooks.FindCustomerOutput] {
		result, err := sdkgo.RunQuery(newQuickBooksDexContext("find"), client.FindCustomer(), quickbooksConnection, input)
		require.NoError(t, err)
		return result
	}

	customers = []any{
		customerJSON(testCustomerID, "City Agency", "Accounts@CityAgency.example.com", true),
		customerJSON("61", "City Agency (old)", "accounts@cityagency.example.com", false),
		customerJSON("62", "City Agency Australia", "accounts@cityagency.example.com.au", true),
	}
	result := run(quickbooks.FindCustomerInput{EmailAddress: " accounts@cityagency.example.com "})
	require.Equal(t, quickbooks.FindCustomerBranchFound, result.Branch)
	customer := result.Value.Customer
	require.Equal(t, testCustomerID, customer.CustomerID)
	require.Equal(t, "City Agency", customer.DisplayName)
	require.Equal(t, "(555) 555-0100", customer.Phone)
	require.Equal(t, quickbooks.Decimal("0"), customer.Balance)
	require.Equal(t, time.Date(2026, 9, 2, 15, 0, 0, 0, time.UTC), *customer.UpdatedAt, "QuickBooks times are read as UTC")
	require.Equal(t, testCustomerID, result.Receipt.ProviderObjectID)
	require.Equal(t, "select * from Customer where PrimaryEmailAddr = 'accounts@cityagency.example.com' MAXRESULTS 10",
		provider.request(0).query.Get("query"))
	require.Equal(t, companyPath+"/query", provider.request(0).path)

	result = run(quickbooks.FindCustomerInput{EmailAddress: "accounts@cityagency.example.com", IncludesInactive: true})
	require.Equal(t, quickbooks.FindCustomerBranchAmbiguous, result.Branch, "QuickBooks does not keep email addresses unique")
	require.Len(t, result.Value.Matches, 2)
	require.Equal(t, sdkgo.FailureConflict, result.Failure.Kind)
	require.Equal(t, "select * from Customer where PrimaryEmailAddr = 'accounts@cityagency.example.com' AND Active IN (true, false) MAXRESULTS 10",
		provider.request(1).query.Get("query"))

	customers = nil
	result = run(quickbooks.FindCustomerInput{EmailAddress: "nobody@example.com"})
	require.Equal(t, quickbooks.FindCustomerBranchNotFound, result.Branch, "QuickBooks omits the entity list when nothing matches")
	require.Equal(t, "nobody@example.com", result.Value.EmailAddress)
}

func TestFindCustomerByDisplayNameEscapesApostrophes(t *testing.T) {
	provider := newRecordingQuickBooks(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeValue(t, response, http.StatusOK, map[string]any{"QueryResponse": map[string]any{"Customer": []any{
			customerJSON(testCustomerID, "Adam's Candy Shop", "", true),
		}}})
	})
	result, err := sdkgo.RunQuery(newQuickBooksDexContext("name"), newQuickBooksClient(t, provider.URL).FindCustomer(), quickbooksConnection,
		quickbooks.FindCustomerInput{DisplayName: "Adam's Candy Shop"})
	require.NoError(t, err)
	require.Equal(t, quickbooks.FindCustomerBranchFound, result.Branch)
	require.Equal(t, `select * from Customer where DisplayName = 'Adam\'s Candy Shop' MAXRESULTS 10`, provider.request(0).query.Get("query"))
}

func TestFindCustomerRejectsUnsafeInputWithoutARequest(t *testing.T) {
	provider := newRecordingQuickBooks(t, func(http.ResponseWriter, recordedRequest, int) { t.Fatal("no request is expected") })
	client := newQuickBooksClient(t, provider.URL)
	for name, input := range map[string]quickbooks.FindCustomerInput{
		"neither": {}, "both": {EmailAddress: "a@example.com", DisplayName: "A"}, "display-name email": {EmailAddress: "Ana <a@example.com>"},
		"quote in email": {EmailAddress: "a'b@example.com"}, "backslash in name": {DisplayName: `A\B`}, "line break in name": {DisplayName: "A\nB"},
	} {
		result, err := sdkgo.RunQuery(newQuickBooksDexContext(name), client.FindCustomer(), quickbooksConnection, input)
		require.NoError(t, err)
		require.Equal(t, quickbooks.FindCustomerBranchDefect, result.Branch, name)
	}
	query, err := quickbooks.BuildFindCustomerQuery(quickbooks.FindCustomerInput{DisplayName: "O'Brien"})
	require.NoError(t, err)
	require.Equal(t, `select * from Customer where DisplayName = 'O\'Brien' MAXRESULTS 10`, query)
}

func TestCreateCustomerSendsTheRequestIDAndMapsANameConflict(t *testing.T) {
	provider := newRecordingQuickBooks(t, func(response http.ResponseWriter, _ recordedRequest, index int) {
		if index == 0 {
			writeValue(t, response, http.StatusOK, map[string]any{"Customer": customerJSON(testCustomerID, "City Agency", "accounts@cityagency.example.com", true)})
			return
		}
		writeJSON(t, response, http.StatusBadRequest, faultJSON("ValidationFault", "6240"))
	})
	client := newQuickBooksClient(t, provider.URL)
	input := quickbooks.CreateCustomerInput{
		DisplayName: " City Agency ", EmailAddress: "accounts@cityagency.example.com", GivenName: "Ana", FamilyName: "Ortiz",
		CompanyName: "City Agency LLC", Phone: "(555) 555-0100", CurrencyCode: "USD",
	}
	result, err := sdkgo.RunMutation(newQuickBooksDexContext("create-customer"), client.CreateCustomer(), quickbooksConnection, input)
	require.NoError(t, err)
	require.Equal(t, quickbooks.CreateCustomerBranchCreated, result.Branch)
	require.Equal(t, testCustomerID, result.Value.Customer.CustomerID)
	require.Equal(t, testCustomerID, result.Receipt.ProviderObjectID)
	request := provider.request(0)
	require.Equal(t, http.MethodPost, request.method)
	require.Equal(t, companyPath+"/customer", request.path)
	require.Equal(t, string(result.Receipt.IdempotencyKey), request.query.Get("requestid"))
	require.Equal(t, "application/json", request.header.Get("Content-Type"))
	require.JSONEq(t, `{"DisplayName":"City Agency","PrimaryEmailAddr":{"Address":"accounts@cityagency.example.com"},"GivenName":"Ana",
		"FamilyName":"Ortiz","CompanyName":"City Agency LLC","PrimaryPhone":{"FreeFormNumber":"(555) 555-0100"},"CurrencyRef":{"value":"USD"}}`, request.body)

	result, err = sdkgo.RunMutation(newQuickBooksDexContext("create-customer-conflict"), client.CreateCustomer(), quickbooksConnection, input)
	require.NoError(t, err)
	require.Equal(t, quickbooks.CreateCustomerBranchNameConflict, result.Branch)
	require.Equal(t, "City Agency", result.Value.DisplayName, "the requested name is echoed so findCustomer can resolve it")
	require.Equal(t, sdkgo.FailureConflict, result.Failure.Kind)
	require.Equal(t, "6240", result.Receipt.Metadata[quickbooks.FaultCodeReceiptKey])
	require.NotContains(t, result.Failure.Message, "SENTINEL")
}

func TestCreateCustomerValidatesBeforeAnyRequestAndAnUnusableAnswerIsUncertain(t *testing.T) {
	provider := newRecordingQuickBooks(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeJSON(t, response, http.StatusOK, `{"Customer":{"Id":"not-an-id"}}`)
	})
	client := newQuickBooksClient(t, provider.URL)
	for name, input := range map[string]quickbooks.CreateCustomerInput{
		"no name": {}, "colon": {DisplayName: "Parent:Child"}, "tab": {DisplayName: "A\tB"}, "bad email": {DisplayName: "A", EmailAddress: "a@"},
		"long phone": {DisplayName: "A", Phone: "0123456789012345678901234567890"}, "currency": {DisplayName: "A", CurrencyCode: "usd"},
	} {
		result, err := sdkgo.RunMutation(newQuickBooksDexContext(name), client.CreateCustomer(), quickbooksConnection, input)
		require.NoError(t, err)
		require.Equal(t, quickbooks.CreateCustomerBranchDefect, result.Branch, name)
	}
	require.Zero(t, provider.requestCount())
	result, err := sdkgo.RunMutation(newQuickBooksDexContext("unusable"), client.CreateCustomer(), quickbooksConnection, quickbooks.CreateCustomerInput{DisplayName: "A"})
	require.NoError(t, err)
	require.Equal(t, quickbooks.CreateCustomerBranchUncertain, result.Branch)
}
