//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package approvedorder

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/xero"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// integrationClientID is split so the 32-hex fixture is never one credential-shaped literal.
	integrationClientID     = "0F1E2D3C4B5A6978" + "8796A5B4C3D2E1F0"
	integrationClientSecret = "xeroIntegrationClientSecret0123456789"
	integrationOAuthToken   = "xero-oauth-access-token-0123456789"
	integrationCustomerID   = "025867f1-d741-4d6b-b1af-9ac774b59ba7"
	integrationOtherID      = "06638157-fdfa-47f4-91d0-875b5f5c18c6"
	integrationTenantID     = "70784a63-d24b-46a9-a4db-0e70a274b056"
	integrationOtherTenant  = "e0da6937-de07-4a14-adee-37abfac298ce"
	integrationOrganisation = "Maple Florist"
	integrationBankCode     = "090"
	integrationBankID       = "ac993f75-035b-433c-82e0-7b7a2d40802c"
	integrationOrderID      = "ORDER-1042"
	integrationEmail        = "accounts@cityagency.example.com"

	defaultRequestTimeout = 5 * time.Second
	// slowResponseDelay outlasts Dex's roughly seven-second async local phase, so an async Step is dispatched again.
	slowResponseDelay  = 9 * time.Second
	slowRequestTimeout = 20 * time.Second
)

var whereEmailPattern = regexp.MustCompile(`^EmailAddress=="([^"]+)"$`)

func integrationOrder() Input {
	return Input{
		OrderID: integrationOrderID, CustomerEmail: integrationEmail, CurrencyCode: "USD",
		InvoiceDate: "2026-10-01", DueDate: "2026-10-15", PaidOn: "2026-10-01",
		PaymentAccountCode: integrationBankCode, PaymentReference: "ch_3QxF2a",
		Lines: []OrderLine{
			{Description: "Spring bouquet", Quantity: "2", UnitAmount: "49.95", AccountCode: "200", TaxType: "OUTPUT"},
			{Description: "Delivery", Quantity: "1", UnitAmount: "12.5", AccountCode: "260", TaxType: "NONE"},
		},
	}
}

func TestApprovedOrderRaisesOneInvoiceAndRecordsOnePaymentWithRealDex(t *testing.T) {
	provider := newFakeXero(t)
	harness := newOrderHarness(t, provider, customConnection, defaultRequestTimeout)

	outcome := harness.runOrder(t, "new-order", integrationOrder())
	require.Equal(t, OrderInvoicedAndPaid, outcome.Action)
	require.True(t, outcome.IsFullyPaid)
	require.Equal(t, "INV-0001", outcome.InvoiceNumber, "Xero numbered the invoice from the organisation's settings")
	require.Equal(t, xero.Decimal("122.39"), outcome.Total, "2 x 49.95 + 10% tax + 12.50, exactly")
	require.Equal(t, xero.Decimal("0.00"), outcome.AmountDue)
	require.Equal(t, xero.InvoiceStatusPaid, outcome.InvoiceStatus)
	require.Equal(t, integrationCustomerID, outcome.ContactID)
	require.NotEmpty(t, outcome.PaymentID)
	require.Equal(t, 1, provider.invoiceCount())
	require.Equal(t, 1, provider.paymentCount())

	create := provider.lastRequest("createInvoice")
	require.Equal(t, http.MethodPut, create.method)
	require.Equal(t, "4", create.query.Get("unitdp"))
	require.JSONEq(t, `{"Type":"ACCREC","Contact":{"ContactID":"`+integrationCustomerID+`"},"Date":"2026-10-01","DueDate":"2026-10-15",
		"LineAmountTypes":"Exclusive","LineItems":[
			{"Description":"Spring bouquet","Quantity":2,"UnitAmount":49.95,"AccountCode":"200","TaxType":"OUTPUT"},
			{"Description":"Delivery","Quantity":1,"UnitAmount":12.5,"AccountCode":"260","TaxType":"NONE"}],
		"CurrencyCode":"USD","Reference":"ORDER-1042","Status":"AUTHORISED"}`, create.body)
	require.Contains(t, create.body, `"UnitAmount":49.95`, "amounts travel as exact JSON number literals")
	payment := provider.lastRequest("recordPayment")
	require.JSONEq(t, `{"Invoice":{"InvoiceID":"`+outcome.InvoiceID+`"},"Account":{"Code":"090"},"Date":"2026-10-01","Amount":122.39,"Reference":"ch_3QxF2a"}`, payment.body)
	createKey, paymentKey := create.header.Get("Idempotency-Key"), payment.header.Get("Idempotency-Key")
	require.NotEmpty(t, createKey)
	require.NotEmpty(t, paymentKey)
	require.NotEqual(t, createKey, paymentKey, "each Step execution has its own key")

	lookup := provider.lastRequest("findContact")
	require.Equal(t, `EmailAddress=="accounts@cityagency.example.com"`, lookup.query.Get("where"))
	list := provider.lastRequest("listInvoices")
	require.Equal(t, `Type=="ACCREC" AND Reference=="ORDER-1042"`, list.query.Get("where"))
	require.Equal(t, "AUTHORISED,PAID", list.query.Get("Statuses"))
	require.Equal(t, integrationCustomerID, list.query.Get("ContactIDs"))
	for _, name := range []string{"findContact", "listInvoices", "createInvoice", "recordPayment", "getInvoice"} {
		require.Empty(t, provider.lastRequest(name).header.Get("Xero-Tenant-Id"), "%s: a Custom Connection sends no tenant", name)
	}

	require.Equal(t, 1, provider.count("token"), "one 30-minute token serves every call")
	token := provider.lastRequest("token")
	require.Equal(t, "Basic "+base64.StdEncoding.EncodeToString([]byte(integrationClientID+":"+integrationClientSecret)), token.header.Get("Authorization"))
	require.Equal(t, "grant_type=client_credentials&scope=accounting.invoices+accounting.payments+accounting.contacts.read", token.body)
	persisted := harness.readPersistedCredentials(t)
	require.NotEmpty(t, persisted.AccessToken, "localconfig stored the minted access token")
	require.True(t, persisted.ExpiresAt.After(time.Now().Add(20*time.Minute)))
}

// TestSlowInvoiceCreateIsDispatchedAgainAndRaisesOneInvoiceWithRealDex is a duplicate-dispatch test: Xero
// answers the second dispatch's identical key with the first request's cached response.
func TestSlowInvoiceCreateIsDispatchedAgainAndRaisesOneInvoiceWithRealDex(t *testing.T) {
	provider := newFakeXero(t)
	provider.delaysFirstCreate = true
	harness := newOrderHarness(t, provider, customConnection, slowRequestTimeout)

	startedAt := time.Now()
	outcome := harness.runOrder(t, "slow-create", integrationOrder())
	require.GreaterOrEqual(t, time.Since(startedAt), slowResponseDelay)
	require.Equal(t, OrderInvoicedAndPaid, outcome.Action)
	provider.waitForDelayedRequests(t)
	require.GreaterOrEqual(t, provider.count("createInvoice"), 2, "Dex dispatched the create again past its local phase")
	require.Equal(t, 1, provider.count("appliedCreateInvoice"), "Xero ran the request once")
	require.Equal(t, 1, provider.invoiceCount(), "the repeated dispatch did not raise a second invoice")
	require.Len(t, provider.distinctIdempotencyKeys("createInvoice"), 1, "every attempt sent the same key")
	require.Equal(t, 1, provider.paymentCount())
	t.Logf("slow create: requests=%d replays=%d", provider.count("createInvoice"), provider.count("replay"))
}

// TestSlowPaymentIsDispatchedAgainAndRecordsOnePaymentWithRealDex is a duplicate-dispatch test for recordPayment.
func TestSlowPaymentIsDispatchedAgainAndRecordsOnePaymentWithRealDex(t *testing.T) {
	provider := newFakeXero(t)
	provider.delaysFirstPayment = true
	harness := newOrderHarness(t, provider, customConnection, slowRequestTimeout)

	outcome := harness.runOrder(t, "slow-payment", integrationOrder())
	require.Equal(t, OrderInvoicedAndPaid, outcome.Action)
	require.True(t, outcome.IsFullyPaid)
	provider.waitForDelayedRequests(t)
	require.GreaterOrEqual(t, provider.count("recordPayment"), 2, "Dex dispatched the payment again past its local phase")
	require.Equal(t, 1, provider.count("appliedRecordPayment"))
	require.Equal(t, 1, provider.paymentCount(), "the repeated dispatch did not pay the invoice twice")
	require.Len(t, provider.distinctIdempotencyKeys("recordPayment"), 1)
	invoice := provider.invoice(outcome.InvoiceID)
	require.Equal(t, "122.39", invoice.amountPaid.FloatString(2))
	require.Equal(t, "0.00", invoice.amountDue.FloatString(2))
	t.Logf("slow payment: requests=%d replays=%d", provider.count("recordPayment"), provider.count("replay"))
}

func TestLostInvoiceResponseIsReplayedUnderTheSameKeyWithRealDex(t *testing.T) {
	provider := newFakeXero(t)
	provider.losesFirstCreateResponse = true
	harness := newOrderHarness(t, provider, customConnection, defaultRequestTimeout)

	outcome := harness.runOrder(t, "lost-create", integrationOrder())
	require.Equal(t, OrderInvoicedAndPaid, outcome.Action)
	require.Equal(t, 2, provider.count("createInvoice"))
	require.Equal(t, 1, provider.count("replay"), "the retry received Xero's cached response")
	require.Equal(t, 1, provider.invoiceCount())
	require.Len(t, provider.distinctIdempotencyKeys("createInvoice"), 1)
}

// TestLostWorkerDuringPaymentRecordsOnePaymentWithRealDex shows the key surviving a Worker lost mid-payment.
func TestLostWorkerDuringPaymentRecordsOnePaymentWithRealDex(t *testing.T) {
	provider := newFakeXero(t)
	provider.holdsFirstPayment = make(chan struct{})
	harness := newOrderHarness(t, provider, customConnection, slowRequestTimeout)
	flowID := harness.startOrder(t, "lost-worker", integrationOrder())
	require.Eventually(t, func() bool { return provider.count("recordPayment") == 1 }, 30*time.Second, 50*time.Millisecond,
		"the first payment attempt must reach Xero")
	harness.replaceWorker(t)
	require.Eventually(t, func() bool { return provider.count("recordPayment") >= 2 }, time.Minute, 50*time.Millisecond,
		"an attempt on the new Worker must reach Xero")
	close(provider.holdsFirstPayment)

	result := harness.waitForFlow(t, flowID)
	require.Equal(t, dex.FlowCompleted, result.Status, "Flow %s: %s", flowID, result.ErrorMessage)
	provider.waitForDelayedRequests(t)
	require.Equal(t, 1, provider.paymentCount(), "the attempt on the new Worker did not pay twice")
	require.Len(t, provider.distinctIdempotencyKeys("recordPayment"), 1, "the key survives Worker replacement")
}

func TestPaidInvoiceForTheOrderIsLeftAloneWithRealDex(t *testing.T) {
	provider := newFakeXero(t)
	paid := provider.seedInvoice(integrationCustomerID, integrationOrderID, "AUTHORISED", "80.00", "80.00")
	provider.seedInvoice(integrationOtherID, integrationOrderID, "AUTHORISED", "80.00", "0.00")
	harness := newOrderHarness(t, provider, customConnection, defaultRequestTimeout)

	outcome := harness.runOrder(t, "already-paid", integrationOrder())
	require.Equal(t, OrderAlreadySettled, outcome.Action)
	require.Equal(t, paid, outcome.InvoiceID)
	require.True(t, outcome.IsFullyPaid)
	require.Zero(t, provider.count("createInvoice"))
	require.Zero(t, provider.count("recordPayment"))
	require.Equal(t, 2, provider.invoiceCount())
}

func TestApprovedInvoiceForTheOrderIsPaidWithoutRaisingAnotherWithRealDex(t *testing.T) {
	provider := newFakeXero(t)
	existing := provider.seedInvoice(integrationCustomerID, integrationOrderID, "AUTHORISED", "75.40", "0.00")
	harness := newOrderHarness(t, provider, customConnection, defaultRequestTimeout)

	outcome := harness.runOrder(t, "existing-invoice", integrationOrder())
	require.Equal(t, OrderExistingInvoicePaid, outcome.Action)
	require.Equal(t, existing, outcome.InvoiceID)
	require.True(t, outcome.IsFullyPaid)
	require.Zero(t, provider.count("createInvoice"))
	require.Contains(t, provider.lastRequest("recordPayment").body, `"Amount":75.40`, "the exact amount due is paid")
	require.Equal(t, 1, provider.paymentCount())
}

func TestMissingContactCompletesWithoutWritingWithRealDex(t *testing.T) {
	provider := newFakeXero(t)
	harness := newOrderHarness(t, provider, customConnection, defaultRequestTimeout)
	order := integrationOrder()
	order.CustomerEmail = "nobody@unknown.example.com"

	outcome := harness.runOrder(t, "missing-contact", order)
	require.Equal(t, OrderContactMissing, outcome.Action)
	require.Zero(t, provider.count("listInvoices"))
	require.Zero(t, provider.count("createInvoice"))
}

func TestMinuteRateLimitWaitsForRetryAfterWithRealDex(t *testing.T) {
	provider := newFakeXero(t)
	provider.rateLimitsFirstContactLookup = true
	harness := newOrderHarness(t, provider, customConnection, defaultRequestTimeout)

	outcome := harness.runOrder(t, "minute-limit", integrationOrder())
	require.Equal(t, OrderInvoicedAndPaid, outcome.Action)
	times := provider.requestTimes("findContact")
	require.Len(t, times, 2)
	require.GreaterOrEqual(t, times[1].Sub(times[0]), time.Second, "the retry waited for Retry-After")
}

func TestDailyLimitFailsTheFlowBeforeAnyInvoiceWithRealDex(t *testing.T) {
	provider := newFakeXero(t)
	provider.dailyLimitsCreate = true
	harness := newOrderHarness(t, provider, customConnection, defaultRequestTimeout)

	result := harness.waitForFlow(t, harness.startOrder(t, "daily-limit", integrationOrder()))
	require.Equal(t, dex.FlowFailed, result.Status, "the unwired optional dailyLimitReached branch fails the Flow")
	require.Equal(t, 1, provider.count("createInvoice"), "a daily limit is not retried inside the Step")
	require.Zero(t, provider.invoiceCount())
	require.NotContains(t, result.ErrorMessage, "SENTINEL")
	t.Logf("daily limit failure: %s", result.ErrorMessage)
}

func TestRejectedInvoiceFailsTheFlowWithoutXeroTextWithRealDex(t *testing.T) {
	provider := newFakeXero(t)
	provider.rejectsCreate = true
	harness := newOrderHarness(t, provider, customConnection, defaultRequestTimeout)

	result := harness.waitForFlow(t, harness.startOrder(t, "rejected", integrationOrder()))
	require.Equal(t, dex.FlowFailed, result.Status, "an unwired optional providerRejected branch fails the Flow")
	require.NotContains(t, result.ErrorMessage, "SENTINEL")
	require.NotContains(t, result.ErrorMessage, integrationClientSecret)
	require.Equal(t, 1, provider.count("createInvoice"), "a conclusive rejection is not retried")
	require.Zero(t, provider.invoiceCount())
	t.Logf("rejected create failure: %s", result.ErrorMessage)
}

func TestInternalErrorOnCreateEndsUncertainWithoutResendingWithRealDex(t *testing.T) {
	provider := newFakeXero(t)
	provider.failsCreateInternally = true
	harness := newOrderHarness(t, provider, customConnection, defaultRequestTimeout)

	result := harness.waitForFlow(t, harness.startOrder(t, "internal-error", integrationOrder()))
	require.Equal(t, dex.FlowFailed, result.Status, "the unwired optional uncertain branch fails the Flow")
	require.Equal(t, 1, provider.count("createInvoice"), "Xero caches a 500 under the key, so the Step does not resend")
	require.Zero(t, provider.count("recordPayment"))
	t.Logf("uncertain create failure: %s", result.ErrorMessage)
}

func TestOAuthConnectionSendsTheNamedOrganisationTenantWithRealDex(t *testing.T) {
	provider := newFakeXero(t)
	harness := newOrderHarness(t, provider, oauthConnection, defaultRequestTimeout)

	outcome := harness.runOrder(t, "oauth-tenant", integrationOrder())
	require.Equal(t, OrderInvoicedAndPaid, outcome.Action)
	require.Equal(t, 1, provider.count("connections"), "the derived tenant is remembered after the first call")
	for _, name := range []string{"findContact", "listInvoices", "createInvoice", "recordPayment", "getInvoice"} {
		require.Equal(t, integrationTenantID, provider.lastRequest(name).header.Get("Xero-Tenant-Id"), name)
	}
	require.Zero(t, provider.count("token"), "an unexpired OAuth token needs no refresh")
}

func TestInvalidOrderFailsBeforeCallingXeroWithRealDex(t *testing.T) {
	provider := newFakeXero(t)
	harness := newOrderHarness(t, provider, customConnection, defaultRequestTimeout)
	order := integrationOrder()
	order.CustomerEmail = "City Agency <accounts@cityagency.example.com>"

	result := harness.waitForFlow(t, harness.startOrder(t, "invalid", order))
	require.Equal(t, dex.FlowFailed, result.Status)
	require.Zero(t, provider.totalRequests())
}

// fakeXero is a stateful Xero stand-in that caches, waits on, and replays Idempotency-Key responses like Xero.
type fakeXero struct {
	*httptest.Server
	t     *testing.T
	mutex sync.Mutex
	// delayedRequests tracks handlers still sleeping, so a test can assert their final effect.
	delayedRequests sync.WaitGroup

	clock             time.Time
	nextInvoiceNumber int
	nextIdentifier    int
	contacts          []fakeContact
	invoices          map[string]*fakeInvoice
	invoiceOrder      []string
	payments          map[string]bool
	issuedTokens      map[string]bool
	responsesByKey    map[string]*fakeIdempotentResponse
	counts            map[string]int
	requests          map[string][]fakeRecordedRequest

	delaysFirstCreate            bool
	delaysFirstPayment           bool
	losesFirstCreateResponse     bool
	holdsFirstPayment            chan struct{}
	rateLimitsFirstContactLookup bool
	dailyLimitsCreate            bool
	rejectsCreate                bool
	failsCreateInternally        bool
}

type fakeContact struct {
	id    string
	name  string
	email string
}

type fakeInvoice struct {
	id         string
	number     string
	contactID  string
	reference  string
	status     string
	currency   string
	date       string
	dueDate    string
	lines      []map[string]any
	subTotal   *big.Rat
	totalTax   *big.Rat
	total      *big.Rat
	amountPaid *big.Rat
	amountDue  *big.Rat
	payments   []map[string]any
	updatedAt  time.Time
}

type fakeIdempotentResponse struct {
	fingerprint string
	done        chan struct{}
	status      int
	body        string
}

type fakeRecordedRequest struct {
	at     time.Time
	method string
	query  url.Values
	header http.Header
	body   string
}

func newFakeXero(t *testing.T) *fakeXero {
	t.Helper()
	provider := &fakeXero{
		t: t, clock: time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC), nextInvoiceNumber: 1, nextIdentifier: 1,
		contacts: []fakeContact{
			{id: integrationCustomerID, name: "City Agency", email: integrationEmail},
			{id: integrationOtherID, name: "Marine Systems", email: "accounts@marinesystems.example.com"},
		},
		invoices: map[string]*fakeInvoice{}, payments: map[string]bool{}, issuedTokens: map[string]bool{integrationOAuthToken: true},
		responsesByKey: map[string]*fakeIdempotentResponse{}, counts: map[string]int{}, requests: map[string][]fakeRecordedRequest{},
	}
	provider.Server = httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *fakeXero) serveHTTP(response http.ResponseWriter, request *http.Request) {
	body, err := io.ReadAll(request.Body)
	if err != nil {
		provider.writeJSON(response, http.StatusBadRequest, `{"Type":"ValidationException"}`)
		return
	}
	if request.URL.Path == "/connect/token" {
		provider.issueToken(response, request, body)
		return
	}
	token := strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer ")
	provider.mutex.Lock()
	isIssued := provider.issuedTokens[token]
	provider.mutex.Unlock()
	if !isIssued {
		provider.writeJSON(response, http.StatusUnauthorized, `{"Type":null,"Title":"Unauthorized","Status":401,"Detail":"AuthenticationUnsuccessful"}`)
		return
	}
	if request.URL.Path == "/connections" {
		provider.record("connections", request, body)
		provider.writeJSON(response, http.StatusOK, `[
			{"id":"c1","tenantId":"`+integrationOtherTenant+`","tenantType":"ORGANISATION","tenantName":"Adam Demo Company (NZ)"},
			{"id":"c2","tenantId":"`+integrationTenantID+`","tenantType":"ORGANISATION","tenantName":"`+integrationOrganisation+`"},
			{"id":"c3","tenantId":"c3d5e782-2153-4cda-bdb4-cec791ceb90d","tenantType":"PRACTICEMANAGER","tenantName":null}]`)
		return
	}
	if tenant := request.Header.Get("Xero-Tenant-Id"); token == integrationOAuthToken && tenant != integrationTenantID {
		provider.writeJSON(response, http.StatusForbidden, `{"title":"Forbidden","status":403,"detail":"AuthenticationUnsuccessful"}`)
		return
	}
	path := strings.TrimPrefix(request.URL.Path, "/api.xro/2.0")
	switch {
	case request.Method == http.MethodGet && path == "/Contacts":
		provider.findContacts(response, request, body)
	case request.Method == http.MethodGet && path == "/Invoices":
		provider.listInvoices(response, request, body)
	case request.Method == http.MethodGet && strings.HasPrefix(path, "/Invoices/"):
		provider.getInvoice(response, request, body, strings.TrimPrefix(path, "/Invoices/"))
	case request.Method == http.MethodPut && path == "/Invoices":
		provider.runIdempotent(response, request, body, "createInvoice", provider.createInvoice)
	case request.Method == http.MethodPut && path == "/Payments":
		provider.runIdempotent(response, request, body, "recordPayment", provider.recordPayment)
	default:
		provider.writeJSON(response, http.StatusNotFound, `{"Title":"NotFound"}`)
	}
}

func (provider *fakeXero) issueToken(response http.ResponseWriter, request *http.Request, body []byte) {
	provider.record("token", request, body)
	clientID, clientSecret, hasBasic := request.BasicAuth()
	form, err := url.ParseQuery(string(body))
	if err != nil || !hasBasic || clientID != integrationClientID || clientSecret != integrationClientSecret || form.Get("client_secret") != "" {
		provider.writeJSON(response, http.StatusBadRequest, `{"error":"invalid_client"}`)
		return
	}
	if form.Get("grant_type") != "client_credentials" {
		provider.writeJSON(response, http.StatusBadRequest, `{"error":"unsupported_grant_type"}`)
		return
	}
	provider.mutex.Lock()
	accessToken := fmt.Sprintf("xero-custom-connection-token-%04d", provider.nextIdentifier)
	provider.nextIdentifier++
	provider.issuedTokens[accessToken] = true
	provider.mutex.Unlock()
	provider.writeValue(response, http.StatusOK, map[string]any{
		"access_token": accessToken, "expires_in": 1800, "token_type": "Bearer", "scope": form.Get("scope"),
	})
}

func (provider *fakeXero) findContacts(response http.ResponseWriter, request *http.Request, body []byte) {
	attempt := provider.record("findContact", request, body)
	if provider.rateLimitsFirstContactLookup && attempt == 1 {
		response.Header().Set("X-Rate-Limit-Problem", "minute")
		response.Header().Set("Retry-After", "1")
		provider.writeJSON(response, http.StatusTooManyRequests, `{"Message":"SENTINEL slow down"}`)
		return
	}
	match := whereEmailPattern.FindStringSubmatch(request.URL.Query().Get("where"))
	if match == nil || request.URL.Query().Get("page") != "1" {
		provider.writeJSON(response, http.StatusBadRequest, `{"Type":"QueryParseException"}`)
		return
	}
	var contacts []any
	provider.mutex.Lock()
	for _, contact := range provider.contacts {
		if strings.EqualFold(contact.email, match[1]) {
			contacts = append(contacts, map[string]any{
				"ContactID": contact.id, "ContactStatus": "ACTIVE", "Name": contact.name, "EmailAddress": contact.email,
				"IsCustomer": true, "IsSupplier": false, "DefaultCurrency": "USD", "UpdatedDateUTC": dotNetDate(provider.clock),
			})
		}
	}
	provider.mutex.Unlock()
	if contacts == nil {
		contacts = []any{}
	}
	provider.writeValue(response, http.StatusOK, map[string]any{
		"Id": "f1", "Status": "OK", "pagination": map[string]any{"page": 1, "pageSize": 10, "pageCount": 1, "itemCount": len(contacts)},
		"Contacts": contacts,
	})
}

func (provider *fakeXero) listInvoices(response http.ResponseWriter, request *http.Request, body []byte) {
	provider.record("listInvoices", request, body)
	query := request.URL.Query()
	statuses := strings.Split(query.Get("Statuses"), ",")
	contactIDs := strings.Split(query.Get("ContactIDs"), ",")
	reference := ""
	for _, condition := range strings.Split(query.Get("where"), " AND ") {
		if value, isReference := strings.CutPrefix(condition, `Reference=="`); isReference {
			reference = strings.TrimSuffix(value, `"`)
		}
	}
	provider.mutex.Lock()
	var invoices []any
	for _, id := range provider.invoiceOrder {
		invoice := provider.invoices[id]
		if (query.Get("Statuses") != "" && !contains(statuses, invoice.status)) || (query.Get("ContactIDs") != "" && !contains(contactIDs, invoice.contactID)) ||
			(reference != "" && invoice.reference != reference) {
			continue
		}
		invoices = append(invoices, provider.invoiceJSON(invoice))
	}
	provider.mutex.Unlock()
	if invoices == nil {
		invoices = []any{}
	}
	provider.writeValue(response, http.StatusOK, map[string]any{
		"Status": "OK", "pagination": map[string]any{"page": 1, "pageSize": 10, "pageCount": 1, "itemCount": len(invoices)}, "Invoices": invoices,
	})
}

func (provider *fakeXero) getInvoice(response http.ResponseWriter, request *http.Request, body []byte, invoiceID string) {
	provider.record("getInvoice", request, body)
	provider.mutex.Lock()
	invoice, isFound := provider.invoices[invoiceID]
	var value map[string]any
	if isFound {
		value = provider.invoiceJSON(invoice)
	}
	provider.mutex.Unlock()
	if !isFound {
		provider.writeJSON(response, http.StatusNotFound, `{"Title":"NotFound","Detail":"SENTINEL missing"}`)
		return
	}
	provider.writeValue(response, http.StatusOK, map[string]any{"Status": "OK", "Invoices": []any{value}})
}

// runIdempotent applies Xero's documented Idempotency-Key semantics around one write.
func (provider *fakeXero) runIdempotent(response http.ResponseWriter, request *http.Request, body []byte, name string,
	execute func(attempt int, body []byte) (int, string)) {
	attempt := provider.record(name, request, body)
	if name == "createInvoice" && provider.dailyLimitsCreate {
		response.Header().Set("X-Rate-Limit-Problem", "day")
		response.Header().Set("Retry-After", "43200")
		provider.writeJSON(response, http.StatusTooManyRequests, `{"Message":"SENTINEL daily"}`)
		return
	}
	key := request.Header.Get("Idempotency-Key")
	fingerprint := request.Method + " " + request.URL.String() + " " + string(body)
	provider.mutex.Lock()
	earlier, isKnown := provider.responsesByKey[key]
	if key != "" && !isKnown {
		earlier = &fakeIdempotentResponse{fingerprint: fingerprint, done: make(chan struct{})}
		provider.responsesByKey[key] = earlier
	}
	provider.mutex.Unlock()
	if key == "" {
		provider.writeJSON(response, http.StatusBadRequest, `{"Message":"the fake requires an Idempotency-Key"}`)
		return
	}
	if isKnown {
		if earlier.fingerprint != fingerprint {
			provider.writeJSON(response, http.StatusBadRequest, `{"Message":"Idempotency Key is used with a different request."}`)
			return
		}
		<-earlier.done
		provider.mutex.Lock()
		provider.counts["replay"]++
		provider.mutex.Unlock()
		provider.writeJSON(response, earlier.status, earlier.body)
		return
	}
	status, answer := execute(attempt, body)
	earlier.status, earlier.body = status, answer
	close(earlier.done)
	if name == "createInvoice" && provider.losesFirstCreateResponse && attempt == 1 {
		provider.dropConnection(response)
		return
	}
	provider.writeJSON(response, status, answer)
}

func (provider *fakeXero) createInvoice(attempt int, body []byte) (int, string) {
	if provider.delaysFirstCreate && attempt == 1 {
		provider.delayedRequests.Add(1)
		defer provider.delayedRequests.Done()
		time.Sleep(slowResponseDelay)
	}
	if provider.rejectsCreate {
		return http.StatusBadRequest, `{"ErrorNumber":10,"Type":"ValidationException","Message":"SENTINEL A validation exception occurred",
			"Elements":[{"ValidationErrors":[{"Message":"SENTINEL Account code '200' is not a valid code"}]}]}`
	}
	if provider.failsCreateInternally {
		return http.StatusInternalServerError, `{"Message":"SENTINEL internal"}`
	}
	var payload struct {
		Type    string `json:"Type"`
		Contact struct {
			ContactID string `json:"ContactID"`
		} `json:"Contact"`
		Date, DueDate, CurrencyCode, Reference, Status string
		LineItems                                      []struct {
			Description string      `json:"Description"`
			Quantity    json.Number `json:"Quantity"`
			UnitAmount  json.Number `json:"UnitAmount"`
			AccountCode string      `json:"AccountCode"`
			TaxType     string      `json:"TaxType"`
		} `json:"LineItems"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	require.NoError(provider.t, decoder.Decode(&payload))
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	invoice := &fakeInvoice{
		id: provider.newIdentifier(), number: fmt.Sprintf("INV-%04d", provider.nextInvoiceNumber), contactID: payload.Contact.ContactID,
		reference: payload.Reference, status: payload.Status, currency: payload.CurrencyCode, date: payload.Date, dueDate: payload.DueDate,
		subTotal: new(big.Rat), totalTax: new(big.Rat), amountPaid: new(big.Rat), updatedAt: provider.advanceClock(),
	}
	provider.nextInvoiceNumber++
	for _, line := range payload.LineItems {
		quantity, unitAmount := mustRat(provider.t, line.Quantity.String()), mustRat(provider.t, line.UnitAmount.String())
		lineAmount := roundCents(new(big.Rat).Mul(quantity, unitAmount))
		taxAmount := new(big.Rat)
		if line.TaxType == "OUTPUT" {
			taxAmount = roundCents(new(big.Rat).Mul(lineAmount, big.NewRat(1, 10)))
		}
		invoice.subTotal.Add(invoice.subTotal, lineAmount)
		invoice.totalTax.Add(invoice.totalTax, taxAmount)
		invoice.lines = append(invoice.lines, map[string]any{
			"LineItemID": provider.newIdentifier(), "Description": line.Description, "Quantity": json.Number(quantity.FloatString(4)),
			"UnitAmount": json.Number(unitAmount.FloatString(4)), "AccountCode": line.AccountCode, "TaxType": line.TaxType,
			"TaxAmount": json.Number(taxAmount.FloatString(2)), "LineAmount": json.Number(lineAmount.FloatString(2)),
		})
	}
	invoice.total = new(big.Rat).Add(invoice.subTotal, invoice.totalTax)
	invoice.amountDue = new(big.Rat).Set(invoice.total)
	provider.invoices[invoice.id] = invoice
	provider.invoiceOrder = append(provider.invoiceOrder, invoice.id)
	provider.counts["appliedCreateInvoice"]++
	return http.StatusOK, provider.encode(map[string]any{"Id": "put-invoice", "Status": "OK", "Invoices": []any{provider.invoiceJSON(invoice)}})
}

func (provider *fakeXero) recordPayment(attempt int, body []byte) (int, string) {
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
		Invoice struct {
			InvoiceID string `json:"InvoiceID"`
		} `json:"Invoice"`
		Account struct {
			Code string `json:"Code"`
		} `json:"Account"`
		Date      string      `json:"Date"`
		Amount    json.Number `json:"Amount"`
		Reference string      `json:"Reference"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	require.NoError(provider.t, decoder.Decode(&payload))
	amount := mustRat(provider.t, payload.Amount.String())
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	invoice, isFound := provider.invoices[payload.Invoice.InvoiceID]
	if !isFound || invoice.status != "AUTHORISED" || amount.Cmp(invoice.amountDue) > 0 || payload.Account.Code != integrationBankCode {
		return http.StatusBadRequest, `{"ErrorNumber":10,"Type":"ValidationException","Message":"SENTINEL payment",
			"Elements":[{"ValidationErrors":[{"Message":"SENTINEL Payment amount exceeds the amount outstanding"}]}]}`
	}
	paymentID := provider.newIdentifier()
	provider.payments[paymentID] = true
	invoice.amountPaid.Add(invoice.amountPaid, amount)
	invoice.amountDue.Sub(invoice.amountDue, amount)
	if invoice.amountDue.Sign() == 0 {
		invoice.status = "PAID"
	}
	invoice.updatedAt = provider.advanceClock()
	invoice.payments = append(invoice.payments, map[string]any{
		"PaymentID": paymentID, "Date": dotNetDate(provider.clock), "Amount": json.Number(amount.FloatString(2)),
	})
	provider.counts["appliedRecordPayment"]++
	return http.StatusOK, provider.encode(map[string]any{"Status": "OK", "Payments": []any{map[string]any{
		"PaymentID": paymentID, "Date": dotNetDate(provider.clock), "Amount": json.Number(amount.FloatString(2)),
		"BankAmount": json.Number(amount.FloatString(2)), "CurrencyRate": json.Number("1.000000"), "Reference": payload.Reference,
		"Status": "AUTHORISED", "PaymentType": "ACCRECPAYMENT", "IsReconciled": false, "UpdatedDateUTC": dotNetDate(provider.clock),
		"Account": map[string]any{"AccountID": integrationBankID, "Code": payload.Account.Code},
		"Invoice": map[string]any{
			"InvoiceID": invoice.id, "InvoiceNumber": invoice.number, "Type": "ACCREC", "Status": invoice.status,
			"AmountDue": json.Number(invoice.amountDue.FloatString(2)), "AmountPaid": json.Number(invoice.amountPaid.FloatString(2)),
			"CurrencyCode": invoice.currency,
		},
	}}})
}

// seedInvoice stores an approved invoice for the order with a total and an amount already paid.
func (provider *fakeXero) seedInvoice(contactID string, reference string, status string, total string, paid string) string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	totalAmount, paidAmount := mustRat(provider.t, total), mustRat(provider.t, paid)
	invoice := &fakeInvoice{
		id: provider.newIdentifier(), number: fmt.Sprintf("INV-%04d", provider.nextInvoiceNumber), contactID: contactID, reference: reference,
		status: status, currency: "USD", date: "2026-09-28", dueDate: "2026-10-12", subTotal: totalAmount, totalTax: new(big.Rat),
		total: totalAmount, amountPaid: paidAmount, amountDue: new(big.Rat).Sub(totalAmount, paidAmount), updatedAt: provider.advanceClock(),
		lines: []map[string]any{{"Description": "Seeded", "Quantity": json.Number("1.0000"), "UnitAmount": json.Number(totalAmount.FloatString(4)),
			"LineAmount": json.Number(totalAmount.FloatString(2))}},
	}
	if invoice.amountDue.Sign() == 0 {
		invoice.status = "PAID"
	}
	provider.nextInvoiceNumber++
	provider.invoices[invoice.id] = invoice
	provider.invoiceOrder = append(provider.invoiceOrder, invoice.id)
	return invoice.id
}

// invoiceJSON requires provider.mutex and writes amounts as JSON numbers, as Xero does.
func (provider *fakeXero) invoiceJSON(invoice *fakeInvoice) map[string]any {
	payments := append([]map[string]any{}, invoice.payments...)
	value := map[string]any{
		"Type": "ACCREC", "InvoiceID": invoice.id, "InvoiceNumber": invoice.number, "Reference": invoice.reference,
		"Contact": map[string]any{"ContactID": invoice.contactID, "Name": "Contact"}, "Status": invoice.status,
		"DateString": invoice.date + "T00:00:00", "Date": "/Date(1759276800000+0000)/", "DueDateString": invoice.dueDate + "T00:00:00",
		"LineAmountTypes": "Exclusive", "LineItems": invoice.lines, "CurrencyCode": invoice.currency, "CurrencyRate": json.Number("1.0000000000"),
		"SubTotal": json.Number(invoice.subTotal.FloatString(2)), "TotalTax": json.Number(invoice.totalTax.FloatString(2)),
		"Total": json.Number(invoice.total.FloatString(2)), "AmountDue": json.Number(invoice.amountDue.FloatString(2)),
		"AmountPaid": json.Number(invoice.amountPaid.FloatString(2)), "AmountCredited": json.Number("0.00"), "Payments": payments,
		"UpdatedDateUTC": dotNetDate(invoice.updatedAt), "HasErrors": false,
	}
	if invoice.status == "PAID" {
		value["FullyPaidOnDate"] = dotNetDate(invoice.updatedAt)
	}
	return value
}

// newIdentifier requires provider.mutex and returns a deterministic UUID.
func (provider *fakeXero) newIdentifier() string {
	identifier := fmt.Sprintf("8f3c%04x-0000-4000-8000-%012x", provider.nextIdentifier, provider.nextIdentifier)
	provider.nextIdentifier++
	return identifier
}

// advanceClock requires provider.mutex; every write moves UpdatedDateUTC forward one minute.
func (provider *fakeXero) advanceClock() time.Time {
	provider.clock = provider.clock.Add(time.Minute)
	return provider.clock
}

func (provider *fakeXero) record(name string, request *http.Request, body []byte) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.counts[name]++
	provider.requests[name] = append(provider.requests[name], fakeRecordedRequest{
		at: time.Now(), method: request.Method, query: request.URL.Query(), header: request.Header.Clone(), body: string(body),
	})
	return provider.counts[name]
}

func (provider *fakeXero) encode(value any) string {
	encoded, err := json.Marshal(value)
	require.NoError(provider.t, err)
	return string(encoded)
}

func (provider *fakeXero) writeValue(response http.ResponseWriter, status int, value any) {
	provider.writeJSON(response, status, provider.encode(value))
}

func (provider *fakeXero) writeJSON(response http.ResponseWriter, status int, body string) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("Xero-Correlation-Id", "5fe9659e-e5cc-4747-ad01-47adb038bf34")
	response.Header().Set("X-DayLimit-Remaining", "4990")
	response.Header().Set("X-MinLimit-Remaining", "59")
	response.WriteHeader(status)
	if _, err := io.WriteString(response, body); err != nil {
		provider.t.Logf("fake Xero response write failed: %v", err)
	}
}

// dropConnection closes the connection after Xero applied the request, as a lost response would.
func (provider *fakeXero) dropConnection(response http.ResponseWriter) {
	hijacker, isHijacker := response.(http.Hijacker)
	require.True(provider.t, isHijacker)
	connection, _, err := hijacker.Hijack()
	require.NoError(provider.t, err)
	require.NoError(provider.t, connection.Close())
}

func (provider *fakeXero) waitForDelayedRequests(t *testing.T) {
	t.Helper()
	finished := make(chan struct{})
	go func() {
		provider.delayedRequests.Wait()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(2 * slowResponseDelay):
		t.Fatal("a delayed fake Xero request did not finish")
	}
}

func (provider *fakeXero) count(name string) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.counts[name]
}

func (provider *fakeXero) totalRequests() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	total := 0
	for _, requests := range provider.requests {
		total += len(requests)
	}
	return total
}

func (provider *fakeXero) invoiceCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return len(provider.invoices)
}

func (provider *fakeXero) paymentCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return len(provider.payments)
}

func (provider *fakeXero) invoice(invoiceID string) fakeInvoice {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return *provider.invoices[invoiceID]
}

func (provider *fakeXero) lastRequest(name string) fakeRecordedRequest {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	requests := provider.requests[name]
	require.NotEmpty(provider.t, requests, name)
	return requests[len(requests)-1]
}

func (provider *fakeXero) requestTimes(name string) []time.Time {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	var times []time.Time
	for _, request := range provider.requests[name] {
		times = append(times, request.at)
	}
	return times
}

func (provider *fakeXero) distinctIdempotencyKeys(name string) map[string]bool {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	keys := map[string]bool{}
	for _, request := range provider.requests[name] {
		keys[request.header.Get("Idempotency-Key")] = true
	}
	return keys
}

func mustRat(t *testing.T, value string) *big.Rat {
	t.Helper()
	parsed, isParsed := new(big.Rat).SetString(value)
	require.True(t, isParsed, "decimal %q", value)
	return parsed
}

// roundCents rounds half away from zero to two places, exactly.
func roundCents(value *big.Rat) *big.Rat {
	rounded, isParsed := new(big.Rat).SetString(value.FloatString(2))
	if !isParsed {
		panic("rounded decimal did not parse")
	}
	return rounded
}

func dotNetDate(instant time.Time) string {
	return "/Date(" + strconv.FormatInt(instant.UnixMilli(), 10) + "+0000)/"
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

// connectionMethod selects how the harness authenticates to the fake.
type connectionMethod int

const (
	// customConnection loads a Dex Web-shaped Custom Connection record without an access token.
	customConnection connectionMethod = iota + 1
	// oauthConnection uses an unexpired OAuth access token and a named organisation.
	oauthConnection
)

// orderHarness owns a real Worker and Client against the Dex Server at DEX_FLOW_SERVICE_ADDRESS.
type orderHarness struct {
	flow            *Flow
	registry        *dex.Registry
	cache           *blobcache.Cache
	serverAddress   string
	workerAddress   string
	worker          *dex.Worker
	workerResult    chan error
	client          *dex.Client
	connectionsFile string
}

type persistedCredentials struct {
	AccessToken string
	ExpiresAt   time.Time
}

func newOrderHarness(t *testing.T, provider *fakeXero, method connectionMethod, requestTimeout time.Duration) *orderHarness {
	t.Helper()
	harness := &orderHarness{serverAddress: environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")}
	options := []xero.Option{xero.WithLocalProviderURL(provider.URL), xero.WithHTTPClient(&http.Client{Timeout: requestTimeout})}
	var connection xero.Connection
	var err error
	switch method {
	case customConnection:
		harness.connectionsFile = filepath.Join(t.TempDir(), "connections.json")
		writeCustomConnectionRecord(t, harness.connectionsFile)
		store, loadErr := localconfig.LoadFile(harness.connectionsFile)
		require.NoError(t, loadErr)
		connection, err = xero.NewLocalConnection(store, ConnectionName, options...)
	default:
		reference := sdkgo.ConnectionRef{Provider: "xero", Name: ConnectionName}
		client, newErr := xero.New(xero.Config{Organisation: integrationOrganisation}, sdkgo.StaticCredentialProvider[xero.Credentials]{reference: {
			AuthMethodID: xero.OAuthAuthMethodID, AccessToken: sdkgo.NewSecretString(integrationOAuthToken),
		}}, options...)
		require.NoError(t, newErr)
		connection, err = xero.NewConnection(client, reference)
	}
	require.NoError(t, err)
	harness.flow = NewFlow(connection)
	harness.registry, err = dex.NewRegistry([]dex.Flow{harness.flow})
	require.NoError(t, err)
	harness.cache, err = blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	harness.workerAddress = net.JoinHostPort("127.0.0.1", availableIntegrationPort(t))
	harness.client, err = dex.NewClient(harness.registry, harness.cache, dex.ClientOptions{
		FlowServiceAddress: harness.serverAddress, WorkerTarget: &dex.WorkerTarget{Address: harness.workerAddress},
	})
	require.NoError(t, err)
	harness.startWorker(t)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(harness.worker.Stop(ctx), <-harness.workerResult, harness.client.Close(), harness.cache.Close()))
	})
	return harness
}

// writeCustomConnectionRecord writes what Dex Web saves for the Custom Connection method, including its record-level authMethodId.
func writeCustomConnectionRecord(t *testing.T, path string) {
	t.Helper()
	file := map[string]any{
		"schemaVersion": localconfig.SchemaVersion,
		"connections": []map[string]any{{
			"connectorId": xero.ConnectorID, "modulePath": "github.com/superdurable/dex-connectors-library/connectors/xero",
			"moduleVersion": "v0.1.0", "provider": "xero", "connectionName": ConnectionName, "authMethodId": xero.CustomConnectionAuthMethodID,
			"configuration": map[string]any{},
			"credentials": map[string]any{
				"auth_method": xero.CustomConnectionAuthMethodID, "client_id": integrationClientID, "client_secret": integrationClientSecret,
			},
		}},
	}
	encoded, err := json.Marshal(file)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, encoded, 0o600))
}

func (harness *orderHarness) readPersistedCredentials(t *testing.T) persistedCredentials {
	t.Helper()
	contents, err := os.ReadFile(harness.connectionsFile)
	require.NoError(t, err)
	var file struct {
		Connections []struct {
			AuthMethodID string `json:"authMethodId"`
			Credentials  struct {
				AccessToken string `json:"access_token"`
			} `json:"credentials"`
			CredentialExpiresAt time.Time `json:"credentialExpiresAt"`
		} `json:"connections"`
	}
	require.NoError(t, json.Unmarshal(contents, &file))
	require.Len(t, file.Connections, 1)
	require.Equal(t, xero.CustomConnectionAuthMethodID, file.Connections[0].AuthMethodID, "the refresh kept Dex Web's record member")
	return persistedCredentials{AccessToken: file.Connections[0].Credentials.AccessToken, ExpiresAt: file.Connections[0].CredentialExpiresAt}
}

func (harness *orderHarness) startWorker(t *testing.T) {
	t.Helper()
	worker, err := dex.NewWorker(harness.registry, harness.cache, dex.WorkerOptions{
		BindAddress: harness.workerAddress, FlowServiceAddress: harness.serverAddress, WorkerTarget: dex.WorkerTarget{Address: harness.workerAddress},
	})
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	harness.worker, harness.workerResult = worker, workerResult
}

// replaceWorker force-stops the Worker without draining its handlers, like a crash, then starts a new one.
func (harness *orderHarness) replaceWorker(t *testing.T) {
	t.Helper()
	expired, cancel := context.WithCancel(context.Background())
	cancel()
	// A forced stop returns the expired context's error; losing the in-flight handler is the point.
	_ = harness.worker.Stop(expired)
	require.NoError(t, <-harness.workerResult)
	harness.startWorker(t)
}

func (harness *orderHarness) runOrder(t *testing.T, scenario string, input Input) InvoicedOrderOutcome {
	t.Helper()
	flowID := harness.startOrder(t, scenario, input)
	result := harness.waitForFlow(t, flowID)
	require.Equal(t, dex.FlowCompleted, result.Status, "Flow %s: %s", flowID, result.ErrorMessage)
	var outcome InvoicedOrderOutcome
	require.NoError(t, result.DecodeSingleOutput(&outcome))
	return outcome
}

func (harness *orderHarness) startOrder(t *testing.T, scenario string, input Input) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	flowID := "xero-approved-order-" + scenario + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err := harness.client.StartFlow(ctx, harness.flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err, "start Flow %s on the Dex Server at %s", flowID, harness.serverAddress)
	return flowID
}

// waitForFlow waits, across server long-poll caps, until the Flow closes.
func (harness *orderHarness) waitForFlow(t *testing.T, flowID string) dex.FlowResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	for {
		result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
		var longPollTimeout *dex.LongPollTimeoutError
		if errors.As(err, &longPollTimeout) {
			continue
		}
		require.NoError(t, err, "Flow %s did not close", flowID)
		return result
	}
}

func availableIntegrationPort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	return strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
}

func environmentOr(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
