// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package activecampaign

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListConnectionsInput filters Deep Data ecommerce integrations.
type ListConnectionsInput struct {
	// Service optionally filters by the integration's service name.
	Service string `json:"service,omitempty"`
	// ExternalID optionally filters by the store identifier in that service.
	ExternalID string `json:"externalId,omitempty"`
}

// GetConnectionInput selects one ecommerce connection by numeric ID.
type GetConnectionInput struct {
	// ConnectionID is the numeric provider connection ID.
	ConnectionID string `json:"connectionId"`
}

// ConnectionMetadata identifies one ecommerce integration without provider links.
type ConnectionMetadata struct {
	// ID is the numeric provider connection ID.
	ID string `json:"id"`
	// Service is the external commerce service name.
	Service string `json:"service"`
	// ExternalID is the store identifier in that service.
	ExternalID string `json:"externalId"`
	// Name is the integration's display name.
	Name string `json:"name"`
	// ListID is the optional list that receives ecommerce contacts.
	ListID string `json:"listId"`
}

// ConnectionsPage contains connections matching the requested filters.
type ConnectionsPage struct {
	// Connections contains the matching connection metadata.
	Connections []ConnectionMetadata `json:"connections"`
	// Total is the provider's matching connection count.
	Total int `json:"total"`
}

// ListConnectionsOperation reads ecommerce integrations.
type ListConnectionsOperation struct{ client *Client }

// GetConnectionOperation reads one ecommerce integration.
type GetConnectionOperation struct{ client *Client }

// ListConnections binds the listing Query.
func (client *Client) ListConnections() ListConnectionsOperation {
	return ListConnectionsOperation{client}
}

// GetConnection binds the lookup Query.
func (client *Client) GetConnection() GetConnectionOperation { return GetConnectionOperation{client} }

// Definition returns the manifest-generated listing contract.
func (ListConnectionsOperation) Definition() sdkgo.QueryDefinition { return ListConnectionsDefinition }

// Definition returns the manifest-generated lookup contract.
func (GetConnectionOperation) Definition() sdkgo.QueryDefinition { return GetConnectionDefinition }

// Invoke reads Deep Data connections by service or store identifier.
func (operation ListConnectionsOperation) Invoke(call sdkgo.Call, input ListConnectionsInput) sdkgo.QueryAttempt[ConnectionsPage] {
	branches := queryBranches{found: ListConnectionsBranchListed, rejected: ListConnectionsBranchProviderRejected, invalid: ListConnectionsBranchInvalidResponse, defect: ListConnectionsBranchDefect}
	if len(input.Service) > 250 || len(input.ExternalID) > 250 || strings.ContainsAny(input.Service+input.ExternalID, "\r\n") {
		return queryBranch("listConnections", branches.defect, ConnectionsPage{}, sdkgo.FailureValidation, "service or external ID is invalid")
	}
	query := url.Values{}
	if input.Service != "" {
		query.Set("filters[service]", input.Service)
	}
	if input.ExternalID != "" {
		query.Set("filters[externalid]", input.ExternalID)
	}
	path := "/connections"
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	return invokeQuery(call, operation.client, "listConnections", path, branches, decodeConnectionsPage)
}

// Invoke reads one ecommerce connection by ID.
func (operation GetConnectionOperation) Invoke(call sdkgo.Call, input GetConnectionInput) sdkgo.QueryAttempt[ConnectionMetadata] {
	branches := queryBranches{found: GetConnectionBranchFound, notFound: GetConnectionBranchNotFound, rejected: GetConnectionBranchProviderRejected, invalid: GetConnectionBranchInvalidResponse, defect: GetConnectionBranchDefect}
	if !numericID.MatchString(input.ConnectionID) {
		return queryBranch("getConnection", branches.defect, ConnectionMetadata{}, sdkgo.FailureValidation, "connection ID must be numeric")
	}
	return invokeQuery(call, operation.client, "getConnection", "/connections/"+input.ConnectionID, branches, decodeConnection)
}

type connectionWire struct {
	ID         string `json:"id"`
	Service    string `json:"service"`
	ExternalID string `json:"externalid"`
	Name       string `json:"name"`
	ListID     string `json:"listId"`
}

func decodeConnectionsPage(body []byte) (ConnectionsPage, error) {
	var wire struct {
		Connections []connectionWire `json:"connections"`
		Meta        struct {
			Total string `json:"total"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return ConnectionsPage{}, err
	}
	total, err := strconv.Atoi(wire.Meta.Total)
	if wire.Connections == nil || err != nil || total < 0 {
		return ConnectionsPage{}, fmt.Errorf("connection collection or count missing")
	}
	page := ConnectionsPage{Connections: make([]ConnectionMetadata, 0, len(wire.Connections)), Total: total}
	for _, item := range wire.Connections {
		connection, err := connectionMetadata(item)
		if err != nil {
			return ConnectionsPage{}, err
		}
		page.Connections = append(page.Connections, connection)
	}
	return page, nil
}

func decodeConnection(body []byte) (ConnectionMetadata, error) {
	var wire struct {
		Connection *connectionWire `json:"connection"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return ConnectionMetadata{}, err
	}
	if wire.Connection == nil {
		return ConnectionMetadata{}, fmt.Errorf("connection missing")
	}
	return connectionMetadata(*wire.Connection)
}

func connectionMetadata(wire connectionWire) (ConnectionMetadata, error) {
	if !numericID.MatchString(wire.ID) || wire.Service == "" || wire.ExternalID == "" || wire.Name == "" {
		return ConnectionMetadata{}, fmt.Errorf("connection identity invalid")
	}
	return ConnectionMetadata{ID: wire.ID, Service: wire.Service, ExternalID: wire.ExternalID, Name: wire.Name, ListID: wire.ListID}, nil
}
