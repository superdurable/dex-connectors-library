// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package freshbooks

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

var accountIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
var amountPattern = regexp.MustCompile(`^-?[0-9]+(?:\.[0-9]+)?$`)
var currencyCodePattern = regexp.MustCompile(`^[A-Z]{3}$`)

// GetInvoiceInput identifies an invoice within an accounting account.
type GetInvoiceInput struct {
	// AccountID is business_memberships[].business.account_id from FreshBooks /me.
	AccountID string `json:"accountId"`
	// InvoiceID is the positive numeric ID shown by the invoice API or webhook.
	InvoiceID int64 `json:"invoiceId"`
}

// Invoice contains selected financial fields from one FreshBooks invoice.
type Invoice struct {
	// AccountID identifies the accounting account.
	AccountID string `json:"accountId"`
	// InvoiceID is the stable numeric invoice ID.
	InvoiceID int64 `json:"invoiceId"`
	// InvoiceNumber is the displayed document number.
	InvoiceNumber string `json:"invoiceNumber"`
	// Amount is the exact decimal total as a string.
	Amount string `json:"amount"`
	// CurrencyCode is the ISO currency code returned with Amount.
	CurrencyCode string `json:"currencyCode"`
	// PaymentStatus is FreshBooks' current payment status.
	PaymentStatus string `json:"paymentStatus"`
}

// GetInvoiceOperation reads one FreshBooks invoice.
type GetInvoiceOperation struct{ client *Client }

// GetInvoice binds the Query to this client.
func (client *Client) GetInvoice() GetInvoiceOperation { return GetInvoiceOperation{client} }

// Definition returns the generated invoice Query contract.
func (GetInvoiceOperation) Definition() sdkgo.QueryDefinition { return GetInvoiceDefinition }

// Invoke reads one invoice and classifies the provider outcome.
func (operation GetInvoiceOperation) Invoke(call sdkgo.Call, input GetInvoiceInput) sdkgo.QueryAttempt[Invoice] {
	if !accountIDPattern.MatchString(input.AccountID) || input.InvoiceID <= 0 {
		return invoiceBranch(GetInvoiceBranchDefect, sdkgo.FailureValidation, "FreshBooks account ID or invoice ID is invalid")
	}
	path := "/accounting/account/" + input.AccountID + "/invoices/invoices/" + strconv.FormatInt(input.InvoiceID, 10)
	response := operation.client.get(call, path)
	if errors.Is(response.err, errCredential) {
		return invoiceBranch(GetInvoiceBranchDefect, sdkgo.FailureAuthentication, "FreshBooks OAuth credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return invoiceRetry(sdkgo.FailureTransport, "FreshBooks request failed", 0)
	}
	if response.status == http.StatusTooManyRequests {
		return invoiceRetry(sdkgo.FailureRateLimit, "FreshBooks rate limited the request", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return invoiceRetry(sdkgo.FailureAvailability, "FreshBooks is temporarily unavailable", response.retryAfter)
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return invoiceBranch(GetInvoiceBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "FreshBooks response exceeded the configured limit")
	}
	if response.err != nil {
		return invoiceRetry(sdkgo.FailureTransport, "FreshBooks response was interrupted", 0)
	}
	if response.status == http.StatusNotFound {
		return sdkgo.NewQueryBranch(GetInvoiceBranchMissing, Invoice{}, nil, sdkgo.Receipt{})
	}
	if response.status != http.StatusOK {
		return invoiceBranch(GetInvoiceBranchProviderRejected, sdkgo.FailureProviderRejection, "FreshBooks rejected HTTP "+strconv.Itoa(response.status))
	}
	invoice, err := decodeInvoice(response.body, input)
	if err != nil {
		return invoiceBranch(GetInvoiceBranchInvalidResponse, sdkgo.FailureProtocol, "FreshBooks returned invalid invoice JSON")
	}
	return sdkgo.NewQueryBranch(GetInvoiceBranchFound, invoice, nil, sdkgo.Receipt{})
}

func decodeInvoice(body []byte, input GetInvoiceInput) (Invoice, error) {
	var envelope struct {
		Response struct {
			Result struct {
				Invoice struct {
					AccountID     string `json:"accountid"`
					InvoiceID     int64  `json:"invoiceid"`
					InvoiceNumber string `json:"invoice_number"`
					Amount        struct {
						Amount string `json:"amount"`
						Code   string `json:"code"`
					} `json:"amount"`
					PaymentStatus string `json:"payment_status"`
				} `json:"invoice"`
			} `json:"result"`
		} `json:"response"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return Invoice{}, err
	}
	providerInvoice := envelope.Response.Result.Invoice
	if providerInvoice.AccountID != input.AccountID || providerInvoice.InvoiceID != input.InvoiceID || providerInvoice.InvoiceNumber == "" || !amountPattern.MatchString(providerInvoice.Amount.Amount) || !currencyCodePattern.MatchString(providerInvoice.Amount.Code) || providerInvoice.PaymentStatus == "" {
		return Invoice{}, errors.New("FreshBooks invoice fields are invalid")
	}
	return Invoice{AccountID: providerInvoice.AccountID, InvoiceID: providerInvoice.InvoiceID, InvoiceNumber: providerInvoice.InvoiceNumber, Amount: providerInvoice.Amount.Amount, CurrencyCode: providerInvoice.Amount.Code, PaymentStatus: providerInvoice.PaymentStatus}, nil
}

func invoiceFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "freshbooks", Operation: "getInvoice", Message: message}
}
func invoiceBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[Invoice] {
	failure := invoiceFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, Invoice{}, &failure, sdkgo.Receipt{})
}
func invoiceRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[Invoice] {
	return sdkgo.NewQueryRetry[Invoice](invoiceFailure(kind, message), delay)
}
