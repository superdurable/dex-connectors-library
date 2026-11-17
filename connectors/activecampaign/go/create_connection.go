// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package activecampaign

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateConnectionInput creates one Deep Data ecommerce integration.
type CreateConnectionInput struct {
	// Service is the external commerce service name.
	Service string `json:"service"`
	// ExternalID is the store identifier within that service.
	ExternalID string `json:"externalId"`
	// Name is the connection's display name.
	Name string `json:"name"`
	// ListID is the optional numeric contact list ID.
	ListID string `json:"listId,omitempty"`
}

// CreateConnectionOperation creates one connection with bounded sends.
type CreateConnectionOperation struct{ client *Client }

type connectionCreateDispatch struct {
	client     *Client
	call       sdkgo.Call
	body       []byte
	response   []byte
	status     int
	retryAfter time.Duration
	err        error
}

// CreateConnection binds the create Mutation.
func (client *Client) CreateConnection() CreateConnectionOperation {
	return CreateConnectionOperation{client}
}

// Definition returns the manifest-generated create contract.
func (CreateConnectionOperation) Definition() sdkgo.MutationDefinition {
	return CreateConnectionDefinition
}

// IdempotencyKey correlates bounded sends locally; the provider receives no key.
func (CreateConnectionOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateConnectionInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

func (operation CreateConnectionOperation) Invoke(call sdkgo.Call, input CreateConnectionInput) sdkgo.MutationAttempt[ConnectionMetadata] {
	if !validConnectionLabel(input.Service) || !validConnectionLabel(input.ExternalID) || !validConnectionLabel(input.Name) || input.ListID != "" && !numericID.MatchString(input.ListID) {
		return connectionMutationBranch("createConnection", CreateConnectionBranchDefect, sdkgo.FailureValidation, "service, external ID, name, or list ID is invalid")
	}
	fields := map[string]string{"service": input.Service, "externalid": input.ExternalID, "name": input.Name}
	if input.ListID != "" {
		fields["listId"] = input.ListID
	}
	encoded, err := json.Marshal(map[string]any{"connection": fields})
	if err != nil {
		return connectionMutationBranch("createConnection", CreateConnectionBranchDefect, sdkgo.FailureLocalDefect, "account request could not be encoded")
	}
	fence, err := singledispatch.NewFence(call, CreateConnectionDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return connectionMutationBranch("createConnection", CreateConnectionBranchDefect, sdkgo.FailureLocalDefect, "connection checkpoint could not be initialized")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return connectionMutationBranch("createConnection", CreateConnectionBranchDefect, sdkgo.FailureLocalDefect, "connection checkpoint is invalid")
	}
	if decision.MaximumPriorSends > 0 || decision.Marker.ProviderID != "" {
		found, state := operation.readBack(call, input, decision.Marker.ProviderID)
		switch state {
		case "found":
			return sdkgo.NewMutationBranch(CreateConnectionBranchCreated, found, nil, dealReceipt(call, found.ID))
		case "retry":
			return sdkgo.NewMutationRetry[ConnectionMetadata](queryFailure("createConnection", sdkgo.FailureAvailability, "connection read-back unavailable"), 0)
		case "uncertain":
			return connectionCreateUncertain(call, "connection read-back cannot uniquely confirm the earlier create")
		}
	}
	if decision.Kind == singledispatch.ReadBackRequired {
		return connectionCreateUncertain(call, "an earlier connection create may have reached the send cap")
	}
	dispatch := &connectionCreateDispatch{client: operation.client, call: call, body: encoded}
	if err := fence.Dispatch(dispatch.execute); err != nil {
		return sdkgo.NewMutationRetry[ConnectionMetadata](queryFailure("createConnection", sdkgo.FailureAvailability, "connection checkpoint unavailable"), 0)
	}
	if errors.Is(dispatch.err, errCredential) {
		if err := fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[ConnectionMetadata](queryFailure("createConnection", sdkgo.FailureAvailability, "connection checkpoint unavailable"), 0)
		}
		return connectionMutationBranch("createConnection", CreateConnectionBranchDefect, sdkgo.FailureAuthentication, "connection credentials unavailable")
	}
	if errors.Is(dispatch.err, errRequest) {
		return sdkgo.NewMutationRetry[ConnectionMetadata](queryFailure("createConnection", sdkgo.FailureTransport, "connection create outcome is unknown"), 0)
	}
	if dispatch.status == http.StatusTooManyRequests || dispatch.status == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[ConnectionMetadata](queryFailure("createConnection", sdkgo.FailureAvailability, "connection checkpoint unavailable"), 0)
		}
		kind := sdkgo.FailureRateLimit
		if dispatch.status == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return sdkgo.NewMutationRetry[ConnectionMetadata](queryFailure("createConnection", kind, "ActiveCampaign temporarily rejected account creation"), dispatch.retryAfter)
	}
	if dispatch.status == http.StatusRequestTimeout || dispatch.status >= 500 {
		return sdkgo.NewMutationRetry[ConnectionMetadata](queryFailure("createConnection", sdkgo.FailureAvailability, "connection create outcome is unknown"), dispatch.retryAfter)
	}
	if dispatch.status != http.StatusCreated && dispatch.status != http.StatusOK {
		if decision.MaximumPriorSends > 0 {
			return connectionCreateUncertain(call, "earlier connection create may have succeeded")
		}
		return connectionMutationBranch("createConnection", CreateConnectionBranchProviderRejected, sdkgo.FailureProviderRejection, "ActiveCampaign rejected HTTP "+strconv.Itoa(dispatch.status))
	}
	if dispatch.err != nil {
		return connectionCreateUncertain(call, "connection create response was interrupted")
	}
	connection, err := decodeConnection(dispatch.response)
	if err != nil {
		return connectionCreateUncertain(call, "connection create response was invalid")
	}
	if !matchesConnection(connection, input) {
		return connectionCreateUncertain(call, "connection create response does not match the request")
	}
	_ = fence.RecordProviderID(connection.ID)
	return sdkgo.NewMutationBranch(CreateConnectionBranchCreated, connection, nil, dealReceipt(call, connection.ID))
}

func (operation CreateConnectionOperation) readBack(call sdkgo.Call, input CreateConnectionInput, providerID string) (ConnectionMetadata, string) {
	path := "/connections/" + providerID
	if providerID == "" {
		path = "/connections?" + url.Values{"filters[service]": {input.Service}, "filters[externalid]": {input.ExternalID}}.Encode()
	}
	body, status, _, err := operation.client.get(call, path)
	if err != nil || status == http.StatusTooManyRequests || status >= 500 {
		return ConnectionMetadata{}, "retry"
	}
	if status == http.StatusNotFound {
		return ConnectionMetadata{}, "absent"
	}
	if status != http.StatusOK {
		return ConnectionMetadata{}, "uncertain"
	}
	if providerID != "" {
		connection, err := decodeConnection(body)
		if err != nil || !matchesConnection(connection, input) {
			return ConnectionMetadata{}, "uncertain"
		}
		return connection, "found"
	}
	var wire struct {
		Connections []connectionWire `json:"connections"`
		Meta        struct {
			Total string `json:"total"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Connections == nil {
		return ConnectionMetadata{}, "uncertain"
	}
	total, err := strconv.Atoi(wire.Meta.Total)
	if err != nil || total > len(wire.Connections) {
		return ConnectionMetadata{}, "uncertain"
	}
	var match ConnectionMetadata
	for _, item := range wire.Connections {
		connection, err := connectionMetadata(item)
		if err != nil {
			return ConnectionMetadata{}, "uncertain"
		}
		if connection.Service == input.Service && connection.ExternalID == input.ExternalID {
			if !matchesConnection(connection, input) {
				return ConnectionMetadata{}, "uncertain"
			}
			if match.ID != "" {
				return ConnectionMetadata{}, "uncertain"
			}
			match = connection
		}
	}
	if match.ID != "" {
		return match, "found"
	}
	return ConnectionMetadata{}, "absent"
}

func matchesConnection(connection ConnectionMetadata, input CreateConnectionInput) bool {
	return connection.Service == input.Service && connection.ExternalID == input.ExternalID && connection.Name == input.Name && (input.ListID == "" || connection.ListID == input.ListID)
}

func (dispatch *connectionCreateDispatch) execute(ctx context.Context) error {
	dispatch.response, dispatch.status, dispatch.retryAfter, dispatch.err = dispatch.client.requestWithContext(ctx, dispatch.call, http.MethodPost, "/connections", dispatch.body)
	return nil
}
func connectionCreateUncertain(call sdkgo.Call, message string) sdkgo.MutationAttempt[ConnectionMetadata] {
	return sdkgo.NewMutationUncertain(ConnectionMetadata{}, queryFailure("createConnection", sdkgo.FailureTransport, message), dealReceipt(call, ""))
}
