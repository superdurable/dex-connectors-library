// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package okki

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListCustomerFieldsInput selects the fixed OKKI company field dictionary.
type ListCustomerFieldsInput struct{}

// CustomerField is one field definition without default values or customer data.
type CustomerField struct {
	// ID is the tenant's stable field identifier.
	ID string `json:"id"`
	// Name is the tenant's field label.
	Name string `json:"name"`
	// FieldType is OKKI's documented numeric field type.
	FieldType int `json:"fieldType"`
}

// CustomerFields contains a bounded set of customer field definitions.
type CustomerFields struct {
	// Fields are returned in provider order.
	Fields []CustomerField `json:"fields"`
}

// ListCustomerFieldsOperation reads the company field dictionary.
type ListCustomerFieldsOperation struct{ client *Client }

// ListCustomerFields binds the Query to this client.
func (client *Client) ListCustomerFields() ListCustomerFieldsOperation {
	return ListCustomerFieldsOperation{client}
}

// Definition returns the manifest-generated Query worker.
func (ListCustomerFieldsOperation) Definition() sdkgo.QueryDefinition {
	return ListCustomerFieldsDefinition
}

// Invoke queries OKKI and classifies the provider outcome.
func (operation ListCustomerFieldsOperation) Invoke(call sdkgo.Call, _ ListCustomerFieldsInput) sdkgo.QueryAttempt[CustomerFields] {
	response := operation.client.listCustomerFields(call)
	if errors.Is(response.err, errCredential) {
		return fieldsBranch(ListCustomerFieldsBranchDefect, sdkgo.FailureAuthentication, "OKKI credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[CustomerFields](fieldsFailure(sdkgo.FailureTransport, "OKKI request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[CustomerFields](fieldsFailure(sdkgo.FailureRateLimit, "OKKI rate limited the query"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[CustomerFields](fieldsFailure(sdkgo.FailureAvailability, "OKKI is temporarily unavailable"), response.retryAfter)
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return fieldsBranch(ListCustomerFieldsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "OKKI response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[CustomerFields](fieldsFailure(sdkgo.FailureTransport, "OKKI response was interrupted"), 0)
	}
	if response.status != http.StatusOK {
		return fieldsBranch(ListCustomerFieldsBranchProviderRejected, sdkgo.FailureProviderRejection, "OKKI rejected HTTP "+strconv.Itoa(response.status))
	}
	if !utf8.Valid(response.body) {
		return fieldsBranch(ListCustomerFieldsBranchInvalidResponse, sdkgo.FailureProtocol, "OKKI returned invalid UTF-8")
	}
	var wire struct {
		Code *int `json:"code"`
		Data []struct {
			ID        json.RawMessage `json:"id"`
			Name      string          `json:"name"`
			FieldType int             `json:"field_type"`
		} `json:"data"`
	}
	decoder := json.NewDecoder(bytes.NewReader(response.body))
	if decoder.Decode(&wire) != nil {
		return fieldsBranch(ListCustomerFieldsBranchInvalidResponse, sdkgo.FailureProtocol, "OKKI returned invalid customer field JSON")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return fieldsBranch(ListCustomerFieldsBranchInvalidResponse, sdkgo.FailureProtocol, "OKKI returned trailing JSON")
	}
	if wire.Code == nil {
		return fieldsBranch(ListCustomerFieldsBranchInvalidResponse, sdkgo.FailureProtocol, "OKKI returned incomplete status")
	}
	if *wire.Code != 200 {
		if *wire.Code >= 500 {
			return sdkgo.NewQueryRetry[CustomerFields](fieldsFailure(sdkgo.FailureAvailability, "OKKI reported a transient error"), 0)
		}
		return fieldsBranch(ListCustomerFieldsBranchProviderRejected, sdkgo.FailureProviderRejection, "OKKI rejected the query")
	}
	if wire.Data == nil || len(wire.Data) > 500 {
		return fieldsBranch(ListCustomerFieldsBranchInvalidResponse, sdkgo.FailureProtocol, "OKKI returned incomplete or excessive fields")
	}
	fields := make([]CustomerField, 0, len(wire.Data))
	for _, field := range wire.Data {
		var id string
		if len(field.ID) == 0 {
			return fieldsBranch(ListCustomerFieldsBranchInvalidResponse, sdkgo.FailureProtocol, "OKKI field ID is missing")
		}
		if field.ID[0] == '"' {
			if json.Unmarshal(field.ID, &id) != nil {
				return fieldsBranch(ListCustomerFieldsBranchInvalidResponse, sdkgo.FailureProtocol, "OKKI field ID is invalid")
			}
		} else {
			var number json.Number
			if json.Unmarshal(field.ID, &number) != nil {
				return fieldsBranch(ListCustomerFieldsBranchInvalidResponse, sdkgo.FailureProtocol, "OKKI field ID is invalid")
			}
			id = number.String()
		}
		if id == "" {
			continue
		}
		if len(id) > 128 || field.Name == "" || len(field.Name) > 512 || field.FieldType <= 0 {
			return fieldsBranch(ListCustomerFieldsBranchInvalidResponse, sdkgo.FailureProtocol, "OKKI field definition is invalid")
		}
		fields = append(fields, CustomerField{ID: id, Name: field.Name, FieldType: field.FieldType})
	}
	return sdkgo.NewQueryBranch(ListCustomerFieldsBranchListed, CustomerFields{Fields: fields}, nil, sdkgo.Receipt{})
}
func fieldsFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "okki", Operation: "listCustomerFields", Message: message}
}
func fieldsBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[CustomerFields] {
	failure := fieldsFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, CustomerFields{}, &failure, sdkgo.Receipt{})
}
