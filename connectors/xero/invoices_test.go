// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package xero_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/xero"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func validCreateInvoiceInput() xero.CreateInvoiceInput {
	return xero.CreateInvoiceInput{
		Type: xero.InvoiceTypeAccountsReceivable, ContactID: strings.ToUpper(testContactID),
		LineItems: []xero.InvoiceLineItemInput{
			{Description: " Spring bouquet ", Quantity: "2", UnitAmount: "49.9500", AccountCode: "200", TaxType: "OUTPUT", DiscountRate: "12.5"},
			{Description: "Card", Quantity: "0.3333", UnitAmount: "-1.2345", AccountCode: "200"},
		},
		Date: "2026-10-01", DueDate: "2026-10-15", LineAmountTypes: xero.LineAmountTypesExclusive, CurrencyCode: "USD",
		Reference: "ORDER-1042", Status: xero.InvoiceStatusAuthorised,
	}
}

func TestGetInvoiceReadsExactDecimalsAndDates(t *testing.T) {
	provider := newRecordingXero(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		invoice := invoiceJSON()
		invoice["Payments"] = []any{map[string]any{"PaymentID": testPaymentID, "Date": "/Date(1759276800000+0000)/", "Amount": "10.00"}}
		writeValue(t, response, http.StatusOK, map[string]any{"Status": "OK", "Invoices": []any{invoice}})
	})
	result, err := sdkgo.RunQuery(newXeroDexContext("get"), newXeroClient(t, provider.URL).GetInvoice(), xeroConnection,
		xero.GetInvoiceInput{InvoiceID: strings.ToUpper(testInvoiceID)})
	require.NoError(t, err)
	require.Equal(t, xero.GetInvoiceBranchFound, result.Branch)
	invoice := result.Value
	require.Equal(t, testInvoiceID, invoice.InvoiceID)
	require.Equal(t, "INV-0042", invoice.InvoiceNumber)
	require.Equal(t, xero.InvoiceStatusAuthorised, invoice.Status)
	require.Equal(t, "2026-10-01", invoice.Date)
	require.Equal(t, "2026-10-15", invoice.DueDate)
	require.Equal(t, xero.Decimal("109.89"), invoice.Total)
	require.Equal(t, xero.Decimal("1.0000000000"), invoice.CurrencyRate, "Xero's digits are kept, not reformatted")
	require.Equal(t, xero.Decimal("49.9500"), invoice.LineItems[0].UnitAmount)
	require.Equal(t, xero.InvoicePayment{PaymentID: testPaymentID, Date: "2025-10-01", Amount: "10.00"}, invoice.Payments[0])
	require.Equal(t, time.Date(2025, 10, 1, 9, 0, 0, 0, time.UTC), invoice.UpdatedAt)
	require.Equal(t, testInvoiceID, result.Receipt.ProviderObjectID)
	require.Equal(t, "5fe9659e-e5cc-4747-ad01-47adb038bf34", result.Receipt.ProviderRequestID)

	request := provider.request(0)
	require.Equal(t, "/api.xro/2.0/Invoices/"+testInvoiceID, request.path)
	require.Equal(t, "4", request.query.Get("unitdp"), "four-decimal unit amounts are requested")
	require.Equal(t, "Bearer "+testAccessToken, request.header.Get("Authorization"))
	require.Empty(t, request.header.Get("Xero-Tenant-Id"))
}

func TestGetInvoiceByNumberEscapesThePathAndRejectsAnotherInvoice(t *testing.T) {
	provider := newRecordingXero(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeValue(t, response, http.StatusOK, map[string]any{"Invoices": []any{invoiceJSON()}})
	})
	client := newXeroClient(t, provider.URL)
	result, err := sdkgo.RunQuery(newXeroDexContext("number"), client.GetInvoice(), xeroConnection, xero.GetInvoiceInput{InvoiceNumber: "INV-0042"})
	require.NoError(t, err)
	require.Equal(t, xero.GetInvoiceBranchFound, result.Branch)
	result, err = sdkgo.RunQuery(newXeroDexContext("other"), client.GetInvoice(), xeroConnection, xero.GetInvoiceInput{InvoiceNumber: "INV/9 9"})
	require.NoError(t, err)
	require.Equal(t, xero.GetInvoiceBranchInvalidResponse, result.Branch, "an answer about another invoice is never accepted")
	require.Equal(t, "/api.xro/2.0/Invoices/INV/9 9", provider.request(1).path)

	for name, input := range map[string]xero.GetInvoiceInput{
		"neither": {}, "both": {InvoiceID: testInvoiceID, InvoiceNumber: "INV-1"}, "malformed id": {InvoiceID: "INV-1"},
		"quote in number": {InvoiceNumber: `INV"1`},
	} {
		result, err := sdkgo.RunQuery(newXeroDexContext(name), client.GetInvoice(), xeroConnection, input)
		require.NoError(t, err)
		require.Equal(t, xero.GetInvoiceBranchDefect, result.Branch, name)
	}
	require.Equal(t, 2, provider.requestCount(), "invalid identifiers are rejected before any request")
}

func TestListInvoicesSendsOptimisedFiltersAndReportsPagination(t *testing.T) {
	provider := newRecordingXero(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeValue(t, response, http.StatusOK, map[string]any{
			"pagination": map[string]any{"page": 2, "pageSize": 10, "pageCount": 3, "itemCount": 27}, "Invoices": []any{invoiceJSON()},
		})
	})
	modifiedSince := time.Date(2026, 9, 30, 23, 15, 7, 0, time.FixedZone("NZST", 12*3600))
	result, err := sdkgo.RunQuery(newXeroDexContext("list"), newXeroClient(t, provider.URL).ListInvoices(), xeroConnection, xero.ListInvoicesInput{
		Statuses: []xero.InvoiceStatus{xero.InvoiceStatusAuthorised, xero.InvoiceStatusPaid}, ContactIDs: []string{testContactID},
		InvoiceNumbers: []string{"INV-0042", "INV-0043"}, Type: xero.InvoiceTypeAccountsReceivable, Reference: " ORDER-1042 ",
		ModifiedSince: &modifiedSince, Page: 2, PageSize: 10,
	})
	require.NoError(t, err)
	require.Equal(t, xero.ListInvoicesBranchListed, result.Branch)
	require.Len(t, result.Value.Invoices, 1)
	require.Equal(t, 3, result.Value.PageCount)
	require.Equal(t, 27, result.Value.ItemCount)
	require.True(t, result.Value.HasMorePages)

	request := provider.request(0)
	require.Equal(t, "/api.xro/2.0/Invoices", request.path)
	require.Equal(t, "AUTHORISED,PAID", request.query.Get("Statuses"))
	require.Equal(t, testContactID, request.query.Get("ContactIDs"))
	require.Equal(t, "INV-0042,INV-0043", request.query.Get("InvoiceNumbers"))
	require.Equal(t, `Type=="ACCREC" AND Reference=="ORDER-1042"`, request.query.Get("where"))
	require.Equal(t, "2", request.query.Get("page"))
	require.Equal(t, "10", request.query.Get("pageSize"))
	require.Equal(t, "4", request.query.Get("unitdp"))
	require.Equal(t, "2026-09-30T11:15:07", request.header.Get("If-Modified-Since"), "Xero's documented UTC timestamp format")
}

func TestListInvoicesDefaultsAndBoundsTheFirstPage(t *testing.T) {
	provider := newRecordingXero(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeValue(t, response, http.StatusOK, map[string]any{"Invoices": []any{}})
	})
	client := newXeroClient(t, provider.URL)
	result, err := sdkgo.RunQuery(newXeroDexContext("default"), client.ListInvoices(), xeroConnection, xero.ListInvoicesInput{})
	require.NoError(t, err)
	require.Equal(t, xero.ListInvoicesBranchListed, result.Branch)
	require.Empty(t, result.Value.Invoices)
	require.False(t, result.Value.HasMorePages)
	require.Equal(t, "1", provider.request(0).query.Get("page"))
	require.Equal(t, "25", provider.request(0).query.Get("pageSize"))
	require.Empty(t, provider.request(0).query.Get("where"))

	for name, input := range map[string]xero.ListInvoicesInput{
		"page size above the bound": {PageSize: 101},
		"negative page":             {Page: -1},
		"unknown status":            {Statuses: []xero.InvoiceStatus{"OPEN"}},
		"malformed contact":         {ContactIDs: []string{"City Agency"}},
		"quote in reference":        {Reference: `ORDER"1`},
		"comma in number":           {InvoiceNumbers: []string{"INV-1,INV-2"}},
		"unknown type":              {Type: "SALES"},
	} {
		result, err := sdkgo.RunQuery(newXeroDexContext(name), client.ListInvoices(), xeroConnection, input)
		require.NoError(t, err)
		require.Equal(t, xero.ListInvoicesBranchDefect, result.Branch, name)
	}
	require.Equal(t, 1, provider.requestCount())
}

func TestCreateInvoiceSendsExactNumberLiteralsUnderTheStepKey(t *testing.T) {
	provider := newRecordingXero(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		invoice := invoiceJSON()
		invoice["Warnings"] = []any{map[string]any{"Message": "SENTINEL rate looks wrong"}}
		writeValue(t, response, http.StatusOK, map[string]any{"Id": "put", "Status": "OK", "Invoices": []any{invoice}})
	})
	client := newXeroClient(t, provider.URL)
	result, err := sdkgo.RunMutation(newXeroDexContext("create"), client.CreateInvoice(), xeroConnection, validCreateInvoiceInput())
	require.NoError(t, err)
	require.Equal(t, xero.CreateInvoiceBranchCreated, result.Branch)
	require.Equal(t, testInvoiceID, result.Value.Invoice.InvoiceID)
	require.Equal(t, xero.Decimal("109.89"), result.Value.Invoice.AmountDue)
	require.Equal(t, 1, result.Value.WarningCount, "warnings are counted, never quoted")
	require.Equal(t, testInvoiceID, result.Receipt.ProviderObjectID)

	request := provider.request(0)
	require.Equal(t, http.MethodPut, request.method, "PUT only creates")
	require.Equal(t, "/api.xro/2.0/Invoices", request.path)
	require.Equal(t, "4", request.query.Get("unitdp"))
	require.Equal(t, string(result.Receipt.IdempotencyKey), request.header.Get("Idempotency-Key"))
	require.Equal(t, "application/json", request.header.Get("Content-Type"))
	require.Equal(t, `{"Type":"ACCREC","Contact":{"ContactID":"`+testContactID+`"},"Date":"2026-10-01","DueDate":"2026-10-15",`+
		`"LineAmountTypes":"Exclusive","LineItems":[`+
		`{"Description":"Spring bouquet","Quantity":2,"UnitAmount":49.9500,"AccountCode":"200","TaxType":"OUTPUT","DiscountRate":12.5},`+
		`{"Description":"Card","Quantity":0.3333,"UnitAmount":-1.2345,"AccountCode":"200"}],`+
		`"CurrencyCode":"USD","Reference":"ORDER-1042","Status":"AUTHORISED"}`, request.body)

	_, err = sdkgo.RunMutation(newXeroDexContext("create"), client.CreateInvoice(), xeroConnection, validCreateInvoiceInput())
	require.NoError(t, err)
	require.Equal(t, request.body, provider.request(1).body, "the body is deterministic, as Xero requires for a repeated key")
	require.Equal(t, request.header.Get("Idempotency-Key"), provider.request(1).header.Get("Idempotency-Key"), "one Step execution, one key")
}

func TestCreateInvoiceRetriesOnlyWhatTheKeyMakesSafe(t *testing.T) {
	for _, test := range []struct {
		name  string
		reply func(http.ResponseWriter)
		kind  sdkgo.FailureKind
		delay time.Duration
	}{
		{name: "lost response", reply: func(response http.ResponseWriter) { dropConnection(t, response) }, kind: sdkgo.FailureTransport},
		{name: "minute limit", reply: func(response http.ResponseWriter) {
			response.Header().Set("X-Rate-Limit-Problem", "minute")
			response.Header().Set("Retry-After", "17")
			writeJSON(t, response, http.StatusTooManyRequests, `{"Message":"SENTINEL"}`)
		}, kind: sdkgo.FailureRateLimit, delay: 17 * time.Second},
		{name: "concurrency limit", reply: func(response http.ResponseWriter) {
			response.Header().Set("X-Rate-Limit-Problem", "concurrent")
			writeJSON(t, response, http.StatusTooManyRequests, `{}`)
		}, kind: sdkgo.FailureRateLimit},
		{name: "maintenance", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusServiceUnavailable, `The Xero API is currently offline for maintenance`)
		}, kind: sdkgo.FailureAvailability},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingXero(t, func(response http.ResponseWriter, _ recordedRequest, _ int) { test.reply(response) })
			_, err := sdkgo.RunMutation(newXeroDexContext("create-"+test.name), newXeroClient(t, provider.URL).CreateInvoice(), xeroConnection, validCreateInvoiceInput())
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			require.Equal(t, test.kind, retry.Failure.Kind)
			require.NotContains(t, retry.Failure.Message, "SENTINEL")
			if test.delay > 0 {
				var retryAfter *dex.RetryAfterError
				require.ErrorAs(t, err, &retryAfter)
				require.Equal(t, test.delay, retryAfter.After)
			}
			require.NotEmpty(t, provider.request(0).header.Get("Idempotency-Key"))
		})
	}
}

func TestCreateInvoiceMapsRejectionsLimitsAndUnknownOutcomes(t *testing.T) {
	for _, test := range []struct {
		name     string
		reply    func(http.ResponseWriter)
		branch   sdkgo.BranchID
		kind     sdkgo.FailureKind
		message  string
		metadata map[string]string
	}{
		{name: "validation", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusBadRequest, `{"ErrorNumber":10,"Type":"ValidationException","Message":"SENTINEL",
				"Elements":[{"ValidationErrors":[{"Message":"SENTINEL one"},{"Message":"SENTINEL two"}]}]}`)
		}, branch: xero.CreateInvoiceBranchProviderRejected, kind: sdkgo.FailureValidation,
			message: "Xero rejected the request (HTTP 400) [ValidationException; error 10; 2 validation errors]"},
		{name: "key reused with another request", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusBadRequest, `Idempotency Key: SENTINEL is used with a different request.`)
		}, branch: xero.CreateInvoiceBranchProviderRejected, kind: sdkgo.FailureValidation},
		{name: "missing scope", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusForbidden, `{"title":"Forbidden","status":403,"detail":"AuthorizationUnsuccessful"}`)
		}, branch: xero.CreateInvoiceBranchProviderRejected, kind: sdkgo.FailureAuthorization,
			message: "Xero denied access to the organisation or scope (HTTP 403) [AuthorizationUnsuccessful]"},
		{name: "daily limit", reply: func(response http.ResponseWriter) {
			response.Header().Set("X-Rate-Limit-Problem", "day")
			response.Header().Set("Retry-After", "43200")
			writeJSON(t, response, http.StatusTooManyRequests, `{}`)
		}, branch: xero.CreateInvoiceBranchDailyLimitReached, kind: sdkgo.FailureQuotaExhausted,
			metadata: map[string]string{xero.RetryAfterSecondsReceiptKey: "43200", xero.RateLimitProblemReceiptKey: "day"}},
		{name: "unnamed long limit", reply: func(response http.ResponseWriter) {
			response.Header().Set("Retry-After", "3600")
			writeJSON(t, response, http.StatusTooManyRequests, `{}`)
		}, branch: xero.CreateInvoiceBranchDailyLimitReached, kind: sdkgo.FailureQuotaExhausted,
			metadata: map[string]string{xero.RetryAfterSecondsReceiptKey: "3600"}},
		{name: "cached internal error", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusInternalServerError, `{"Message":"SENTINEL"}`)
		}, branch: xero.CreateInvoiceBranchUncertain, kind: sdkgo.FailureAvailability},
		{name: "unusable accepted answer", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusOK, `{"Invoices":[{"InvoiceID":"not-a-uuid"}]}`)
		}, branch: xero.CreateInvoiceBranchUncertain, kind: sdkgo.FailureProtocol},
		{name: "oversized accepted answer", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusOK, `{"Invoices":[{"Description":"`+strings.Repeat("x", 2048)+`"}]}`)
		}, branch: xero.CreateInvoiceBranchUncertain, kind: sdkgo.FailureResponseTooLarge},
		{name: "per-invoice error", reply: func(response http.ResponseWriter) {
			invoice := invoiceJSON()
			invoice["HasErrors"], invoice["ValidationErrors"] = true, []any{map[string]any{"Message": "SENTINEL"}}
			writeValue(t, response, http.StatusOK, map[string]any{"Invoices": []any{invoice}})
		}, branch: xero.CreateInvoiceBranchProviderRejected, kind: sdkgo.FailureValidation},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingXero(t, func(response http.ResponseWriter, _ recordedRequest, _ int) { test.reply(response) })
			client, err := xero.New(xero.Config{MaxResponseBytes: 1024}, customConnectionCredentials(), xero.WithLocalProviderURL(provider.URL))
			require.NoError(t, err)
			result, err := sdkgo.RunMutation(newXeroDexContext("create-"+test.name), client.CreateInvoice(), xeroConnection, validCreateInvoiceInput())
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.NotNil(t, result.Failure)
			require.Equal(t, test.kind, result.Failure.Kind)
			require.NotContains(t, result.Failure.Message, "SENTINEL")
			if test.message != "" {
				require.Equal(t, test.message, result.Failure.Message)
			}
			for key, value := range test.metadata {
				require.Equal(t, value, result.Receipt.Metadata[key], key)
			}
			require.Equal(t, "ORDER-1042", result.Value.Reference, "the reference is echoed for reconciliation")
			require.Equal(t, 1, provider.requestCount(), "no branch resends the request")
		})
	}
}

func TestCreateInvoiceRejectsInexactOrInvalidInputBeforeAnyRequest(t *testing.T) {
	provider := newRecordingXero(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeJSON(t, response, http.StatusInternalServerError, `{}`)
	})
	client := newXeroClient(t, provider.URL)
	for name, change := range map[string]func(*xero.CreateInvoiceInput){
		"exponent amount":        func(input *xero.CreateInvoiceInput) { input.LineItems[0].UnitAmount = "4.995e1" },
		"leading zero":           func(input *xero.CreateInvoiceInput) { input.LineItems[0].Quantity = "02" },
		"five decimal places":    func(input *xero.CreateInvoiceInput) { input.LineItems[0].UnitAmount = "49.95001" },
		"comma decimal":          func(input *xero.CreateInvoiceInput) { input.LineItems[0].UnitAmount = "49,95" },
		"discount on a bill":     func(input *xero.CreateInvoiceInput) { input.Type = xero.InvoiceTypeAccountsPayable },
		"discount above 100":     func(input *xero.CreateInvoiceInput) { input.LineItems[0].DiscountRate = "100.01" },
		"negative discount":      func(input *xero.CreateInvoiceInput) { input.LineItems[0].DiscountRate = "-1" },
		"lowercase currency":     func(input *xero.CreateInvoiceInput) { input.CurrencyCode = "usd" },
		"paid status":            func(input *xero.CreateInvoiceInput) { input.Status = xero.InvoiceStatusPaid },
		"unknown line type":      func(input *xero.CreateInvoiceInput) { input.LineAmountTypes = "Gross" },
		"contact name":           func(input *xero.CreateInvoiceInput) { input.ContactID = "City Agency" },
		"no lines":               func(input *xero.CreateInvoiceInput) { input.LineItems = nil },
		"blank description":      func(input *xero.CreateInvoiceInput) { input.LineItems[0].Description = "  " },
		"slashed date":           func(input *xero.CreateInvoiceInput) { input.DueDate = "15/10/2026" },
		"multi-line reference":   func(input *xero.CreateInvoiceInput) { input.Reference = "ORDER\n1042" },
		"non-ASCII number":       func(input *xero.CreateInvoiceInput) { input.InvoiceNumber = "INV-№1" },
		"account code injection": func(input *xero.CreateInvoiceInput) { input.LineItems[0].AccountCode = `200"` },
	} {
		t.Run(name, func(t *testing.T) {
			input := validCreateInvoiceInput()
			input.LineItems = append([]xero.InvoiceLineItemInput(nil), input.LineItems...)
			change(&input)
			result, err := sdkgo.RunMutation(newXeroDexContext("invalid-"+name), client.CreateInvoice(), xeroConnection, input)
			require.NoError(t, err)
			require.Equal(t, xero.CreateInvoiceBranchDefect, result.Branch)
			require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
		})
	}
	require.Zero(t, provider.requestCount())
}
