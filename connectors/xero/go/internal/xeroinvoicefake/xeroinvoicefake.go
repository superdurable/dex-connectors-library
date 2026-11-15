// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package xeroinvoicefake serves one Xero organisation's GET /Invoices list and GET /Invoices/{id} read
// over HTTP for invoiceChanged tests. It honours If-Modified-Since to the second, the Statuses filter, a
// Type where filter, order by UpdatedDateUTC, page and pageSize, and answers with DateTimeUTC and a
// pagination object, as Xero's Accounting API does.
package xeroinvoicefake

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	invoicesPath        = "/api.xro/2.0/Invoices"
	modifiedSinceLayout = "2006-01-02T15:04:05"
	contactID           = "025867f1-d741-4d6b-b1af-9ac774b59ba7"
	// defaultChangeStep spaces changes by more than a second, so most changes fall in distinct seconds.
	defaultChangeStep = 1250 * time.Millisecond
)

// BaseTime is the fake organisation's clock at start.
var BaseTime = time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)

// Request is one recorded request.
type Request struct {
	// Path is the request path.
	Path string
	// Query is the request query string.
	Query url.Values
	// ModifiedSince is the If-Modified-Since header, or blank.
	ModifiedSince string
	// Status is the HTTP status the fake answered.
	Status int
}

// Organisation is a stateful single-organisation fake that is safe for concurrent use.
type Organisation struct {
	*httptest.Server
	mutex          sync.Mutex
	now            time.Time
	invoices       map[string]*storedInvoice
	rateLimited    int
	rateLimitKind  string
	retryAfter     string
	failures       int
	failureStatus  int
	requests       []Request
	maximumPerPage int
}

type storedInvoice struct {
	id        string
	number    string
	kind      string
	status    string
	total     string
	updatedAt time.Time
}

// New starts a fake whose clock is BaseTime; the test cleanup closes it.
func New(t *testing.T) *Organisation {
	t.Helper()
	fake := &Organisation{now: BaseTime, invoices: map[string]*storedInvoice{}, maximumPerPage: 100}
	fake.Server = httptest.NewServer(http.HandlerFunc(fake.serveHTTP))
	t.Cleanup(fake.Close)
	return fake
}

// Upsert creates or changes an invoice and stamps it with the clock advanced by a little over a second.
// invoiceType is ACCREC or ACCPAY, and total a decimal string such as 49.95.
func (fake *Organisation) Upsert(invoiceID string, invoiceNumber string, invoiceType string, status string, total string) time.Time {
	return fake.UpsertAfter(defaultChangeStep, invoiceID, invoiceNumber, invoiceType, status, total)
}

// UpsertAfter is Upsert with an explicit clock step; a zero step stamps the same instant as the last change.
func (fake *Organisation) UpsertAfter(step time.Duration, invoiceID string, invoiceNumber string, invoiceType string, status string, total string) time.Time {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.now = fake.now.Add(step)
	fake.invoices[strings.ToLower(invoiceID)] = &storedInvoice{
		id: strings.ToLower(invoiceID), number: invoiceNumber, kind: invoiceType, status: status, total: total, updatedAt: fake.now,
	}
	return fake.now
}

// SetStatus changes an existing invoice's status and stamps it like Upsert.
func (fake *Organisation) SetStatus(invoiceID string, status string) time.Time {
	fake.mutex.Lock()
	stored := *fake.invoices[strings.ToLower(invoiceID)]
	fake.mutex.Unlock()
	return fake.Upsert(stored.id, stored.number, stored.kind, status, stored.total)
}

// AdvanceClock moves the organisation's clock without changing an invoice.
func (fake *Organisation) AdvanceClock(step time.Duration) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.now = fake.now.Add(step)
}

// RateLimitNext answers the next count list requests with 429, X-Rate-Limit-Problem problem (minute,
// day, or concurrent), and Retry-After retryAfter seconds.
func (fake *Organisation) RateLimitNext(count int, problem string, retryAfter string) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.rateLimited, fake.rateLimitKind, fake.retryAfter = count, problem, retryAfter
}

// FailNext answers the next count list requests with status, such as 500 or 403.
func (fake *Organisation) FailNext(count int, status int) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.failures, fake.failureStatus = count, status
}

// LimitPageSize makes the fake answer at most pageSize invoices per page, so a test can page through a
// few invoices instead of creating Xero's 100-invoice pages.
func (fake *Organisation) LimitPageSize(pageSize int) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.maximumPerPage = pageSize
}

// Requests returns a copy of every recorded request.
func (fake *Organisation) Requests() []Request {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	return append([]Request(nil), fake.requests...)
}

func (fake *Organisation) serveHTTP(response http.ResponseWriter, request *http.Request) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	recorded := Request{Path: request.URL.Path, Query: request.URL.Query(), ModifiedSince: request.Header.Get("If-Modified-Since")}
	status, body := fake.answer(request, recorded, response.Header())
	recorded.Status = status
	fake.requests = append(fake.requests, recorded)
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	_, _ = response.Write(body) // A failed write is the client's lost response, which the test observes.
}

func (fake *Organisation) answer(request *http.Request, recorded Request, header http.Header) (int, []byte) {
	switch {
	case request.Method != http.MethodGet || !strings.HasPrefix(request.Header.Get("Authorization"), "Bearer "):
		return http.StatusUnauthorized, []byte(`{"Type":"AuthenticationUnsuccessful"}`)
	case request.URL.Path == invoicesPath:
		if fake.rateLimited > 0 {
			fake.rateLimited--
			header.Set("X-Rate-Limit-Problem", fake.rateLimitKind)
			header.Set("Retry-After", fake.retryAfter)
			return http.StatusTooManyRequests, []byte(`{"Type":"RateLimitExceeded"}`)
		}
		if fake.failures > 0 {
			fake.failures--
			return fake.failureStatus, []byte(`{"Type":"InjectedFailure"}`)
		}
		return fake.listInvoices(recorded)
	case strings.HasPrefix(request.URL.Path, invoicesPath+"/"):
		stored, ok := fake.invoices[strings.ToLower(strings.TrimPrefix(request.URL.Path, invoicesPath+"/"))]
		if !ok {
			return http.StatusNotFound, []byte(`{"Type":"NotFound"}`)
		}
		return fake.encode(map[string]any{"Invoices": []any{invoiceJSON(stored)}})
	default:
		return http.StatusNotFound, []byte(`{"Type":"NotFound"}`)
	}
}

func (fake *Organisation) listInvoices(recorded Request) (int, []byte) {
	var since time.Time
	if recorded.ModifiedSince != "" {
		parsed, err := time.Parse(modifiedSinceLayout, recorded.ModifiedSince)
		if err != nil {
			return http.StatusBadRequest, []byte(`{"Type":"ValidationException"}`)
		}
		since = parsed
	}
	statuses := map[string]bool{}
	for _, status := range strings.Split(recorded.Query.Get("Statuses"), ",") {
		if status != "" {
			statuses[status] = true
		}
	}
	invoiceType := ""
	if where := recorded.Query.Get("where"); where != "" {
		if !strings.HasPrefix(where, `Type=="`) || !strings.HasSuffix(where, `"`) {
			return http.StatusBadRequest, []byte(`{"Type":"ValidationException"}`)
		}
		invoiceType = strings.TrimSuffix(strings.TrimPrefix(where, `Type=="`), `"`)
	}
	var matching []*storedInvoice
	for _, stored := range fake.invoices {
		// Xero compares If-Modified-Since to the second, inclusively.
		if stored.updatedAt.Truncate(time.Second).Before(since) || (len(statuses) > 0 && !statuses[stored.status]) ||
			(invoiceType != "" && stored.kind != invoiceType) {
			continue
		}
		matching = append(matching, stored)
	}
	isDescending := strings.HasSuffix(recorded.Query.Get("order"), " DESC")
	sort.Slice(matching, func(i, j int) bool {
		if !matching[i].updatedAt.Equal(matching[j].updatedAt) {
			return matching[i].updatedAt.Before(matching[j].updatedAt) != isDescending
		}
		return matching[i].id < matching[j].id
	})
	page, pageSize := atLeastOne(recorded.Query.Get("page")), atLeastOne(recorded.Query.Get("pageSize"))
	pageSize = min(pageSize, fake.maximumPerPage)
	start := min((page-1)*pageSize, len(matching))
	end := min(start+pageSize, len(matching))
	elements := make([]any, 0, end-start)
	for _, stored := range matching[start:end] {
		elements = append(elements, invoiceJSON(stored))
	}
	return fake.encode(map[string]any{
		"DateTimeUTC": dotNetDate(fake.now),
		"pagination":  map[string]any{"page": page, "pageSize": pageSize, "pageCount": (len(matching) + pageSize - 1) / pageSize, "itemCount": len(matching)},
		"Invoices":    elements,
	})
}

func (fake *Organisation) encode(value any) (int, []byte) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return http.StatusInternalServerError, []byte(`{}`)
	}
	return http.StatusOK, encoded
}

func invoiceJSON(stored *storedInvoice) map[string]any {
	amountDue, amountPaid := stored.total, "0.00"
	if stored.status == "PAID" {
		amountDue, amountPaid = "0.00", stored.total
	}
	return map[string]any{
		"InvoiceID": stored.id, "InvoiceNumber": stored.number, "Type": stored.kind, "Status": stored.status,
		"Contact":    map[string]any{"ContactID": contactID, "Name": "City Agency"},
		"DateString": "2026-10-01T00:00:00", "CurrencyCode": "USD", "LineAmountTypes": "NoTax",
		"LineItems": []any{map[string]any{"Description": "Spring bouquet", "Quantity": json.Number("1.0000"),
			"UnitAmount": json.Number(stored.total), "AccountCode": "200", "LineAmount": json.Number(stored.total)}},
		"SubTotal": json.Number(stored.total), "Total": json.Number(stored.total),
		"AmountDue": json.Number(amountDue), "AmountPaid": json.Number(amountPaid),
		"UpdatedDateUTC": dotNetDate(stored.updatedAt),
	}
}

func dotNetDate(instant time.Time) string {
	return fmt.Sprintf("/Date(%d+0000)/", instant.UnixMilli())
}

func atLeastOne(value string) int {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		return 1
	}
	return parsed
}
