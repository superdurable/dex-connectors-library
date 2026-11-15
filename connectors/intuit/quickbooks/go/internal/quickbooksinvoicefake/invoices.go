// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package quickbooksinvoicefake is an in-process stand-in for the QuickBooks Online invoice query and read
// endpoints the invoiceChanged Trigger and its example use. It keeps a company clock, gives every write a
// new SyncToken and LastUpdatedTime, answers the watermark query in LastUpdatedTime order, and can throttle.
package quickbooksinvoicefake

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"sync"
	"testing"
	"time"
)

// companyZone is the fake company's UTC offset; QuickBooks writes MetaData times in company time.
var companyZone = time.FixedZone("PDT", -7*60*60)

var (
	companyPathPattern = regexp.MustCompile(`^/v3/company/([0-9]+)/(query|invoice/([0-9]+))$`)
	queryPattern       = regexp.MustCompile(`^select \* from Invoice(?: where MetaData\.LastUpdatedTime >= '([^']+)')? ORDERBY MetaData\.LastUpdatedTime( DESC)? STARTPOSITION ([0-9]+) MAXRESULTS ([0-9]+)$`)
)

// Invoices is one fake company. It is safe for concurrent use by the server and the test.
type Invoices struct {
	*httptest.Server
	mutex             sync.Mutex
	realmID           string
	clock             time.Time
	nextID            int
	invoices          map[string]*invoice
	throttles         int
	retryAfter        string
	queries           []string
	rejectsWatermarks bool
	isClockHeld       bool
}

type invoice struct {
	id, docNumber, customerID, amount, balance string
	syncToken                                  int
	createdAt, updatedAt                       time.Time
}

// New starts a fake company with realmID; the server closes when the test ends.
func New(t *testing.T, realmID string) *Invoices {
	t.Helper()
	fake := &Invoices{
		realmID: realmID, clock: time.Date(2026, 11, 2, 9, 0, 0, 0, companyZone), nextID: 130, invoices: map[string]*invoice{},
	}
	fake.Server = httptest.NewServer(http.HandlerFunc(fake.serve))
	t.Cleanup(fake.Close)
	return fake
}

// Create adds an invoice at SyncToken 0, one company second after the previous write, and returns its Id.
func (fake *Invoices) Create(docNumber string, customerID string, amount string) string {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.nextID++
	now := fake.tick()
	created := &invoice{id: strconv.Itoa(fake.nextID), docNumber: docNumber, customerID: customerID, amount: amount, balance: amount, createdAt: now, updatedAt: now}
	fake.invoices[created.id] = created
	return created.id
}

// CreateBackdated adds an invoice whose LastUpdatedTime is age before the company clock, as a write that
// QuickBooks stamped before an earlier poll but made visible only after it.
func (fake *Invoices) CreateBackdated(docNumber string, customerID string, amount string, age time.Duration) string {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.nextID++
	stamped := fake.clock.Add(-age)
	created := &invoice{id: strconv.Itoa(fake.nextID), docNumber: docNumber, customerID: customerID, amount: amount, balance: amount, createdAt: stamped, updatedAt: stamped}
	fake.invoices[created.id] = created
	return created.id
}

// Pay zeroes the balance of invoice id as an applied payment does, bumping its SyncToken.
func (fake *Invoices) Pay(id string) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	changed := fake.invoices[id]
	changed.balance, changed.syncToken, changed.updatedAt = "0", changed.syncToken+1, fake.tick()
}

// HoldClock makes later writes share the current company second until ReleaseClock, as a burst does.
func (fake *Invoices) HoldClock() {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.clock = fake.clock.Add(time.Second)
	fake.isClockHeld = true
}

// ReleaseClock lets every later write advance the company clock by one second again.
func (fake *Invoices) ReleaseClock() {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.isClockHeld = false
}

// Throttle makes the next count queries answer HTTP 429 ThrottleExceeded with Retry-After retryAfter
// seconds; blank sends no Retry-After header.
func (fake *Invoices) Throttle(count int, retryAfter string) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.throttles, fake.retryAfter = count, retryAfter
}

// RejectWatermarkQueries makes every later query with a LastUpdatedTime condition answer HTTP 400 with a
// QuickBooks validation Fault, while the baseline query still succeeds.
func (fake *Invoices) RejectWatermarkQueries(rejects bool) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.rejectsWatermarks = rejects
}

// Queries returns every query statement received, in order.
func (fake *Invoices) Queries() []string {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	return append([]string(nil), fake.queries...)
}

// tick returns the company time of the next write.
func (fake *Invoices) tick() time.Time {
	if !fake.isClockHeld {
		fake.clock = fake.clock.Add(time.Second)
	}
	return fake.clock
}
