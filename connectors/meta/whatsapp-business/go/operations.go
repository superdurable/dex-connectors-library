// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package whatsappbusiness

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListPhoneNumbersInput selects one WhatsApp Business Account and an optional page cursor.
type ListPhoneNumbersInput struct {
	// BusinessAccountID is the numeric WABA ID in Meta Business Settings.
	BusinessAccountID string `json:"businessAccountId"`
	// After is the opaque cursor from a prior PhoneNumbersPage; blank reads the first page.
	After string `json:"after,omitempty"`
}

// BusinessPhoneNumber identifies one registered business phone number.
type BusinessPhoneNumber struct {
	// ID is the stable Meta phone-number ID for later messaging operations.
	ID string `json:"id"`
	// VerifiedName is the provider-verified business display name.
	VerifiedName string `json:"verifiedName"`
	// DisplayPhoneNumber is the provider-formatted business telephone number.
	DisplayPhoneNumber string `json:"displayPhoneNumber"`
}

// PhoneNumbersPage contains one bounded Graph API page and its continuation cursor.
type PhoneNumbersPage struct {
	// Numbers are the visible business phone numbers on this page.
	Numbers []BusinessPhoneNumber `json:"numbers"`
	// NextCursor is blank when Meta did not report a subsequent page.
	NextCursor string `json:"nextCursor"`
}

// ListPhoneNumbersOperation reads one page of phone-number metadata.
type ListPhoneNumbersOperation struct{ client *Client }

// ListPhoneNumbers binds the Query to a client.
func (client *Client) ListPhoneNumbers() ListPhoneNumbersOperation {
	return ListPhoneNumbersOperation{client}
}

// Definition returns the manifest-generated Query contract.
func (ListPhoneNumbersOperation) Definition() sdkgo.QueryDefinition {
	return ListPhoneNumbersDefinition
}

var numericID = regexp.MustCompile(`^[0-9]{1,40}$`)

// Invoke reads one page, classifying provider failures and excluding unneeded fields.
func (operation ListPhoneNumbersOperation) Invoke(call sdkgo.Call, input ListPhoneNumbersInput) sdkgo.QueryAttempt[PhoneNumbersPage] {
	if !numericID.MatchString(input.BusinessAccountID) || len(input.After) > 4096 || strings.ContainsAny(input.After, "\r\n\x00") {
		return queryBranch(ListPhoneNumbersBranchDefect, PhoneNumbersPage{}, sdkgo.FailureValidation, "WABA ID or page cursor is invalid")
	}
	path := "/" + input.BusinessAccountID + "/phone_numbers"
	if input.After != "" {
		path += "?" + url.Values{"after": {input.After}}.Encode()
	}
	response := operation.client.get(call, path)
	if errors.Is(response.err, errCredential) {
		return queryBranch(ListPhoneNumbersBranchDefect, PhoneNumbersPage{}, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[PhoneNumbersPage](queryFailure(sdkgo.FailureTransport, "Meta request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[PhoneNumbersPage](queryFailure(sdkgo.FailureRateLimit, "Meta rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[PhoneNumbersPage](queryFailure(sdkgo.FailureAvailability, "Meta is temporarily unavailable"), response.retryAfter)
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return queryBranch(ListPhoneNumbersBranchInvalidResponse, PhoneNumbersPage{}, sdkgo.FailureResponseTooLarge, "Meta response exceeded the configured limit")
	}
	if response.status == http.StatusNotFound {
		return queryBranch(ListPhoneNumbersBranchNotFound, PhoneNumbersPage{}, sdkgo.FailureNotFound, "Meta did not find the WhatsApp Business Account")
	}
	if response.status != http.StatusOK {
		return queryBranch(ListPhoneNumbersBranchProviderRejected, PhoneNumbersPage{}, sdkgo.FailureProviderRejection, "Meta rejected HTTP "+strconv.Itoa(response.status))
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[PhoneNumbersPage](queryFailure(sdkgo.FailureTransport, "Meta response was interrupted"), 0)
	}
	page, err := decodePhoneNumbersPage(response.body)
	if err != nil {
		return queryBranch(ListPhoneNumbersBranchInvalidResponse, PhoneNumbersPage{}, sdkgo.FailureProtocol, "Meta returned invalid phone-number JSON")
	}
	return sdkgo.NewQueryBranch(ListPhoneNumbersBranchListed, page, nil, sdkgo.Receipt{})
}

func decodePhoneNumbersPage(body []byte) (PhoneNumbersPage, error) {
	var wire struct {
		Data []struct {
			ID                 string `json:"id"`
			VerifiedName       string `json:"verified_name"`
			DisplayPhoneNumber string `json:"display_phone_number"`
		} `json:"data"`
		Paging struct {
			Cursors struct {
				After string `json:"after"`
			} `json:"cursors"`
		} `json:"paging"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return PhoneNumbersPage{}, err
	}
	if wire.Data == nil || len(wire.Data) > 100 || len(wire.Paging.Cursors.After) > 4096 {
		return PhoneNumbersPage{}, errors.New("Meta phone-number page is invalid")
	}
	page := PhoneNumbersPage{Numbers: make([]BusinessPhoneNumber, 0, len(wire.Data)), NextCursor: wire.Paging.Cursors.After}
	for _, item := range wire.Data {
		if !numericID.MatchString(item.ID) || item.VerifiedName == "" || item.DisplayPhoneNumber == "" {
			return PhoneNumbersPage{}, fmt.Errorf("Meta phone-number identity is incomplete")
		}
		page.Numbers = append(page.Numbers, BusinessPhoneNumber{ID: item.ID, VerifiedName: item.VerifiedName, DisplayPhoneNumber: item.DisplayPhoneNumber})
	}
	return page, nil
}

func queryBranch(branch sdkgo.BranchID, value PhoneNumbersPage, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[PhoneNumbersPage] {
	failure := queryFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, value, &failure, sdkgo.Receipt{})
}

func queryFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "whatsapp-business", Operation: "listPhoneNumbers", Message: message}
}
