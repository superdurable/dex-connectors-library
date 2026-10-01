// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package helpscout

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// MaxCustomerMatches bounds the customer profiles findCustomerByEmail returns.
const MaxCustomerMatches = 50

// FindCustomerByEmailInput names one email address to look up.
type FindCustomerByEmailInput struct {
	// Email is one bare address, such as jane@example.com.
	Email string `json:"email"`
}

// CustomerMatches lists every customer profile Help Scout matched, newest first. Help Scout can hold
// several profiles for one person, so the Flow decides which one it means.
type CustomerMatches struct {
	// Email is the address that was looked up.
	Email string `json:"email"`
	// Customers lists up to MaxCustomerMatches matching profiles; it is empty on the notFound branch.
	Customers []Customer `json:"customers"`
	// HasMore reports that Help Scout matched more profiles than Customers holds.
	HasMore bool `json:"hasMore,omitempty"`
}

// Customer is the connector-safe subset of one Help Scout customer profile. Times are UTC.
type Customer struct {
	// ID is the customer ID, which replyToConversation accepts as CustomerID.
	ID int64 `json:"id"`
	// FirstName is the customer's first name.
	FirstName string `json:"firstName,omitempty"`
	// LastName is the customer's last name.
	LastName string `json:"lastName,omitempty"`
	// Emails lists the profile's email addresses that Help Scout embedded in the response.
	Emails []string `json:"emails"`
	// OrganizationID is the customer's organization, or zero.
	OrganizationID int64 `json:"organizationId,omitempty"`
	// ConversationCount is Help Scout's count of the customer's conversations.
	ConversationCount int `json:"conversationCount"`
	// CreatedAt is when the profile was created.
	CreatedAt time.Time `json:"createdAt"`
	// UpdatedAt is when the profile was last changed, or zero.
	UpdatedAt time.Time `json:"updatedAt,omitzero"`
}

// FindCustomerByEmailOperation implements the findCustomerByEmail Query. Build it with
// Client.FindCustomerByEmail.
type FindCustomerByEmailOperation struct{ client *Client }

type customerPageWire struct {
	Embedded struct {
		Customers []struct {
			ID                int64  `json:"id"`
			FirstName         string `json:"firstName"`
			LastName          string `json:"lastName"`
			OrganizationID    int64  `json:"organizationId"`
			ConversationCount int    `json:"conversationCount"`
			CreatedAt         string `json:"createdAt"`
			UpdatedAt         string `json:"updatedAt"`
			Embedded          struct {
				Emails []struct {
					Value string `json:"value"`
				} `json:"emails"`
			} `json:"_embedded"`
		} `json:"customers"`
	} `json:"_embedded"`
	Links helpScoutPageLinks `json:"_links"`
}

// Definition returns the immutable findCustomerByEmail operation definition.
func (FindCustomerByEmailOperation) Definition() sdkgo.QueryDefinition {
	return FindCustomerByEmailDefinition
}

// Invoke reads the first page of GET /v3/customers?email=..., Help Scout's typed email filter, rather than
// the /v2 query=(email:"...") search, which also matches profiles whose email only contains the address.
func (operation FindCustomerByEmailOperation) Invoke(call sdkgo.Call, input FindCustomerByEmailInput) sdkgo.QueryAttempt[CustomerMatches] {
	operationID := FindCustomerByEmailDefinition.Operation.OperationID
	client := operation.client
	output := CustomerMatches{Email: input.Email, Customers: []Customer{}}
	if !isBareEmailAddress(input.Email) {
		return sdkgo.NewQueryBranch(FindCustomerByEmailBranchDefect, output,
			helpScoutFailurePointer(operationID, sdkgo.FailureValidation, "email must be one bare email address such as jane@example.com"), sdkgo.Receipt{})
	}
	credentials, err := client.resolveCredentials(call)
	if err != nil {
		return credentialQueryAttempt[CustomerMatches](operationID, FindCustomerByEmailBranchDefect, err)
	}
	response, err := client.send(call.Context, call, &credentials,
		helpScoutRequest{method: http.MethodGet, path: "/v3/customers", query: url.Values{"email": {input.Email}}}, nil)
	branches := failureBranches{providerRejected: FindCustomerByEmailBranchProviderRejected, invalidResponse: FindCustomerByEmailBranchInvalidResponse}
	if attempt, isTerminal := classifyQueryExchange[CustomerMatches](client, call, operationID, credentials, response, err, branches, 0); isTerminal {
		return attempt
	}
	output, err = decodeCustomerPage(response.body, input.Email)
	if err != nil {
		return sdkgo.NewQueryBranch(FindCustomerByEmailBranchInvalidResponse, CustomerMatches{Email: input.Email, Customers: []Customer{}},
			helpScoutFailurePointer(operationID, sdkgo.FailureProtocol, "Help Scout returned an invalid customer page: "+err.Error()),
			client.receipt(call, 0, ""))
	}
	if len(output.Customers) == 0 {
		return sdkgo.NewQueryBranch(FindCustomerByEmailBranchNotFound, output,
			helpScoutFailurePointer(operationID, sdkgo.FailureNotFound, "no Help Scout customer has this email address"), client.receipt(call, 0, ""))
	}
	return sdkgo.NewQueryBranch(FindCustomerByEmailBranchFound, output, nil, client.receipt(call, output.Customers[0].ID, ""))
}

func decodeCustomerPage(body []byte, email string) (CustomerMatches, error) {
	var page customerPageWire
	if err := decodeHelpScoutJSON(body, &page); err != nil {
		return CustomerMatches{}, errors.New("page is not a JSON object")
	}
	output := CustomerMatches{Email: email, Customers: make([]Customer, 0, min(len(page.Embedded.Customers), MaxCustomerMatches))}
	for index, wire := range page.Embedded.Customers {
		if index == MaxCustomerMatches {
			output.HasMore = true
			break
		}
		if wire.ID < 1 {
			return CustomerMatches{}, fmt.Errorf("customer %d has no ID", index)
		}
		createdAt, err := parseHelpScoutTimestamp(wire.CreatedAt)
		if err != nil {
			return CustomerMatches{}, fmt.Errorf("customer %d createdAt: %w", index, err)
		}
		updatedAt, err := parseOptionalHelpScoutTimestamp(wire.UpdatedAt)
		if err != nil {
			return CustomerMatches{}, fmt.Errorf("customer %d updatedAt: %w", index, err)
		}
		customer := Customer{
			ID: wire.ID, FirstName: wire.FirstName, LastName: wire.LastName, Emails: make([]string, 0, len(wire.Embedded.Emails)),
			OrganizationID: wire.OrganizationID, ConversationCount: wire.ConversationCount, CreatedAt: createdAt, UpdatedAt: updatedAt,
		}
		for _, address := range wire.Embedded.Emails {
			if address.Value != "" {
				customer.Emails = append(customer.Emails, address.Value)
			}
		}
		output.Customers = append(output.Customers, customer)
	}
	output.HasMore = output.HasMore || page.Links.Next != nil
	return output, nil
}
