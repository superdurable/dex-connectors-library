//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package paidorder

import (
	"context"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/intuit/quickbooks"
	"github.com/superdurable/dex-connectors-library/connectors/intuit/quickbooks/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	integrationClientID     = "ABquickbooksIntegrationClient0123456789"
	integrationClientSecret = "quickbooksIntegrationClientSecret0123"
	integrationRealmID      = "9341453050298464"
	integrationOrderID      = "ORDER-1042"
	integrationEmail        = "accounts@cityagency.example.com"
	integrationDisplayName  = "City Agency"

	defaultRequestTimeout = 5 * time.Second
	// slowResponseDelay outlasts Dex's roughly seven-second async local phase, so an async Step is dispatched again.
	slowResponseDelay  = 9 * time.Second
	slowRequestTimeout = 20 * time.Second
)

func integrationOrder() Input {
	return Input{
		OrderID: integrationOrderID, CustomerEmail: integrationEmail, CustomerDisplayName: integrationDisplayName,
		CustomerCompanyName: "City Agency LLC", InvoiceDate: "2026-10-01", DueDate: "2026-10-15", PaidOn: "2026-10-01",
		DepositAccountID: integrationBankAccountID, PaymentReference: "ch_3QxF2a",
		Lines: []OrderLine{
			{ItemID: "1", Description: "Spring bouquet", Quantity: "2", UnitPrice: "49.95", TaxCodeID: "NON"},
			{ItemID: "2", Description: "Delivery", Quantity: "1", UnitPrice: "12.5"},
		},
	}
}

func TestPaidOrderCreatesCustomerInvoicePaymentAndReceiptWithRealDex(t *testing.T) {
	provider := newFakeQuickBooks(t)
	harness := newOrderHarness(t, provider, expiredAccessToken, defaultRequestTimeout)

	outcome := harness.runOrder(t, "new-order", integrationOrder())
	require.Equal(t, OrderInvoicedAndPaid, outcome.Action)
	require.True(t, outcome.IsCustomerCreated, "no customer had the email, so the Flow created one")
	require.True(t, outcome.IsFullyPaid)
	require.Equal(t, integrationOrderID, outcome.DocNumber)
	require.Equal(t, quickbooks.Decimal("112.40"), outcome.TotalAmount, "2 x 49.95 + 1 x 12.50, exactly")
	require.Equal(t, quickbooks.Decimal("0"), outcome.Balance)
	require.Equal(t, quickbooks.EmailStatusEmailSent, outcome.EmailStatus)
	require.NotEmpty(t, outcome.PaymentID)
	require.Equal(t, 1, provider.customerCount())
	require.Equal(t, 1, provider.invoiceCount())
	require.Equal(t, 1, provider.paymentCount())
	require.Equal(t, 1, provider.count("appliedSendInvoice"))

	require.Equal(t, "select * from Customer where PrimaryEmailAddr = 'accounts@cityagency.example.com' MAXRESULTS 10",
		provider.lastRequest("findCustomer").query.Get("query"))
	require.JSONEq(t, `{"DisplayName":"City Agency","PrimaryEmailAddr":{"Address":"accounts@cityagency.example.com"},"CompanyName":"City Agency LLC"}`,
		provider.lastRequest("createCustomer").body)
	require.Equal(t, "select * from Invoice where CustomerRef = '"+outcome.CustomerID+"' AND DocNumber = 'ORDER-1042' ORDERBY MetaData.CreateTime STARTPOSITION 1 MAXRESULTS 10",
		provider.lastRequest("listInvoices").query.Get("query"))
	create := provider.lastRequest("createInvoice")
	require.JSONEq(t, `{"CustomerRef":{"value":"`+outcome.CustomerID+`"},"TxnDate":"2026-10-01","DueDate":"2026-10-15","DocNumber":"ORDER-1042",
		"BillEmail":{"Address":"accounts@cityagency.example.com"},"PrivateNote":"Order ORDER-1042","Line":[
		{"DetailType":"SalesItemLineDetail","Amount":99.90,"Description":"Spring bouquet","SalesItemLineDetail":{"ItemRef":{"value":"1"},"Qty":2,"UnitPrice":49.95,"TaxCodeRef":{"value":"NON"}}},
		{"DetailType":"SalesItemLineDetail","Amount":12.50,"Description":"Delivery","SalesItemLineDetail":{"ItemRef":{"value":"2"},"Qty":1,"UnitPrice":12.5}}]}`, create.body)
	require.Contains(t, create.body, `"Amount":99.90`, "amounts travel as exact JSON number literals")
	payment := provider.lastRequest("recordPayment")
	require.JSONEq(t, `{"CustomerRef":{"value":"`+outcome.CustomerID+`"},"TotalAmt":112.40,"TxnDate":"2026-10-01","PaymentRefNum":"ch_3QxF2a",
		"DepositToAccountRef":{"value":"`+integrationBankAccountID+`"},"Line":[{"Amount":112.40,"LinkedTxn":[{"TxnId":"`+outcome.InvoiceID+`","TxnType":"Invoice"}]}]}`, payment.body)
	send := provider.lastRequest("sendInvoice")
	require.Equal(t, integrationEmail, send.query.Get("sendTo"))
	require.Equal(t, "application/octet-stream", send.header.Get("Content-Type"))

	requestIDs := map[string]bool{}
	for _, name := range []string{"createCustomer", "createInvoice", "recordPayment", "sendInvoice"} {
		requestID := provider.lastRequest(name).query.Get("requestid")
		require.Len(t, requestID, 36, "%s: every write carries the Step's requestid", name)
		require.False(t, requestIDs[requestID], "%s: each Step execution has its own requestid", name)
		requestIDs[requestID] = true
	}
	for _, name := range []string{"findCustomer", "createCustomer", "listInvoices", "createInvoice", "recordPayment", "sendInvoice", "getInvoice"} {
		request := provider.lastRequest(name)
		require.Equal(t, quickbooks.MinorVersion, request.query.Get("minorversion"), name)
		require.Equal(t, integrationRealmID, request.realmID, "%s: the realm comes from the ID token", name)
	}
	require.Empty(t, provider.lastRequest("getInvoice").query.Get("requestid"), "reads carry no requestid")

	require.Equal(t, 1, provider.count("token"), "one refresh serves every call for an hour")
	token := provider.lastRequest("token")
	require.Equal(t, "Basic "+base64.StdEncoding.EncodeToString([]byte(integrationClientID+":"+integrationClientSecret)), token.header.Get("Authorization"))
	form, err := url.ParseQuery(token.body)
	require.NoError(t, err)
	require.Equal(t, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {"intuit-refresh-token-0"}}, form)
	stored, expiresAt := harness.credentials.Current()
	require.Equal(t, "intuit-refresh-token-1", stored.RefreshToken.Reveal(), "the rotated refresh token was stored")
	require.NotEmpty(t, stored.IDToken.Reveal(), "the ID token from consent is kept")
	require.NotNil(t, expiresAt)
	require.True(t, expiresAt.After(time.Now().Add(50*time.Minute)))
}

func TestExistingCustomerIsFoundByEmailWithRealDex(t *testing.T) {
	provider := newFakeQuickBooks(t)
	existing := provider.seedCustomer(integrationDisplayName, integrationEmail, true)
	provider.seedCustomer("City Agency (old)", integrationEmail, false)
	provider.seedCustomer("City Agency Australia", "accounts@cityagency.example.com.au", true)
	harness := newOrderHarness(t, provider, unexpiredAccessToken, defaultRequestTimeout)

	outcome := harness.runOrder(t, "existing-customer", integrationOrder())
	require.Equal(t, OrderInvoicedAndPaid, outcome.Action)
	require.Equal(t, existing, outcome.CustomerID, "the inactive and look-alike customers were not chosen")
	require.False(t, outcome.IsCustomerCreated)
	require.Zero(t, provider.count("createCustomer"))
	require.Zero(t, provider.count("token"), "an unexpired access token needs no refresh")
}

// Duplicate dispatch: the repeat gets Fault 600 while the first request runs, then the replay.
func TestSlowInvoiceCreateIsDispatchedAgainAndCreatesOneInvoiceWithRealDex(t *testing.T) {
	provider := newFakeQuickBooks(t)
	provider.delaysFirstCreateInvoice = true
	harness := newOrderHarness(t, provider, unexpiredAccessToken, slowRequestTimeout)

	startedAt := time.Now()
	outcome := harness.runOrder(t, "slow-create", integrationOrder())
	require.GreaterOrEqual(t, time.Since(startedAt), slowResponseDelay)
	require.Equal(t, OrderInvoicedAndPaid, outcome.Action)
	provider.waitForDelayedRequests(t)
	require.GreaterOrEqual(t, provider.count("createInvoice"), 2, "Dex dispatched the create again past its local phase")
	require.Equal(t, 1, provider.count("appliedCreateInvoice"), "QuickBooks ran the request once")
	require.Equal(t, 1, provider.invoiceCount(), "the repeated dispatch did not create a second invoice")
	require.Len(t, provider.distinctRequestIDs("createInvoice"), 1, "every attempt sent the same requestid")
	require.Equal(t, 1, provider.paymentCount())
	t.Logf("slow create: requests=%d duplicates=%d replays=%d", provider.count("createInvoice"), provider.count("duplicateRequestID"), provider.count("replay"))
}

// TestSlowPaymentIsDispatchedAgainAndRecordsOnePaymentWithRealDex is a duplicate-dispatch test for recordPayment.
func TestSlowPaymentIsDispatchedAgainAndRecordsOnePaymentWithRealDex(t *testing.T) {
	provider := newFakeQuickBooks(t)
	provider.delaysFirstPayment = true
	harness := newOrderHarness(t, provider, unexpiredAccessToken, slowRequestTimeout)

	outcome := harness.runOrder(t, "slow-payment", integrationOrder())
	require.Equal(t, OrderInvoicedAndPaid, outcome.Action)
	require.True(t, outcome.IsFullyPaid)
	provider.waitForDelayedRequests(t)
	require.GreaterOrEqual(t, provider.count("recordPayment"), 2, "Dex dispatched the payment again past its local phase")
	require.Equal(t, 1, provider.count("appliedRecordPayment"))
	require.Equal(t, 1, provider.paymentCount(), "the repeated dispatch did not pay the invoice twice")
	require.Len(t, provider.distinctRequestIDs("recordPayment"), 1)
	require.Equal(t, "0.00", provider.invoice(outcome.InvoiceID).balance.FloatString(2))
	t.Logf("slow payment: requests=%d duplicates=%d replays=%d", provider.count("recordPayment"), provider.count("duplicateRequestID"), provider.count("replay"))
}

func TestLostInvoiceResponseIsReplayedUnderTheSameRequestIDWithRealDex(t *testing.T) {
	provider := newFakeQuickBooks(t)
	provider.losesFirstCreateInvoiceResponse = true
	harness := newOrderHarness(t, provider, unexpiredAccessToken, defaultRequestTimeout)

	outcome := harness.runOrder(t, "lost-create", integrationOrder())
	require.Equal(t, OrderInvoicedAndPaid, outcome.Action)
	require.Equal(t, 2, provider.count("createInvoice"))
	require.Equal(t, 1, provider.count("replay"), "the retry received QuickBooks's original response")
	require.Equal(t, 1, provider.invoiceCount())
	require.Len(t, provider.distinctRequestIDs("createInvoice"), 1)
}

// TestLostWorkerDuringPaymentRecordsOnePaymentWithRealDex shows the requestid surviving a Worker lost mid-payment.
func TestLostWorkerDuringPaymentRecordsOnePaymentWithRealDex(t *testing.T) {
	provider := newFakeQuickBooks(t)
	provider.holdsFirstPayment = make(chan struct{})
	harness := newOrderHarness(t, provider, unexpiredAccessToken, slowRequestTimeout)
	flowID := harness.startOrder(t, "lost-worker", integrationOrder())
	require.Eventually(t, func() bool { return provider.count("recordPayment") == 1 }, 30*time.Second, 50*time.Millisecond,
		"the first payment attempt must reach QuickBooks")
	harness.replaceWorker(t)
	require.Eventually(t, func() bool { return provider.count("recordPayment") >= 2 }, time.Minute, 50*time.Millisecond,
		"an attempt on the new Worker must reach QuickBooks")
	close(provider.holdsFirstPayment)

	result := harness.waitForFlow(t, flowID)
	require.Equal(t, dex.FlowCompleted, result.Status, "Flow %s: %s", flowID, result.ErrorMessage)
	provider.waitForDelayedRequests(t)
	require.Equal(t, 1, provider.paymentCount(), "the attempt on the new Worker did not pay twice")
	require.Len(t, provider.distinctRequestIDs("recordPayment"), 1, "the requestid survives Worker replacement")
}

func TestPaidInvoiceForTheOrderIsLeftAloneWithRealDex(t *testing.T) {
	provider := newFakeQuickBooks(t)
	customerID := provider.seedCustomer(integrationDisplayName, integrationEmail, true)
	paid := provider.seedInvoice(customerID, "order-1042", "80.00", "0")
	harness := newOrderHarness(t, provider, unexpiredAccessToken, defaultRequestTimeout)

	outcome := harness.runOrder(t, "already-paid", integrationOrder())
	require.Equal(t, OrderAlreadySettled, outcome.Action)
	require.Equal(t, paid, outcome.InvoiceID, "QuickBooks matches the invoice number ignoring case")
	require.True(t, outcome.IsFullyPaid)
	for _, name := range []string{"createCustomer", "createInvoice", "recordPayment", "sendInvoice"} {
		require.Zero(t, provider.count(name), name)
	}
}

func TestOpenInvoiceForTheOrderIsPaidWithoutRaisingAnotherWithRealDex(t *testing.T) {
	provider := newFakeQuickBooks(t)
	customerID := provider.seedCustomer(integrationDisplayName, integrationEmail, true)
	otherCustomerID := provider.seedCustomer("Marine Systems", "accounts@marinesystems.example.com", true)
	provider.seedInvoice(otherCustomerID, integrationOrderID, "500.00", "500.00")
	existing := provider.seedInvoice(customerID, integrationOrderID, "75.40", "75.40")
	harness := newOrderHarness(t, provider, unexpiredAccessToken, defaultRequestTimeout)

	outcome := harness.runOrder(t, "open-invoice", integrationOrder())
	require.Equal(t, OrderExistingInvoicePaid, outcome.Action)
	require.Equal(t, existing, outcome.InvoiceID, "another customer's invoice with the same number is ignored")
	require.True(t, outcome.IsFullyPaid)
	require.Zero(t, provider.count("createInvoice"))
	require.Contains(t, provider.lastRequest("recordPayment").body, `"TotalAmt":75.40`, "the exact balance is paid")
	require.Equal(t, 1, provider.paymentCount())
	require.Equal(t, "500.00", provider.invoice(provider.invoiceIDs()[0]).balance.FloatString(2), "the decoy was not touched")
}

func TestSharedEmailFailsTheFlowAsAmbiguousWithRealDex(t *testing.T) {
	provider := newFakeQuickBooks(t)
	provider.seedCustomer(integrationDisplayName, integrationEmail, true)
	provider.seedCustomer("City Agency Events", "Accounts@CityAgency.example.com", true)
	harness := newOrderHarness(t, provider, unexpiredAccessToken, defaultRequestTimeout)

	result := harness.waitForFlow(t, harness.startOrder(t, "ambiguous", integrationOrder()))
	require.Equal(t, dex.FlowFailed, result.Status, "the unwired optional ambiguous branch fails the Flow")
	require.Equal(t, 1, provider.count("findCustomer"))
	require.Zero(t, provider.count("createCustomer"))
	require.Zero(t, provider.count("listInvoices"))
}

func TestDisplayNameInUseFailsTheFlowWithoutQuickBooksTextWithRealDex(t *testing.T) {
	provider := newFakeQuickBooks(t)
	provider.seedVendor(integrationDisplayName)
	harness := newOrderHarness(t, provider, unexpiredAccessToken, defaultRequestTimeout)

	result := harness.waitForFlow(t, harness.startOrder(t, "name-conflict", integrationOrder()))
	require.Equal(t, dex.FlowFailed, result.Status, "the unwired optional nameConflict branch fails the Flow")
	require.Equal(t, 1, provider.count("createCustomer"), "a conclusive rejection is not retried")
	require.Zero(t, provider.customerCount())
	require.NotContains(t, result.ErrorMessage, "SENTINEL")
	t.Logf("name conflict failure: %s", result.ErrorMessage)
}

func TestThrottledLookupWaitsForRetryAfterWithRealDex(t *testing.T) {
	provider := newFakeQuickBooks(t)
	provider.throttlesFirstCustomerLookup = true
	harness := newOrderHarness(t, provider, unexpiredAccessToken, defaultRequestTimeout)

	outcome := harness.runOrder(t, "throttled", integrationOrder())
	require.Equal(t, OrderInvoicedAndPaid, outcome.Action)
	times := provider.requestTimes("findCustomer")
	require.Len(t, times, 2)
	require.GreaterOrEqual(t, times[1].Sub(times[0]), time.Second, "the retry waited for Retry-After")
}

func TestRejectedInvoiceFailsTheFlowAfterOneRequestWithRealDex(t *testing.T) {
	provider := newFakeQuickBooks(t)
	provider.rejectsCreateInvoice = true
	harness := newOrderHarness(t, provider, unexpiredAccessToken, defaultRequestTimeout)

	result := harness.waitForFlow(t, harness.startOrder(t, "rejected", integrationOrder()))
	require.Equal(t, dex.FlowFailed, result.Status, "an unwired optional providerRejected branch fails the Flow")
	require.NotContains(t, result.ErrorMessage, "SENTINEL")
	require.NotContains(t, result.ErrorMessage, integrationClientSecret)
	require.Equal(t, 1, provider.count("createInvoice"), "a conclusive rejection is not retried")
	require.Zero(t, provider.invoiceCount())
	require.Zero(t, provider.count("recordPayment"))
	t.Logf("rejected create failure: %s", result.ErrorMessage)
}

func TestRevokedRefreshTokenFailsTheFlowWithoutRetryingWithRealDex(t *testing.T) {
	provider := newFakeQuickBooks(t)
	provider.revokesRefreshToken = true
	harness := newOrderHarness(t, provider, expiredAccessToken, defaultRequestTimeout)

	result := harness.waitForFlow(t, harness.startOrder(t, "revoked", integrationOrder()))
	require.Equal(t, dex.FlowFailed, result.Status, "providerRejected, unwired here, reports that authorization must be renewed")
	require.Equal(t, 1, provider.count("token"), "invalid_grant is terminal, so the refresh is not repeated")
	require.Zero(t, provider.count("findCustomer"))
	require.True(t, harness.credentials.IsReauthorizationRequired())
	require.NotContains(t, result.ErrorMessage, "intuit-refresh-token")
}

func TestInvalidOrderFailsBeforeCallingQuickBooksWithRealDex(t *testing.T) {
	provider := newFakeQuickBooks(t)
	harness := newOrderHarness(t, provider, expiredAccessToken, defaultRequestTimeout)
	order := integrationOrder()
	order.OrderID = "ORDER-1042-WITH-A-VERY-LONG-SUFFIX"

	result := harness.waitForFlow(t, harness.startOrder(t, "invalid", order))
	require.Equal(t, dex.FlowFailed, result.Status)
	require.Zero(t, provider.totalRequests())
}

// accessTokenState selects the stored access token the harness starts from.
type accessTokenState int

const (
	// expiredAccessToken stores tokens whose access token expired, as after an idle hour.
	expiredAccessToken accessTokenState = iota + 1
	// unexpiredAccessToken stores tokens fresh from consent.
	unexpiredAccessToken
)

// orderHarness owns a real Worker and Client against the Dex Server at DEX_FLOW_SERVICE_ADDRESS.
type orderHarness struct {
	flow          *Flow
	registry      *dex.Registry
	cache         *blobcache.Cache
	serverAddress string
	workerAddress string
	worker        *dex.Worker
	workerResult  chan error
	client        *dex.Client
	credentials   *testsupport.RefreshingCredentialSource[quickbooks.Credentials]
}

func newOrderHarness(t *testing.T, provider *fakeQuickBooks, tokenState accessTokenState, requestTimeout time.Duration) *orderHarness {
	t.Helper()
	harness := &orderHarness{serverAddress: environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")}
	expiresAt := time.Now().Add(-time.Minute)
	if tokenState == unexpiredAccessToken {
		expiresAt = time.Now().Add(time.Hour)
	}
	harness.credentials = testsupport.NewRefreshingCredentialSource(quickbooks.Credentials{
		ClientID: integrationClientID, ClientSecret: sdkgo.NewSecretString(integrationClientSecret),
		AccessToken: sdkgo.NewSecretString(provider.consentAccessToken()), RefreshToken: sdkgo.NewSecretString("intuit-refresh-token-0"),
		IDToken: sdkgo.NewSecretString(integrationIDToken(t)),
	}, &expiresAt)
	client, err := quickbooks.New(quickbooks.Config{Environment: quickbooks.EnvironmentSandbox}, harness.credentials,
		quickbooks.WithLocalProviderURL(provider.URL), quickbooks.WithHTTPClient(&http.Client{Timeout: requestTimeout}))
	require.NoError(t, err)
	connection, err := quickbooks.NewConnection(client, sdkgo.ConnectionRef{Provider: "quickbooks", Name: ConnectionName})
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

// integrationIDToken is an unsigned Intuit-shaped ID token naming the integration company.
func integrationIDToken(t *testing.T) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"r4p5SbL2qaFehFzhj8gI"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"8b9f5a10-4c7e-4c3f-9f1e-2f9d3c4b5a69","aud":["` + integrationClientID +
		`"],"realmid":"` + integrationRealmID + `","auth_time":1759309200,"iss":"https://oauth.platform.intuit.com/op/v1","exp":1759312800,"iat":1759309200}`))
	return header + "." + payload + ".c2lnbmF0dXJl"
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

func (harness *orderHarness) runOrder(t *testing.T, scenario string, input Input) PaidOrderOutcome {
	t.Helper()
	flowID := harness.startOrder(t, scenario, input)
	result := harness.waitForFlow(t, flowID)
	require.Equal(t, dex.FlowCompleted, result.Status, "Flow %s: %s", flowID, result.ErrorMessage)
	var outcome PaidOrderOutcome
	require.NoError(t, result.DecodeSingleOutput(&outcome))
	return outcome
}

func (harness *orderHarness) startOrder(t *testing.T, scenario string, input Input) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	flowID := "quickbooks-paid-order-" + scenario + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
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
