// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package chargebee

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListSubscriptionsInput selects one page of subscriptions.
type ListSubscriptionsInput struct {
	// Limit is 1 to 100; zero uses the provider default.
	Limit int `json:"limit,omitempty"`
	// Offset is Chargebee's opaque next_offset; blank begins at the first page.
	Offset string `json:"offset,omitempty"`
}

// SubscriptionPage contains selected subscription status and a pagination token.
type SubscriptionPage struct {
	// Subscriptions contains this page's selected subscriptions.
	Subscriptions []SubscriptionStatus `json:"subscriptions"`
	// NextOffset is passed to the next request; blank means no more pages.
	NextOffset string `json:"nextOffset,omitempty"`
}

// GetCustomerInput selects one customer by ID.
type GetCustomerInput struct {
	// CustomerID is the ID shown in Chargebee Customers or returned by the API.
	CustomerID string `json:"customerId"`
}

// CustomerIdentity excludes cards, balances, and billing addresses.
type CustomerIdentity struct {
	// ID is the Chargebee customer ID.
	ID string `json:"id"`
	// Email is the customer's email when configured; it may be blank.
	Email string `json:"email,omitempty"`
}

// ListCustomersInput selects one page of customers.
type ListCustomersInput struct {
	// Limit is 1 to 100; zero uses the provider default.
	Limit int `json:"limit,omitempty"`
	// Offset is Chargebee's opaque next_offset; blank begins at the first page.
	Offset string `json:"offset,omitempty"`
}

// CustomerPage contains customer identities and a pagination token.
type CustomerPage struct {
	// Customers contains this page's customers without billing data.
	Customers []CustomerIdentity `json:"customers"`
	// NextOffset is passed to the next request; blank means no more pages.
	NextOffset string `json:"nextOffset,omitempty"`
}

// GetInvoiceInput selects one invoice by ID.
type GetInvoiceInput struct {
	// InvoiceID is the ID shown in Chargebee Invoices or returned by the API.
	InvoiceID string `json:"invoiceId"`
}

// InvoiceStatus contains selected invoice fields without payment details.
type InvoiceStatus struct {
	// ID is the Chargebee invoice ID.
	ID string `json:"id"`
	// Status is the provider state, such as paid or payment_due.
	Status string `json:"status"`
	// CurrencyCode is the invoice's three-letter ISO 4217 currency.
	CurrencyCode string `json:"currencyCode"`
	// Total is the invoice total in the currency's minor unit.
	Total int64 `json:"total"`
}

// ListInvoicesInput selects one page of invoices.
type ListInvoicesInput struct {
	// Limit is 1 to 100; zero uses the provider default.
	Limit int `json:"limit,omitempty"`
	// Offset is Chargebee's opaque next_offset; blank begins at the first page.
	Offset string `json:"offset,omitempty"`
}

// InvoicePage contains selected invoice status and a pagination token.
type InvoicePage struct {
	// Invoices contains this page's selected invoices.
	Invoices []InvoiceStatus `json:"invoices"`
	// NextOffset is passed to the next request; blank means no more pages.
	NextOffset string `json:"nextOffset,omitempty"`
}

// GetEventInput selects one recent Chargebee event by ID.
type GetEventInput struct {
	// EventID is the ID shown in Chargebee Events or returned by listEvents.
	EventID string `json:"eventId"`
}

// ListEventsInput selects one page of Chargebee events.
type ListEventsInput struct {
	// Limit is 1 to 100; zero uses the provider default.
	Limit int `json:"limit,omitempty"`
	// Offset is Chargebee's opaque next_offset; blank begins at the first page.
	Offset string `json:"offset,omitempty"`
}

// EventSummary excludes event content, user identity, and payment details.
type EventSummary struct {
	// ID is the Chargebee event ID.
	ID string `json:"id"`
	// EventType identifies the business event.
	EventType string `json:"eventType"`
	// OccurredAt is the event time in Unix seconds.
	OccurredAt int64 `json:"occurredAt"`
}

// EventPage contains selected event metadata and a pagination token.
type EventPage struct {
	// Events contains this page's event summaries.
	Events []EventSummary `json:"events"`
	// NextOffset is passed to the next request; blank means no more pages.
	NextOffset string `json:"nextOffset,omitempty"`
}

// ListSubscriptionsOperation reads one subscription page.
type ListSubscriptionsOperation struct{ client *Client }

// GetCustomerOperation reads one customer identity.
type GetCustomerOperation struct{ client *Client }

// ListCustomersOperation reads one customer page.
type ListCustomersOperation struct{ client *Client }

// GetInvoiceOperation reads one invoice status.
type GetInvoiceOperation struct{ client *Client }

// ListInvoicesOperation reads one invoice page.
type ListInvoicesOperation struct{ client *Client }

// GetEventOperation reads one event summary.
type GetEventOperation struct{ client *Client }

// ListEventsOperation reads one event page.
type ListEventsOperation struct{ client *Client }

// ListSubscriptions binds subscription pagination to this client.
func (client *Client) ListSubscriptions() ListSubscriptionsOperation {
	return ListSubscriptionsOperation{client}
}

// GetCustomer binds customer lookup to this client.
func (client *Client) GetCustomer() GetCustomerOperation { return GetCustomerOperation{client} }

// ListCustomers binds customer pagination to this client.
func (client *Client) ListCustomers() ListCustomersOperation { return ListCustomersOperation{client} }

// GetInvoice binds invoice lookup to this client.
func (client *Client) GetInvoice() GetInvoiceOperation { return GetInvoiceOperation{client} }

// ListInvoices binds invoice pagination to this client.
func (client *Client) ListInvoices() ListInvoicesOperation { return ListInvoicesOperation{client} }

// GetEvent binds event lookup to this client.
func (client *Client) GetEvent() GetEventOperation { return GetEventOperation{client} }

// ListEvents binds event pagination to this client.
func (client *Client) ListEvents() ListEventsOperation { return ListEventsOperation{client} }

// Definition returns the generated subscription page contract.
func (ListSubscriptionsOperation) Definition() sdkgo.QueryDefinition {
	return ListSubscriptionsDefinition
}

// Definition returns the generated customer lookup contract.
func (GetCustomerOperation) Definition() sdkgo.QueryDefinition { return GetCustomerDefinition }

// Definition returns the generated customer page contract.
func (ListCustomersOperation) Definition() sdkgo.QueryDefinition { return ListCustomersDefinition }

// Definition returns the generated invoice lookup contract.
func (GetInvoiceOperation) Definition() sdkgo.QueryDefinition { return GetInvoiceDefinition }

// Definition returns the generated invoice page contract.
func (ListInvoicesOperation) Definition() sdkgo.QueryDefinition { return ListInvoicesDefinition }

// Definition returns the generated event lookup contract.
func (GetEventOperation) Definition() sdkgo.QueryDefinition { return GetEventDefinition }

// Definition returns the generated event page contract.
func (ListEventsOperation) Definition() sdkgo.QueryDefinition { return ListEventsDefinition }

// Invoke reads one page of subscription status.
func (operation ListSubscriptionsOperation) Invoke(call sdkgo.Call, input ListSubscriptionsInput) sdkgo.QueryAttempt[SubscriptionPage] {
	const name = "listSubscriptions"
	path, err := chargebeePagePath("/subscriptions", input.Limit, input.Offset)
	if err != nil {
		return chargebeeQueryBranch(name, "defect", SubscriptionPage{}, sdkgo.FailureValidation, "page limit or offset is invalid")
	}
	return invokeChargebeeQuery(call, operation.client, name, path, "listed", "", func(body []byte) (SubscriptionPage, error) {
		var wire struct {
			List []struct {
				Subscription json.RawMessage `json:"subscription"`
			} `json:"list"`
			NextOffset string `json:"next_offset"`
		}
		if err := json.Unmarshal(body, &wire); err != nil || wire.List == nil {
			return SubscriptionPage{}, errors.New("subscription list missing")
		}
		rows := make([]SubscriptionStatus, 0, len(wire.List))
		for _, row := range wire.List {
			wrapped, err := json.Marshal(map[string]json.RawMessage{"subscription": row.Subscription})
			if err != nil {
				return SubscriptionPage{}, err
			}
			item, err := decodeSubscription(wrapped, "")
			if err != nil {
				return SubscriptionPage{}, err
			}
			rows = append(rows, item)
		}
		return SubscriptionPage{Subscriptions: rows, NextOffset: wire.NextOffset}, nil
	})
}

// Invoke reads a customer identity without cards or balances.
func (operation GetCustomerOperation) Invoke(call sdkgo.Call, input GetCustomerInput) sdkgo.QueryAttempt[CustomerIdentity] {
	const name = "getCustomer"
	if !validSubscriptionID(input.CustomerID) {
		return chargebeeQueryBranch(name, "defect", CustomerIdentity{}, sdkgo.FailureValidation, "customer ID is invalid")
	}
	return invokeChargebeeQuery(call, operation.client, name, "/customers/"+url.PathEscape(input.CustomerID), "found", "notFound", func(body []byte) (CustomerIdentity, error) {
		var wire struct {
			Customer struct {
				ID    string `json:"id"`
				Email string `json:"email"`
			} `json:"customer"`
		}
		if err := json.Unmarshal(body, &wire); err != nil || wire.Customer.ID != input.CustomerID {
			return CustomerIdentity{}, errors.New("customer identity mismatch")
		}
		return CustomerIdentity{ID: wire.Customer.ID, Email: wire.Customer.Email}, nil
	})
}

// Invoke reads one page of customer identities.
func (operation ListCustomersOperation) Invoke(call sdkgo.Call, input ListCustomersInput) sdkgo.QueryAttempt[CustomerPage] {
	const name = "listCustomers"
	path, err := chargebeePagePath("/customers", input.Limit, input.Offset)
	if err != nil {
		return chargebeeQueryBranch(name, "defect", CustomerPage{}, sdkgo.FailureValidation, "page limit or offset is invalid")
	}
	return invokeChargebeeQuery(call, operation.client, name, path, "listed", "", func(body []byte) (CustomerPage, error) {
		var wire struct {
			List []struct {
				Customer struct {
					ID    string `json:"id"`
					Email string `json:"email"`
				} `json:"customer"`
			} `json:"list"`
			NextOffset string `json:"next_offset"`
		}
		if err := json.Unmarshal(body, &wire); err != nil || wire.List == nil {
			return CustomerPage{}, errors.New("customer list missing")
		}
		rows := make([]CustomerIdentity, 0, len(wire.List))
		for _, row := range wire.List {
			if row.Customer.ID == "" {
				return CustomerPage{}, errors.New("customer ID missing")
			}
			rows = append(rows, CustomerIdentity{ID: row.Customer.ID, Email: row.Customer.Email})
		}
		return CustomerPage{Customers: rows, NextOffset: wire.NextOffset}, nil
	})
}

// Invoke reads invoice status and total in minor currency units.
func (operation GetInvoiceOperation) Invoke(call sdkgo.Call, input GetInvoiceInput) sdkgo.QueryAttempt[InvoiceStatus] {
	const name = "getInvoice"
	if !validSubscriptionID(input.InvoiceID) {
		return chargebeeQueryBranch(name, "defect", InvoiceStatus{}, sdkgo.FailureValidation, "invoice ID is invalid")
	}
	return invokeChargebeeQuery(call, operation.client, name, "/invoices/"+url.PathEscape(input.InvoiceID), "found", "notFound", func(body []byte) (InvoiceStatus, error) {
		var wire struct {
			Invoice json.RawMessage `json:"invoice"`
		}
		if err := json.Unmarshal(body, &wire); err != nil {
			return InvoiceStatus{}, err
		}
		return decodeChargebeeInvoice(wire.Invoice, input.InvoiceID)
	})
}

// Invoke reads one page of invoice status and totals.
func (operation ListInvoicesOperation) Invoke(call sdkgo.Call, input ListInvoicesInput) sdkgo.QueryAttempt[InvoicePage] {
	const name = "listInvoices"
	path, err := chargebeePagePath("/invoices", input.Limit, input.Offset)
	if err != nil {
		return chargebeeQueryBranch(name, "defect", InvoicePage{}, sdkgo.FailureValidation, "page limit or offset is invalid")
	}
	return invokeChargebeeQuery(call, operation.client, name, path, "listed", "", func(body []byte) (InvoicePage, error) {
		var wire struct {
			List []struct {
				Invoice json.RawMessage `json:"invoice"`
			} `json:"list"`
			NextOffset string `json:"next_offset"`
		}
		if err := json.Unmarshal(body, &wire); err != nil || wire.List == nil {
			return InvoicePage{}, errors.New("invoice list missing")
		}
		rows := make([]InvoiceStatus, 0, len(wire.List))
		for _, row := range wire.List {
			item, err := decodeChargebeeInvoice(row.Invoice, "")
			if err != nil {
				return InvoicePage{}, err
			}
			rows = append(rows, item)
		}
		return InvoicePage{Invoices: rows, NextOffset: wire.NextOffset}, nil
	})
}

// Invoke reads recent event metadata without exposing event content.
func (operation GetEventOperation) Invoke(call sdkgo.Call, input GetEventInput) sdkgo.QueryAttempt[EventSummary] {
	const name = "getEvent"
	if !validSubscriptionID(input.EventID) {
		return chargebeeQueryBranch(name, "defect", EventSummary{}, sdkgo.FailureValidation, "event ID is invalid")
	}
	return invokeChargebeeQuery(call, operation.client, name, "/events/"+url.PathEscape(input.EventID), "found", "notFound", func(body []byte) (EventSummary, error) {
		var wire struct {
			Event json.RawMessage `json:"event"`
		}
		if err := json.Unmarshal(body, &wire); err != nil {
			return EventSummary{}, err
		}
		return decodeChargebeeEvent(wire.Event, input.EventID)
	})
}

// Invoke reads one page of event metadata without exposing event content.
func (operation ListEventsOperation) Invoke(call sdkgo.Call, input ListEventsInput) sdkgo.QueryAttempt[EventPage] {
	const name = "listEvents"
	path, err := chargebeePagePath("/events", input.Limit, input.Offset)
	if err != nil {
		return chargebeeQueryBranch(name, "defect", EventPage{}, sdkgo.FailureValidation, "page limit or offset is invalid")
	}
	return invokeChargebeeQuery(call, operation.client, name, path, "listed", "", func(body []byte) (EventPage, error) {
		var wire struct {
			List []struct {
				Event json.RawMessage `json:"event"`
			} `json:"list"`
			NextOffset string `json:"next_offset"`
		}
		if err := json.Unmarshal(body, &wire); err != nil || wire.List == nil {
			return EventPage{}, errors.New("event list missing")
		}
		rows := make([]EventSummary, 0, len(wire.List))
		for _, row := range wire.List {
			item, err := decodeChargebeeEvent(row.Event, "")
			if err != nil {
				return EventPage{}, err
			}
			rows = append(rows, item)
		}
		return EventPage{Events: rows, NextOffset: wire.NextOffset}, nil
	})
}

func chargebeePagePath(resource string, limit int, offset string) (string, error) {
	if limit < 0 || limit > 100 || len(offset) > 1000 || strings.ContainsAny(offset, "\r\n") {
		return "", errors.New("invalid page input")
	}
	query := url.Values{}
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}
	if offset != "" {
		query.Set("offset", offset)
	}
	if len(query) == 0 {
		return resource, nil
	}
	return resource + "?" + query.Encode(), nil
}

func decodeChargebeeInvoice(body []byte, requestedID string) (InvoiceStatus, error) {
	var wire struct {
		ID           string `json:"id"`
		Status       string `json:"status"`
		CurrencyCode string `json:"currency_code"`
		Total        *int64 `json:"total"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return InvoiceStatus{}, err
	}
	if wire.ID == "" || requestedID != "" && wire.ID != requestedID || wire.Status == "" || len(wire.CurrencyCode) != 3 || wire.Total == nil {
		return InvoiceStatus{}, errors.New("invoice fields missing")
	}
	return InvoiceStatus{ID: wire.ID, Status: wire.Status, CurrencyCode: wire.CurrencyCode, Total: *wire.Total}, nil
}

func decodeChargebeeEvent(body []byte, requestedID string) (EventSummary, error) {
	var wire struct {
		ID         string `json:"id"`
		EventType  string `json:"event_type"`
		OccurredAt *int64 `json:"occurred_at"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return EventSummary{}, err
	}
	if wire.ID == "" || requestedID != "" && wire.ID != requestedID || wire.EventType == "" || wire.OccurredAt == nil || *wire.OccurredAt <= 0 {
		return EventSummary{}, errors.New("event fields missing")
	}
	return EventSummary{ID: wire.ID, EventType: wire.EventType, OccurredAt: *wire.OccurredAt}, nil
}

func invokeChargebeeQuery[T any](call sdkgo.Call, client *Client, name, path string, success, notFound sdkgo.BranchID, decode func([]byte) (T, error)) sdkgo.QueryAttempt[T] {
	var zero T
	response := client.get(call, path)
	switch {
	case errors.Is(response.err, errCredential):
		return chargebeeQueryBranch(name, "defect", zero, sdkgo.FailureAuthentication, "Chargebee credentials are unavailable or invalid")
	case errors.Is(response.err, errRequest):
		return sdkgo.NewQueryRetry[T](chargebeeFailure(name, sdkgo.FailureTransport, "Chargebee request failed"), 0)
	case response.status == http.StatusTooManyRequests:
		return sdkgo.NewQueryRetry[T](chargebeeFailure(name, sdkgo.FailureRateLimit, "Chargebee rate limited the request"), response.retryAfter)
	case response.status == http.StatusRequestTimeout || response.status >= 500:
		return sdkgo.NewQueryRetry[T](chargebeeFailure(name, sdkgo.FailureAvailability, "Chargebee is temporarily unavailable"), response.retryAfter)
	case response.status == http.StatusNotFound && notFound != "":
		return chargebeeQueryBranch(name, notFound, zero, sdkgo.FailureNotFound, "Chargebee did not find the resource")
	case response.status != http.StatusOK:
		return chargebeeQueryBranch(name, "providerRejected", zero, sdkgo.FailureProviderRejection, "Chargebee rejected HTTP "+strconv.Itoa(response.status))
	case errors.Is(response.err, providerhttp.ErrBodyTooLarge):
		return chargebeeQueryBranch(name, "invalidResponse", zero, sdkgo.FailureResponseTooLarge, "Chargebee response exceeded the configured limit")
	case response.err != nil:
		return sdkgo.NewQueryRetry[T](chargebeeFailure(name, sdkgo.FailureTransport, "Chargebee response was interrupted"), 0)
	}
	value, err := decode(response.body)
	if err != nil {
		return chargebeeQueryBranch(name, "invalidResponse", zero, sdkgo.FailureProtocol, "Chargebee returned invalid JSON")
	}
	return sdkgo.NewQueryBranch(success, value, nil, sdkgo.Receipt{})
}

func chargebeeQueryBranch[T any](name string, branch sdkgo.BranchID, value T, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	failure := chargebeeFailure(name, kind, message)
	return sdkgo.NewQueryBranch(branch, value, &failure, sdkgo.Receipt{})
}
func chargebeeFailure(name string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "chargebee", Operation: name, Message: message}
}
