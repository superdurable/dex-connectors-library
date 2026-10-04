// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package quickbooks

import (
	"errors"
	"net/http"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	createCustomerOperationID = "createCustomer"
	// maximumPersonNameCharacters is QuickBooks's GivenName, FamilyName, and CompanyName limit.
	maximumPersonNameCharacters = 100
	// maximumPhoneCharacters is QuickBooks's FreeFormNumber limit.
	maximumPhoneCharacters = 30
	// maximumCustomerEmailCharacters is QuickBooks's PrimaryEmailAddr limit.
	maximumCustomerEmailCharacters = 100
)

// CreateCustomerInput describes one new customer.
type CreateCustomerInput struct {
	// DisplayName is required and must be unique across QuickBooks customers, vendors, and
	// employees; a name in use selects nameConflict.
	DisplayName string `json:"displayName"`
	// EmailAddress is the primary email address, such as accounts@example.com; blank leaves it unset.
	EmailAddress string `json:"emailAddress,omitempty"`
	// GivenName is the contact's first name, at most 100 characters.
	GivenName string `json:"givenName,omitempty"`
	// FamilyName is the contact's last name, at most 100 characters.
	FamilyName string `json:"familyName,omitempty"`
	// CompanyName is the customer's company, at most 100 characters.
	CompanyName string `json:"companyName,omitempty"`
	// Phone is the primary phone number exactly as it should appear, at most 30 characters.
	Phone string `json:"phone,omitempty"`
	// CurrencyCode is the customer's ISO 4217 currency when the company uses multicurrency;
	// blank uses the company's home currency. QuickBooks cannot change it later.
	CurrencyCode string `json:"currencyCode,omitempty"`
}

// CreateCustomerOutput is the created customer. On every other branch Customer is empty and
// DisplayName echoes the request, so an application can resolve it with findCustomer.
type CreateCustomerOutput struct {
	// Customer is the customer QuickBooks created or, for a replayed attempt, created earlier for this Step.
	Customer Customer `json:"customer"`
	// DisplayName echoes the requested display name.
	DisplayName string `json:"displayName"`
}

// CreateCustomerOperation implements the createCustomer Mutation.
type CreateCustomerOperation struct{ client *Client }

type createCustomerRequestWire struct {
	DisplayName      string            `json:"DisplayName"`
	PrimaryEmailAddr *emailAddressWire `json:"PrimaryEmailAddr,omitempty"`
	GivenName        string            `json:"GivenName,omitempty"`
	FamilyName       string            `json:"FamilyName,omitempty"`
	CompanyName      string            `json:"CompanyName,omitempty"`
	PrimaryPhone     *phoneNumberWire  `json:"PrimaryPhone,omitempty"`
	CurrencyRef      *referenceWire    `json:"CurrencyRef,omitempty"`
}

// Definition returns the immutable connector operation definition.
func (CreateCustomerOperation) Definition() sdkgo.MutationDefinition { return CreateCustomerDefinition }

// IdempotencyKey is the stable Call ID, sent as QuickBooks's requestid. Every attempt of one Step
// execution, including one on a replacement Worker, sends the same requestid.
func (CreateCustomerOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateCustomerInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates the customer with POST /customer under the Step's requestid.
func (operation CreateCustomerOperation) Invoke(call sdkgo.Call, input CreateCustomerInput) sdkgo.MutationAttempt[CreateCustomerOutput] {
	client := operation.client
	requested := CreateCustomerOutput{DisplayName: strings.TrimSpace(input.DisplayName)}
	payload, err := buildCreateCustomerRequest(input)
	if err != nil {
		return sdkgo.NewMutationBranch(CreateCustomerBranchDefect, requested,
			quickbooksFailurePointer(sdkgo.FailureValidation, createCustomerOperationID, err.Error()), sdkgo.Receipt{})
	}
	result := client.exchange(call, createCustomerOperationID, quickbooksRequest{method: http.MethodPost, path: "/customer", payload: payload, isMutation: true})
	receipt := client.receipt(call, result, "")
	if attempt, isTerminal := mutationAttemptForExchange(result, requested, receipt, mutationBranches{
		nameConflict: CreateCustomerBranchNameConflict, providerRejected: CreateCustomerBranchProviderRejected, defect: CreateCustomerBranchDefect,
	}); isTerminal {
		return attempt
	}
	wire, err := decodeEntity[customerWire](result.response.body, "Customer")
	var customer Customer
	if err == nil {
		customer, err = decodeCustomer(wire)
	}
	if err != nil {
		return sdkgo.NewMutationUncertain(requested, quickbooksFailure(sdkgo.FailureProtocol, createCustomerOperationID,
			"QuickBooks accepted the customer but returned an unusable customer: "+err.Error()), receipt)
	}
	receipt.ProviderObjectID = customer.CustomerID
	return sdkgo.NewMutationBranch(CreateCustomerBranchCreated, CreateCustomerOutput{Customer: customer, DisplayName: requested.DisplayName}, nil, receipt)
}

// buildCreateCustomerRequest validates every field before any request.
func buildCreateCustomerRequest(input CreateCustomerInput) (createCustomerRequestWire, error) {
	payload := createCustomerRequestWire{
		DisplayName: strings.TrimSpace(input.DisplayName), GivenName: strings.TrimSpace(input.GivenName),
		FamilyName: strings.TrimSpace(input.FamilyName), CompanyName: strings.TrimSpace(input.CompanyName),
	}
	if payload.DisplayName == "" {
		return createCustomerRequestWire{}, errBlankDisplayName
	}
	for _, text := range []struct {
		name    string
		value   string
		maximum int
	}{
		{"displayName", payload.DisplayName, MaxDisplayNameCharacters}, {"givenName", payload.GivenName, maximumPersonNameCharacters},
		{"familyName", payload.FamilyName, maximumPersonNameCharacters}, {"companyName", payload.CompanyName, maximumPersonNameCharacters},
	} {
		if err := validateText(text.name, text.value, text.maximum, false); err != nil {
			return createCustomerRequestWire{}, err
		}
	}
	if strings.Contains(payload.DisplayName, ":") {
		return createCustomerRequestWire{}, errors.New("displayName cannot contain a colon, which QuickBooks reserves for sub-customers")
	}
	if emailAddress := strings.TrimSpace(input.EmailAddress); emailAddress != "" {
		if err := validateEmailAddress("emailAddress", emailAddress); err != nil {
			return createCustomerRequestWire{}, err
		}
		if err := validateText("emailAddress", emailAddress, maximumCustomerEmailCharacters, false); err != nil {
			return createCustomerRequestWire{}, err
		}
		payload.PrimaryEmailAddr = &emailAddressWire{Address: emailAddress}
	}
	if phone := strings.TrimSpace(input.Phone); phone != "" {
		if err := validateText("phone", phone, maximumPhoneCharacters, false); err != nil {
			return createCustomerRequestWire{}, err
		}
		payload.PrimaryPhone = &phoneNumberWire{FreeFormNumber: phone}
	}
	if currencyCode := strings.TrimSpace(input.CurrencyCode); currencyCode != "" {
		if err := validateCurrencyCode("currencyCode", currencyCode); err != nil {
			return createCustomerRequestWire{}, err
		}
		payload.CurrencyRef = &referenceWire{Value: currencyCode}
	}
	return payload, nil
}
