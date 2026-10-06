//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package paidorder

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	integrationBankAccountID = "35"
	consentAccessToken       = "intuit-access-token-consent"
	// companyTimeZone is the fake company's UTC offset, which QuickBooks writes into MetaData times.
	companyTimeZone = -7 * 60 * 60
)

var (
	companyPathPattern = regexp.MustCompile(`^/v3/company/([0-9]+)(/.*)$`)
	queryPattern       = regexp.MustCompile(`^select \* from (Customer|Invoice)(?: where (.+?))?(?: ORDERBY [A-Za-z.]+)?(?: STARTPOSITION ([0-9]+))? MAXRESULTS ([0-9]+)$`)
	conditionPattern   = regexp.MustCompile(`^([A-Za-z.]+) (=|>|IN) (.+)$`)
)

// fakeQuickBooks replays a repeated requestid's response, answering Fault 600 while the first request still runs.
type fakeQuickBooks struct {
	*httptest.Server
	t     *testing.T
	mutex sync.Mutex
	// delayedRequests tracks handlers still sleeping, so a test can assert their final effect.
	delayedRequests sync.WaitGroup

	clock                time.Time
	nextIdentifier       int
	customers            []*fakeCustomer
	vendorNames          []string
	invoices             map[string]*fakeInvoice
	invoiceOrder         []string
	payments             map[string]bool
	accessTokens         map[string]bool
	refreshToken         string
	refreshCount         int
	responsesByRequestID map[string]*fakeStoredResponse
	counts               map[string]int
	requests             map[string][]fakeRecordedRequest

	delaysFirstCreateInvoice        bool
	delaysFirstPayment              bool
	losesFirstCreateInvoiceResponse bool
	holdsFirstPayment               chan struct{}
	throttlesFirstCustomerLookup    bool
	rejectsCreateInvoice            bool
	revokesRefreshToken             bool
}

type fakeCustomer struct {
	id          string
	displayName string
	companyName string
	email       string
	isActive    bool
}

type fakeInvoice struct {
	id          string
	syncToken   int
	docNumber   string
	customerID  string
	txnDate     string
	dueDate     string
	lines       []map[string]any
	total       *big.Rat
	balance     *big.Rat
	billEmail   string
	emailStatus string
	deliveredAt *time.Time
	privateNote string
	paymentIDs  []string
	createdAt   time.Time
	updatedAt   time.Time
}

type fakeStoredResponse struct {
	fingerprint string
	isDone      bool
	status      int
	body        string
}

type fakeRecordedRequest struct {
	at      time.Time
	method  string
	path    string
	realmID string
	query   url.Values
	header  http.Header
	body    string
}

func newFakeQuickBooks(t *testing.T) *fakeQuickBooks {
	t.Helper()
	provider := &fakeQuickBooks{
		t: t, clock: time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC), nextIdentifier: 58,
		invoices: map[string]*fakeInvoice{}, payments: map[string]bool{}, accessTokens: map[string]bool{consentAccessToken: true},
		refreshToken: "intuit-refresh-token-0", responsesByRequestID: map[string]*fakeStoredResponse{},
		counts: map[string]int{}, requests: map[string][]fakeRecordedRequest{},
	}
	provider.Server = httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *fakeQuickBooks) consentAccessToken() string { return consentAccessToken }

func (provider *fakeQuickBooks) serveHTTP(response http.ResponseWriter, request *http.Request) {
	body, err := io.ReadAll(request.Body)
	if err != nil {
		provider.writeFault(response, http.StatusBadRequest, "ValidationFault", "2010", "unreadable body")
		return
	}
	if request.URL.Path == "/oauth2/v1/tokens/bearer" {
		provider.refreshAccessToken(response, request, body)
		return
	}
	match := companyPathPattern.FindStringSubmatch(request.URL.Path)
	if match == nil {
		provider.writeFault(response, http.StatusNotFound, "ValidationFault", "0", "no such path")
		return
	}
	realmID, path := match[1], match[2]
	provider.mutex.Lock()
	isIssued := provider.accessTokens[strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer ")]
	provider.mutex.Unlock()
	switch {
	case !isIssued:
		provider.writeFault(response, http.StatusUnauthorized, "AuthenticationFault", "3200", "SENTINEL token rejected")
		return
	case realmID != integrationRealmID:
		provider.writeFault(response, http.StatusForbidden, "AuthorizationFault", "120", "SENTINEL another company")
		return
	case request.URL.Query().Get("minorversion") != "75":
		provider.writeFault(response, http.StatusBadRequest, "ValidationFault", "2010", "the fake requires minorversion 75")
		return
	}
	switch {
	case request.Method == http.MethodGet && path == "/query":
		provider.runQuery(response, request, realmID, body)
	case request.Method == http.MethodGet && strings.HasPrefix(path, "/invoice/"):
		provider.getInvoice(response, request, realmID, body, strings.TrimPrefix(path, "/invoice/"))
	case request.Method == http.MethodPost && path == "/customer":
		provider.runIdempotent(response, request, realmID, body, "createCustomer", provider.createCustomer)
	case request.Method == http.MethodPost && path == "/invoice":
		provider.runIdempotent(response, request, realmID, body, "createInvoice", provider.createInvoice)
	case request.Method == http.MethodPost && path == "/payment":
		provider.runIdempotent(response, request, realmID, body, "recordPayment", provider.recordPayment)
	case request.Method == http.MethodPost && strings.HasPrefix(path, "/invoice/") && strings.HasSuffix(path, "/send"):
		invoiceID := strings.TrimSuffix(strings.TrimPrefix(path, "/invoice/"), "/send")
		provider.runIdempotent(response, request, realmID, body, "sendInvoice", func(_ int, request *http.Request, _ []byte) (int, string) {
			return provider.sendInvoice(request, invoiceID)
		})
	default:
		provider.writeFault(response, http.StatusNotFound, "ValidationFault", "0", "no such path")
	}
}

func (provider *fakeQuickBooks) refreshAccessToken(response http.ResponseWriter, request *http.Request, body []byte) {
	provider.record("token", request, "", body)
	clientID, clientSecret, hasBasic := request.BasicAuth()
	form, err := url.ParseQuery(string(body))
	if err != nil || !hasBasic || clientID != integrationClientID || clientSecret != integrationClientSecret {
		provider.writeJSON(response, http.StatusUnauthorized, `{"error":"invalid_client"}`)
		return
	}
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	if provider.revokesRefreshToken || form.Get("grant_type") != "refresh_token" || form.Get("refresh_token") != provider.refreshToken {
		provider.writeJSON(response, http.StatusBadRequest, `{"error":"invalid_grant"}`)
		return
	}
	// Intuit expires the previous refresh token when it issues a new one.
	provider.refreshCount++
	accessToken := fmt.Sprintf("intuit-access-token-%d", provider.refreshCount)
	provider.refreshToken = fmt.Sprintf("intuit-refresh-token-%d", provider.refreshCount)
	provider.accessTokens[accessToken] = true
	provider.writeJSON(response, http.StatusOK, provider.encode(map[string]any{
		"token_type": "bearer", "access_token": accessToken, "expires_in": 3600,
		"refresh_token": provider.refreshToken, "x_refresh_token_expires_in": 8640000,
	}))
}

// runQuery answers the two query shapes the example sends, applying QuickBooks's case-insensitive comparison.
func (provider *fakeQuickBooks) runQuery(response http.ResponseWriter, request *http.Request, realmID string, body []byte) {
	statement := request.URL.Query().Get("query")
	match := queryPattern.FindStringSubmatch(statement)
	if match == nil {
		provider.record("unknownQuery", request, realmID, body)
		provider.writeFault(response, http.StatusBadRequest, "ValidationFault", "4000", "SENTINEL query parser error")
		return
	}
	entity, conditions := match[1], map[string]string{}
	if match[2] != "" {
		for _, condition := range strings.Split(match[2], " AND ") {
			parts := conditionPattern.FindStringSubmatch(condition)
			require.NotNil(provider.t, parts, "condition %q", condition)
			conditions[parts[1]+" "+parts[2]] = unquoteQueryLiteral(parts[3])
		}
	}
	startPosition, maxResults := 1, mustAtoi(provider.t, match[4])
	if match[3] != "" {
		startPosition = mustAtoi(provider.t, match[3])
	}
	if entity == "Customer" {
		attempt := provider.record("findCustomer", request, realmID, body)
		if provider.throttlesFirstCustomerLookup && attempt == 1 {
			response.Header().Set("Retry-After", "1")
			provider.writeFault(response, http.StatusTooManyRequests, "ThrottlingFault", "003001", "SENTINEL slow down")
			return
		}
		provider.writeQueryResponse(response, entity, provider.matchingCustomers(conditions), startPosition, maxResults)
		return
	}
	provider.record("listInvoices", request, realmID, body)
	provider.writeQueryResponse(response, entity, provider.matchingInvoices(conditions), startPosition, maxResults)
}

func (provider *fakeQuickBooks) matchingCustomers(conditions map[string]string) []any {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	var customers []any
	for _, customer := range provider.customers {
		email, hasEmail := conditions["PrimaryEmailAddr ="]
		name, hasName := conditions["DisplayName ="]
		if (hasEmail && !strings.EqualFold(customer.email, email)) || (hasName && !strings.EqualFold(customer.displayName, name)) ||
			(!customer.isActive && conditions["Active IN"] != "(true, false)") {
			continue
		}
		customers = append(customers, provider.customerJSON(customer))
	}
	return customers
}

func (provider *fakeQuickBooks) matchingInvoices(conditions map[string]string) []any {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	var invoices []any
	for _, id := range provider.invoiceOrder {
		invoice := provider.invoices[id]
		customerID, hasCustomer := conditions["CustomerRef ="]
		docNumber, hasDocNumber := conditions["DocNumber ="]
		_, isOpenOnly := conditions["Balance >"]
		if (hasCustomer && invoice.customerID != customerID) || (hasDocNumber && !strings.EqualFold(invoice.docNumber, docNumber)) ||
			(isOpenOnly && invoice.balance.Sign() <= 0) {
			continue
		}
		invoices = append(invoices, provider.invoiceJSON(invoice))
	}
	return invoices
}

func (provider *fakeQuickBooks) writeQueryResponse(response http.ResponseWriter, entity string, entities []any, startPosition int, maxResults int) {
	page := map[string]any{}
	if start := startPosition - 1; start < len(entities) {
		end := min(start+maxResults, len(entities))
		page[entity], page["startPosition"], page["maxResults"] = entities[start:end], startPosition, end-start
	}
	provider.writeJSON(response, http.StatusOK, provider.encode(map[string]any{"QueryResponse": page, "time": provider.responseTime()}))
}

func (provider *fakeQuickBooks) getInvoice(response http.ResponseWriter, request *http.Request, realmID string, body []byte, invoiceID string) {
	provider.record("getInvoice", request, realmID, body)
	provider.mutex.Lock()
	invoice, isFound := provider.invoices[invoiceID]
	var value map[string]any
	if isFound {
		value = provider.invoiceJSON(invoice)
	}
	provider.mutex.Unlock()
	if !isFound {
		provider.writeFault(response, http.StatusBadRequest, "ValidationFault", "610", "SENTINEL Object Not Found")
		return
	}
	provider.writeJSON(response, http.StatusOK, provider.encode(map[string]any{"Invoice": value, "time": provider.responseTime()}))
}

// runIdempotent applies QuickBooks's documented requestid replay around one write.
func (provider *fakeQuickBooks) runIdempotent(response http.ResponseWriter, request *http.Request, realmID string, body []byte, name string,
	execute func(attempt int, request *http.Request, body []byte) (int, string)) {
	attempt := provider.record(name, request, realmID, body)
	requestID := request.URL.Query().Get("requestid")
	if requestID == "" || len(requestID) > 50 {
		provider.writeFault(response, http.StatusBadRequest, "ValidationFault", "2130", "the fake requires a requestid")
		return
	}
	query := request.URL.Query()
	query.Del("requestid")
	fingerprint := request.Method + " " + request.URL.Path + "?" + query.Encode() + " " + string(body)
	key := realmID + "/" + requestID
	provider.mutex.Lock()
	earlier, isKnown := provider.responsesByRequestID[key]
	switch {
	case isKnown && (!earlier.isDone || earlier.fingerprint != fingerprint):
		provider.counts["duplicateRequestID"]++
		provider.mutex.Unlock()
		provider.writeFault(response, http.StatusBadRequest, "ValidationFault", "600", "SENTINEL Duplicate Request ID")
		return
	case isKnown:
		provider.counts["replay"]++
		provider.mutex.Unlock()
		provider.writeJSON(response, earlier.status, earlier.body)
		return
	}
	stored := &fakeStoredResponse{fingerprint: fingerprint}
	provider.responsesByRequestID[key] = stored
	provider.mutex.Unlock()

	status, answer := execute(attempt, request, body)
	provider.mutex.Lock()
	stored.status, stored.body, stored.isDone = status, answer, true
	provider.mutex.Unlock()
	if name == "createInvoice" && provider.losesFirstCreateInvoiceResponse && attempt == 1 {
		provider.dropConnection(response)
		return
	}
	provider.writeJSON(response, status, answer)
}

func (provider *fakeQuickBooks) createCustomer(_ int, _ *http.Request, body []byte) (int, string) {
	var payload struct {
		DisplayName      string `json:"DisplayName"`
		CompanyName      string `json:"CompanyName"`
		PrimaryEmailAddr struct {
			Address string `json:"Address"`
		} `json:"PrimaryEmailAddr"`
	}
	require.NoError(provider.t, json.Unmarshal(body, &payload))
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	names := append([]string(nil), provider.vendorNames...)
	for _, customer := range provider.customers {
		names = append(names, customer.displayName)
	}
	for _, name := range names {
		if strings.EqualFold(name, payload.DisplayName) {
			return http.StatusBadRequest, provider.faultBody("ValidationFault", "6240", "SENTINEL The name supplied already exists.")
		}
	}
	customer := &fakeCustomer{
		id: provider.newIdentifier(), displayName: payload.DisplayName, companyName: payload.CompanyName,
		email: payload.PrimaryEmailAddr.Address, isActive: true,
	}
	provider.customers = append(provider.customers, customer)
	provider.counts["appliedCreateCustomer"]++
	return http.StatusOK, provider.encode(map[string]any{"Customer": provider.customerJSON(customer), "time": provider.responseTime()})
}

func (provider *fakeQuickBooks) createInvoice(attempt int, _ *http.Request, body []byte) (int, string) {
	if provider.delaysFirstCreateInvoice && attempt == 1 {
		provider.delayedRequests.Add(1)
		defer provider.delayedRequests.Done()
		time.Sleep(slowResponseDelay)
	}
	if provider.rejectsCreateInvoice {
		return http.StatusBadRequest, provider.faultBody("ValidationFault", "6000", "SENTINEL A business validation error has occurred")
	}
	var payload struct {
		CustomerRef struct {
			Value string `json:"value"`
		} `json:"CustomerRef"`
		TxnDate, DueDate, DocNumber, PrivateNote string
		BillEmail                                struct {
			Address string `json:"Address"`
		} `json:"BillEmail"`
		Line []struct {
			DetailType          string      `json:"DetailType"`
			Amount              json.Number `json:"Amount"`
			Description         string      `json:"Description"`
			SalesItemLineDetail struct {
				ItemRef struct {
					Value string `json:"value"`
				} `json:"ItemRef"`
				Qty       json.Number `json:"Qty"`
				UnitPrice json.Number `json:"UnitPrice"`
			} `json:"SalesItemLineDetail"`
		} `json:"Line"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	require.NoError(provider.t, decoder.Decode(&payload))
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	if provider.customer(payload.CustomerRef.Value) == nil {
		return http.StatusBadRequest, provider.faultBody("ValidationFault", "2500", "SENTINEL Invalid Reference Id")
	}
	for _, existing := range provider.invoices {
		if strings.EqualFold(existing.docNumber, payload.DocNumber) {
			return http.StatusBadRequest, provider.faultBody("ValidationFault", "6140", "SENTINEL Duplicate Document Number Error")
		}
	}
	now := provider.advanceClock()
	invoice := &fakeInvoice{
		id: provider.newIdentifier(), docNumber: payload.DocNumber, customerID: payload.CustomerRef.Value, txnDate: payload.TxnDate,
		dueDate: payload.DueDate, total: new(big.Rat), billEmail: payload.BillEmail.Address, emailStatus: "NotSet",
		privateNote: payload.PrivateNote, createdAt: now, updatedAt: now,
	}
	for index, line := range payload.Line {
		amount := mustRat(provider.t, line.Amount.String())
		product := new(big.Rat).Mul(mustRat(provider.t, line.SalesItemLineDetail.Qty.String()), mustRat(provider.t, line.SalesItemLineDetail.UnitPrice.String()))
		if product.FloatString(2) != amount.FloatString(2) {
			return http.StatusBadRequest, provider.faultBody("ValidationFault", "6070", "SENTINEL Amount is not equal to UnitPrice * Qty")
		}
		invoice.total.Add(invoice.total, amount)
		invoice.lines = append(invoice.lines, map[string]any{
			"Id": strconv.Itoa(index + 1), "LineNum": index + 1, "Description": line.Description, "Amount": decimalJSON(amount),
			"DetailType": "SalesItemLineDetail", "SalesItemLineDetail": map[string]any{
				"ItemRef": map[string]any{"value": line.SalesItemLineDetail.ItemRef.Value, "name": "Item " + line.SalesItemLineDetail.ItemRef.Value},
				"Qty":     line.SalesItemLineDetail.Qty, "UnitPrice": line.SalesItemLineDetail.UnitPrice,
				"TaxCodeRef": map[string]any{"value": "NON"},
			},
		})
	}
	invoice.balance = new(big.Rat).Set(invoice.total)
	provider.invoices[invoice.id] = invoice
	provider.invoiceOrder = append(provider.invoiceOrder, invoice.id)
	provider.counts["appliedCreateInvoice"]++
	return http.StatusOK, provider.encode(map[string]any{"Invoice": provider.invoiceJSON(invoice), "time": provider.responseTime()})
}

func (provider *fakeQuickBooks) recordPayment(attempt int, _ *http.Request, body []byte) (int, string) {
	if provider.delaysFirstPayment && attempt == 1 {
		provider.delayedRequests.Add(1)
		defer provider.delayedRequests.Done()
		time.Sleep(slowResponseDelay)
	}
	if provider.holdsFirstPayment != nil && attempt == 1 {
		provider.delayedRequests.Add(1)
		defer provider.delayedRequests.Done()
		<-provider.holdsFirstPayment
	}
	var payload struct {
		CustomerRef struct {
			Value string `json:"value"`
		} `json:"CustomerRef"`
		TotalAmt            json.Number `json:"TotalAmt"`
		TxnDate             string      `json:"TxnDate"`
		PaymentRefNum       string      `json:"PaymentRefNum"`
		DepositToAccountRef struct {
			Value string `json:"value"`
		} `json:"DepositToAccountRef"`
		Line []struct {
			Amount    json.Number `json:"Amount"`
			LinkedTxn []struct {
				TxnID   string `json:"TxnId"`
				TxnType string `json:"TxnType"`
			} `json:"LinkedTxn"`
		} `json:"Line"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	require.NoError(provider.t, decoder.Decode(&payload))
	require.Len(provider.t, payload.Line, 1)
	require.Len(provider.t, payload.Line[0].LinkedTxn, 1)
	amount := mustRat(provider.t, payload.TotalAmt.String())
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	invoice, isFound := provider.invoices[payload.Line[0].LinkedTxn[0].TxnID]
	if !isFound || invoice.customerID != payload.CustomerRef.Value || amount.Cmp(invoice.balance) > 0 ||
		mustRat(provider.t, payload.Line[0].Amount.String()).Cmp(amount) != 0 {
		return http.StatusBadRequest, provider.faultBody("ValidationFault", "6000", "SENTINEL payment does not fit the invoice")
	}
	paymentID := provider.newIdentifier()
	provider.payments[paymentID] = true
	invoice.balance.Sub(invoice.balance, amount)
	invoice.paymentIDs = append(invoice.paymentIDs, paymentID)
	invoice.syncToken++
	invoice.updatedAt = provider.advanceClock()
	provider.counts["appliedRecordPayment"]++
	return http.StatusOK, provider.encode(map[string]any{"Payment": map[string]any{
		"Id": paymentID, "SyncToken": "0", "CustomerRef": map[string]any{"value": invoice.customerID, "name": "Customer"},
		"TotalAmt": decimalJSON(amount), "UnappliedAmt": json.Number("0"), "TxnDate": payload.TxnDate, "PaymentRefNum": payload.PaymentRefNum,
		"DepositToAccountRef": map[string]any{"value": payload.DepositToAccountRef.Value}, "CurrencyRef": map[string]any{"value": "USD"},
		"Line":     []any{map[string]any{"Amount": decimalJSON(amount), "LinkedTxn": []any{map[string]any{"TxnId": invoice.id, "TxnType": "Invoice"}}}},
		"MetaData": map[string]any{"CreateTime": provider.timestamp(invoice.updatedAt), "LastUpdatedTime": provider.timestamp(invoice.updatedAt)},
	}, "time": provider.responseTime()})
}

func (provider *fakeQuickBooks) sendInvoice(request *http.Request, invoiceID string) (int, string) {
	if request.Header.Get("Content-Type") != "application/octet-stream" {
		return http.StatusBadRequest, provider.faultBody("ValidationFault", "2010", "the fake requires application/octet-stream")
	}
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	invoice, isFound := provider.invoices[invoiceID]
	if !isFound {
		return http.StatusBadRequest, provider.faultBody("ValidationFault", "610", "SENTINEL Object Not Found")
	}
	if sendTo := request.URL.Query().Get("sendTo"); sendTo != "" {
		invoice.billEmail = sendTo
	}
	if invoice.billEmail == "" {
		return http.StatusBadRequest, provider.faultBody("ValidationFault", "6000", "SENTINEL no email address")
	}
	deliveredAt := provider.advanceClock()
	invoice.emailStatus, invoice.deliveredAt, invoice.updatedAt = "EmailSent", &deliveredAt, deliveredAt
	invoice.syncToken++
	provider.counts["appliedSendInvoice"]++
	return http.StatusOK, provider.encode(map[string]any{"Invoice": provider.invoiceJSON(invoice), "time": provider.responseTime()})
}

// seedCustomer stores a customer and returns its Id.
func (provider *fakeQuickBooks) seedCustomer(displayName string, email string, isActive bool) string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	customer := &fakeCustomer{id: provider.newIdentifier(), displayName: displayName, email: email, isActive: isActive}
	provider.customers = append(provider.customers, customer)
	return customer.id
}

// seedVendor reserves a display name the way a vendor does, so a customer cannot take it.
func (provider *fakeQuickBooks) seedVendor(displayName string) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.vendorNames = append(provider.vendorNames, displayName)
}

// seedInvoice stores an invoice with a total and a remaining balance and returns its Id.
func (provider *fakeQuickBooks) seedInvoice(customerID string, docNumber string, total string, balance string) string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	totalAmount := mustRat(provider.t, total)
	now := provider.advanceClock()
	invoice := &fakeInvoice{
		id: provider.newIdentifier(), docNumber: docNumber, customerID: customerID, txnDate: "2026-09-28", dueDate: "2026-10-12",
		total: totalAmount, balance: mustRat(provider.t, balance), emailStatus: "NotSet", createdAt: now, updatedAt: now,
		lines: []map[string]any{{"Id": "1", "LineNum": 1, "Amount": decimalJSON(totalAmount), "DetailType": "SalesItemLineDetail",
			"SalesItemLineDetail": map[string]any{"ItemRef": map[string]any{"value": "1", "name": "Services"}}}},
	}
	provider.invoices[invoice.id] = invoice
	provider.invoiceOrder = append(provider.invoiceOrder, invoice.id)
	return invoice.id
}

// customer requires provider.mutex.
func (provider *fakeQuickBooks) customer(customerID string) *fakeCustomer {
	for _, customer := range provider.customers {
		if customer.id == customerID {
			return customer
		}
	}
	return nil
}

// customerJSON requires provider.mutex; QuickBooks omits Active for an active customer only sometimes, so it is always sent.
func (provider *fakeQuickBooks) customerJSON(customer *fakeCustomer) map[string]any {
	value := map[string]any{
		"Id": customer.id, "SyncToken": "0", "DisplayName": customer.displayName, "FullyQualifiedName": customer.displayName,
		"PrintOnCheckName": customer.displayName, "Active": customer.isActive, "Balance": json.Number("0"), "Taxable": false,
		"CurrencyRef": map[string]any{"value": "USD", "name": "United States Dollar"}, "PreferredDeliveryMethod": "Email",
		"MetaData": map[string]any{"CreateTime": provider.timestamp(provider.clock), "LastUpdatedTime": provider.timestamp(provider.clock)},
		"domain":   "QBO", "sparse": false,
	}
	if customer.companyName != "" {
		value["CompanyName"] = customer.companyName
	}
	if customer.email != "" {
		value["PrimaryEmailAddr"] = map[string]any{"Address": customer.email}
	}
	return value
}

// invoiceJSON requires provider.mutex and writes amounts as JSON numbers, with the subtotal line QuickBooks adds.
func (provider *fakeQuickBooks) invoiceJSON(invoice *fakeInvoice) map[string]any {
	lines := make([]any, 0, len(invoice.lines)+1)
	for _, line := range invoice.lines {
		lines = append(lines, line)
	}
	lines = append(lines, map[string]any{"Amount": decimalJSON(invoice.total), "DetailType": "SubTotalLineDetail", "SubTotalLineDetail": map[string]any{}})
	linked := []any{}
	for _, paymentID := range invoice.paymentIDs {
		linked = append(linked, map[string]any{"TxnId": paymentID, "TxnType": "Payment"})
	}
	value := map[string]any{
		"Id": invoice.id, "SyncToken": strconv.Itoa(invoice.syncToken), "DocNumber": invoice.docNumber, "TxnDate": invoice.txnDate,
		"DueDate": invoice.dueDate, "CustomerRef": map[string]any{"value": invoice.customerID, "name": "Customer " + invoice.customerID},
		"CurrencyRef": map[string]any{"value": "USD", "name": "United States Dollar"}, "Line": lines, "LinkedTxn": linked,
		"TotalAmt": decimalJSON(invoice.total), "Balance": decimalJSON(invoice.balance), "TxnTaxDetail": map[string]any{"TotalTax": json.Number("0")},
		"EmailStatus": invoice.emailStatus, "PrintStatus": "NeedToPrint", "PrivateNote": invoice.privateNote,
		"MetaData": map[string]any{"CreateTime": provider.timestamp(invoice.createdAt), "LastUpdatedTime": provider.timestamp(invoice.updatedAt)},
		"domain":   "QBO", "sparse": false,
	}
	if invoice.billEmail != "" {
		value["BillEmail"] = map[string]any{"Address": invoice.billEmail}
	}
	if invoice.deliveredAt != nil {
		value["DeliveryInfo"] = map[string]any{"DeliveryType": "Email", "DeliveryTime": provider.timestamp(*invoice.deliveredAt)}
	}
	return value
}

// newIdentifier requires provider.mutex and returns QuickBooks's decimal Ids.
func (provider *fakeQuickBooks) newIdentifier() string {
	identifier := strconv.Itoa(provider.nextIdentifier)
	provider.nextIdentifier++
	return identifier
}

// advanceClock requires provider.mutex; every write moves the clock forward one minute.
func (provider *fakeQuickBooks) advanceClock() time.Time {
	provider.clock = provider.clock.Add(time.Minute)
	return provider.clock
}

// timestamp writes an instant the way QuickBooks does, with the company's UTC offset.
func (provider *fakeQuickBooks) timestamp(instant time.Time) string {
	return instant.In(time.FixedZone("PDT", companyTimeZone)).Format(time.RFC3339)
}

func (provider *fakeQuickBooks) responseTime() string {
	return time.Now().In(time.FixedZone("PDT", companyTimeZone)).Format("2006-01-02T15:04:05.000-07:00")
}

func (provider *fakeQuickBooks) record(name string, request *http.Request, realmID string, body []byte) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.counts[name]++
	provider.requests[name] = append(provider.requests[name], fakeRecordedRequest{
		at: time.Now(), method: request.Method, path: request.URL.Path, realmID: realmID, query: request.URL.Query(),
		header: request.Header.Clone(), body: string(body),
	})
	return provider.counts[name]
}

func (provider *fakeQuickBooks) faultBody(faultType string, code string, message string) string {
	return provider.encode(map[string]any{"Fault": map[string]any{
		"Error": []any{map[string]any{"Message": message, "Detail": message + " detail", "code": code, "element": ""}}, "type": faultType,
	}, "time": provider.responseTime()})
}

func (provider *fakeQuickBooks) writeFault(response http.ResponseWriter, status int, faultType string, code string, message string) {
	provider.writeJSON(response, status, provider.faultBody(faultType, code, message))
}

func (provider *fakeQuickBooks) encode(value any) string {
	encoded, err := json.Marshal(value)
	require.NoError(provider.t, err)
	return string(encoded)
}

func (provider *fakeQuickBooks) writeJSON(response http.ResponseWriter, status int, body string) {
	response.Header().Set("Content-Type", "application/json;charset=UTF-8")
	response.Header().Set("intuit_tid", "1-66f9a1c2-5b3e4f7a2d1c0b9e8f7a6b5c")
	response.WriteHeader(status)
	if _, err := io.WriteString(response, body); err != nil {
		provider.t.Logf("fake QuickBooks response write failed: %v", err)
	}
}

// dropConnection closes the connection after QuickBooks applied the request, as a lost response would.
func (provider *fakeQuickBooks) dropConnection(response http.ResponseWriter) {
	hijacker, isHijacker := response.(http.Hijacker)
	require.True(provider.t, isHijacker)
	connection, _, err := hijacker.Hijack()
	require.NoError(provider.t, err)
	require.NoError(provider.t, connection.Close())
}

func (provider *fakeQuickBooks) waitForDelayedRequests(t *testing.T) {
	t.Helper()
	finished := make(chan struct{})
	go func() {
		provider.delayedRequests.Wait()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(2 * slowResponseDelay):
		t.Fatal("a delayed fake QuickBooks request did not finish")
	}
}

func (provider *fakeQuickBooks) count(name string) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.counts[name]
}

func (provider *fakeQuickBooks) totalRequests() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	total := 0
	for _, requests := range provider.requests {
		total += len(requests)
	}
	return total
}

func (provider *fakeQuickBooks) customerCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return len(provider.customers)
}

func (provider *fakeQuickBooks) invoiceCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return len(provider.invoices)
}

func (provider *fakeQuickBooks) invoiceIDs() []string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return append([]string(nil), provider.invoiceOrder...)
}

func (provider *fakeQuickBooks) paymentCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return len(provider.payments)
}

func (provider *fakeQuickBooks) invoice(invoiceID string) fakeInvoice {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return *provider.invoices[invoiceID]
}

func (provider *fakeQuickBooks) lastRequest(name string) fakeRecordedRequest {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	requests := provider.requests[name]
	require.NotEmpty(provider.t, requests, name)
	return requests[len(requests)-1]
}

func (provider *fakeQuickBooks) requestTimes(name string) []time.Time {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	var times []time.Time
	for _, request := range provider.requests[name] {
		times = append(times, request.at)
	}
	return times
}

func (provider *fakeQuickBooks) distinctRequestIDs(name string) []string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	seen := map[string]bool{}
	for _, request := range provider.requests[name] {
		seen[request.query.Get("requestid")] = true
	}
	requestIDs := make([]string, 0, len(seen))
	for requestID := range seen {
		requestIDs = append(requestIDs, requestID)
	}
	sort.Strings(requestIDs)
	return requestIDs
}

// unquoteQueryLiteral reverses the connector's quoting: single quotes with backslash-escaped apostrophes.
func unquoteQueryLiteral(value string) string {
	if len(value) < 2 || value[0] != '\'' || value[len(value)-1] != '\'' {
		return value
	}
	return strings.ReplaceAll(value[1:len(value)-1], `\'`, "'")
}

// decimalJSON writes zero as 0 and every other amount with two places, as exact JSON numbers.
func decimalJSON(value *big.Rat) json.Number {
	if value.Sign() == 0 {
		return json.Number("0")
	}
	return json.Number(value.FloatString(2))
}

func mustRat(t *testing.T, value string) *big.Rat {
	t.Helper()
	parsed, isParsed := new(big.Rat).SetString(value)
	require.True(t, isParsed, "decimal %q", value)
	return parsed
}

func mustAtoi(t *testing.T, value string) int {
	t.Helper()
	parsed, err := strconv.Atoi(value)
	require.NoError(t, err)
	return parsed
}
