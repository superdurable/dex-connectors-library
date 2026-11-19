// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package harvest

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListClientsInput selects one cursor-paginated page of Harvest clients.
type ListClientsInput struct {
	// Cursor is the opaque cursor from the previous page; blank starts a listing.
	Cursor string `json:"cursor,omitempty"`
	// PerPage is the page size, from 1 through 2000; zero uses 100.
	PerPage int `json:"perPage,omitempty"`
}

// GetClientInput identifies a client by its positive Harvest ID.
type GetClientInput struct {
	// ID is the client ID in Harvest.
	ID int64 `json:"id"`
}

// ClientRecord is the public identity and editable details of a Harvest client.
type ClientRecord struct {
	// ID is assigned by Harvest.
	ID int64 `json:"id"`
	// Name is the client display name.
	Name string `json:"name"`
	// IsActive reports whether the client is active rather than archived.
	IsActive bool `json:"isActive"`
	// Address is the optional physical or mailing address.
	Address string `json:"address,omitempty"`
	// Currency is the client's ISO currency code.
	Currency string `json:"currency,omitempty"`
}

// ClientPage contains one page of clients and its next cursor.
type ClientPage struct {
	// Clients are returned in Harvest's creation-date order.
	Clients []ClientRecord `json:"clients"`
	// NextCursor is blank on the final page.
	NextCursor string `json:"nextCursor,omitempty"`
}

// ListClientsOperation performs a Harvest client-list Query.
type ListClientsOperation struct{ client *Client }

// ListClients binds the client-list Query to this client.
func (client *Client) ListClients() ListClientsOperation { return ListClientsOperation{client} }

// Definition returns the generated client-list contract.
func (ListClientsOperation) Definition() sdkgo.QueryDefinition { return ListClientsDefinition }

// Invoke validates pagination and reads one client page.
func (operation ListClientsOperation) Invoke(call sdkgo.Call, input ListClientsInput) sdkgo.QueryAttempt[ClientPage] {
	if err := validateHarvestPage(input.Cursor, input.PerPage); err != nil {
		return failedBranch[ClientPage]("listClients", ListClientsBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	return invokeGet(operation.client, call, "listClients", harvestPagePath("/clients", input.Cursor, input.PerPage), ListClientsBranchListed, ListClientsBranchProviderRejected, decodeClientPage)
}

// GetClientOperation performs a Harvest client lookup Query.
type GetClientOperation struct{ client *Client }

// GetClient binds the client lookup Query to this client.
func (client *Client) GetClient() GetClientOperation { return GetClientOperation{client} }

// Definition returns the generated client lookup contract.
func (GetClientOperation) Definition() sdkgo.QueryDefinition { return GetClientDefinition }

// Invoke validates the ID and reads the client.
func (operation GetClientOperation) Invoke(call sdkgo.Call, input GetClientInput) sdkgo.QueryAttempt[ClientRecord] {
	if input.ID <= 0 {
		return failedBranch[ClientRecord]("getClient", GetClientBranchDefect, sdkgo.FailureValidation, "client ID must be positive")
	}
	return invokeGet(operation.client, call, "getClient", "/clients/"+strconv.FormatInt(input.ID, 10), GetClientBranchFound, GetClientBranchNotFound, decodeClientRecord)
}

func decodeClientPage(body []byte) (ClientPage, error) {
	var wire struct {
		Clients []clientWire `json:"clients"`
		Links   struct {
			Next *string `json:"next"`
		} `json:"links"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return ClientPage{}, err
	}
	if wire.Clients == nil {
		return ClientPage{}, fmt.Errorf("missing clients")
	}
	next, err := nextHarvestCursor(wire.Links.Next, "/clients")
	if err != nil {
		return ClientPage{}, err
	}
	page := ClientPage{Clients: make([]ClientRecord, 0, len(wire.Clients)), NextCursor: next}
	for _, client := range wire.Clients {
		page.Clients = append(page.Clients, client.record())
	}
	return page, nil
}

func decodeClientRecord(body []byte) (ClientRecord, error) {
	var wire clientWire
	if err := json.Unmarshal(body, &wire); err != nil {
		return ClientRecord{}, err
	}
	if wire.ID <= 0 || wire.Name == "" {
		return ClientRecord{}, fmt.Errorf("missing client identity")
	}
	return wire.record(), nil
}

type clientWire struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	IsActive bool   `json:"is_active"`
	Address  string `json:"address"`
	Currency string `json:"currency"`
}

func (wire clientWire) record() ClientRecord {
	return ClientRecord{ID: wire.ID, Name: wire.Name, IsActive: wire.IsActive, Address: wire.Address, Currency: wire.Currency}
}
