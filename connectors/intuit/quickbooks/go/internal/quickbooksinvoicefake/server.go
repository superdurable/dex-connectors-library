// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package quickbooksinvoicefake

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"time"
)

// serve answers GET /v3/company/{realm}/query and GET /v3/company/{realm}/invoice/{id}.
func (fake *Invoices) serve(response http.ResponseWriter, request *http.Request) {
	match := companyPathPattern.FindStringSubmatch(request.URL.Path)
	switch {
	case request.Header.Get("Authorization") == "":
		writeFault(response, http.StatusUnauthorized, "AuthenticationFault", "3200")
	case request.Method != http.MethodGet || match == nil:
		writeFault(response, http.StatusBadRequest, "ValidationFault", "4000")
	case match[1] != fake.realmID:
		writeFault(response, http.StatusForbidden, "AuthorizationFault", "3100")
	case match[3] != "":
		fake.serveInvoice(response, match[3])
	default:
		fake.serveQuery(response, request.URL.Query().Get("query"))
	}
}

func (fake *Invoices) serveInvoice(response http.ResponseWriter, id string) {
	fake.mutex.Lock()
	found, isPresent := fake.invoices[id]
	var body map[string]any
	if isPresent {
		body = map[string]any{"Invoice": found.wire(), "time": fake.clock.Format(time.RFC3339)}
	}
	fake.mutex.Unlock()
	if !isPresent {
		writeFault(response, http.StatusBadRequest, "ValidationFault", "610")
		return
	}
	writeBody(response, http.StatusOK, body)
}

func (fake *Invoices) serveQuery(response http.ResponseWriter, statement string) {
	fake.mutex.Lock()
	fake.queries = append(fake.queries, statement)
	if fake.throttles > 0 {
		fake.throttles--
		retryAfter := fake.retryAfter
		fake.mutex.Unlock()
		if retryAfter != "" {
			response.Header().Set("Retry-After", retryAfter)
		}
		writeFault(response, http.StatusTooManyRequests, "ThrottleExceeded", "003001")
		return
	}
	match := queryPattern.FindStringSubmatch(statement)
	if match == nil || (fake.rejectsWatermarks && match[1] != "") {
		fake.mutex.Unlock()
		writeFault(response, http.StatusBadRequest, "ValidationFault", "4000")
		return
	}
	page, err := fake.queryLocked(match)
	fake.mutex.Unlock()
	if err != nil {
		writeFault(response, http.StatusBadRequest, "ValidationFault", "4000")
		return
	}
	queryResponse := map[string]any{"startPosition": match[3], "maxResults": len(page)}
	if len(page) != 0 {
		queryResponse["Invoice"] = page
	}
	writeBody(response, http.StatusOK, map[string]any{"QueryResponse": queryResponse, "time": time.Now().In(companyZone).Format(time.RFC3339)})
}

// queryLocked orders by LastUpdatedTime, then Id, which keeps ties in a stable order as QuickBooks does.
func (fake *Invoices) queryLocked(match []string) ([]map[string]any, error) {
	var since time.Time
	if match[1] != "" {
		parsed, err := time.Parse(time.RFC3339, match[1])
		if err != nil {
			return nil, err
		}
		since = parsed
	}
	isDescending := match[2] != ""
	startPosition, _ := strconv.Atoi(match[3]) // The pattern admits only digits.
	maxResults, _ := strconv.Atoi(match[4])
	var selected []*invoice
	for _, candidate := range fake.invoices {
		if candidate.updatedAt.Before(since) {
			continue
		}
		selected = append(selected, candidate)
	}
	sort.Slice(selected, func(i, j int) bool {
		if !selected[i].updatedAt.Equal(selected[j].updatedAt) {
			return selected[i].updatedAt.Before(selected[j].updatedAt) != isDescending
		}
		left, _ := strconv.Atoi(selected[i].id)
		right, _ := strconv.Atoi(selected[j].id)
		return left < right
	})
	page := []map[string]any{}
	for index := startPosition - 1; index >= 0 && index < len(selected) && len(page) < maxResults; index++ {
		page = append(page, selected[index].wire())
	}
	return page, nil
}

func (stored *invoice) wire() map[string]any {
	return map[string]any{
		"Id": stored.id, "SyncToken": strconv.Itoa(stored.syncToken), "DocNumber": stored.docNumber, "TxnDate": stored.createdAt.Format(time.DateOnly),
		"CustomerRef": map[string]any{"value": stored.customerID},
		"Line": []any{
			map[string]any{"Id": "1", "Amount": json.Number(stored.amount), "DetailType": "SalesItemLineDetail",
				"SalesItemLineDetail": map[string]any{"ItemRef": map[string]any{"value": "1"}, "Qty": json.Number("1"), "UnitPrice": json.Number(stored.amount)}},
			map[string]any{"Amount": json.Number(stored.amount), "DetailType": "SubTotalLineDetail", "SubTotalLineDetail": map[string]any{}},
		},
		"TotalAmt": json.Number(stored.amount), "Balance": json.Number(stored.balance), "EmailStatus": "NotSet",
		"MetaData": map[string]any{"CreateTime": stored.createdAt.Format(time.RFC3339), "LastUpdatedTime": stored.updatedAt.Format(time.RFC3339)},
	}
}

func writeFault(response http.ResponseWriter, status int, faultType string, code string) {
	writeBody(response, status, map[string]any{"Fault": map[string]any{
		"type": faultType, "Error": []any{map[string]any{"code": code, "Message": "fake QuickBooks fault"}},
	}})
}

func writeBody(response http.ResponseWriter, status int, body any) {
	encoded, err := json.Marshal(body)
	if err != nil {
		http.Error(response, "fake QuickBooks could not encode its response", http.StatusInternalServerError)
		return
	}
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	// A write error means the client went away; the test sees the failed request.
	_, _ = response.Write(encoded)
}
