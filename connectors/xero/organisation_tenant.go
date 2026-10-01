// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package xero

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	// organisationTenantType is the Xero tenant type that the Accounting API serves.
	organisationTenantType = "ORGANISATION"
	// MaxOrganisationCharacters bounds the organisation name or tenant ID in the connection configuration.
	MaxOrganisationCharacters = 255
)

// organisationTenant is the Xero-Tenant-Id one OAuth call sends; isDerived marks a value read from /connections.
type organisationTenant struct {
	tenantID  string
	isDerived bool
}

// organisationTenantResolver finds an OAuth connection's Xero-Tenant-Id and remembers one read from /connections.
type organisationTenantResolver struct {
	organisation     string
	httpClient       *http.Client
	maxResponseBytes int64

	mutex           sync.Mutex
	derivedTenantID string
}

// connectionWire is one entry of the JSON array that GET https://api.xero.com/connections returns.
type connectionWire struct {
	TenantID   string  `json:"tenantId"`
	TenantType string  `json:"tenantType"`
	TenantName *string `json:"tenantName"`
}

func newOrganisationTenantResolver(organisation string, httpClient *http.Client, maxResponseBytes int64) *organisationTenantResolver {
	return &organisationTenantResolver{organisation: organisation, httpClient: httpClient, maxResponseBytes: maxResponseBytes}
}

// resolveTenant uses a configured tenant ID, else the named or only organisation in /connections.
func (resolver *organisationTenantResolver) resolveTenant(
	call sdkgo.Call,
	accessToken sdkgo.SecretString,
	operation string,
	now time.Time,
) (organisationTenant, *xeroExchange) {
	if uuidPattern.MatchString(resolver.organisation) {
		return organisationTenant{tenantID: strings.ToLower(resolver.organisation)}, nil
	}
	resolver.mutex.Lock()
	derivedTenantID := resolver.derivedTenantID
	resolver.mutex.Unlock()
	if derivedTenantID != "" {
		return organisationTenant{tenantID: derivedTenantID, isDerived: true}, nil
	}
	connections, failedRead := resolver.readConnections(call, accessToken, operation, now)
	if failedRead != nil {
		return organisationTenant{}, failedRead
	}
	tenantID, failure := resolver.selectTenant(connections, operation)
	if failure != nil {
		return organisationTenant{}, &xeroExchange{outcome: exchangeDefect, failure: *failure}
	}
	resolver.mutex.Lock()
	resolver.derivedTenantID = tenantID
	resolver.mutex.Unlock()
	return organisationTenant{tenantID: tenantID, isDerived: true}, nil
}

// forgetDerivedTenant drops a remembered tenant after Xero denied it, as after the organisation was disconnected.
func (resolver *organisationTenantResolver) forgetDerivedTenant(tenantID string) {
	resolver.mutex.Lock()
	defer resolver.mutex.Unlock()
	if resolver.derivedTenantID == tenantID {
		resolver.derivedTenantID = ""
	}
}

func (resolver *organisationTenantResolver) readConnections(
	call sdkgo.Call,
	accessToken sdkgo.SecretString,
	operation string,
	now time.Time,
) ([]connectionWire, *xeroExchange) {
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, connectionsURL, nil)
	if err != nil {
		return nil, &xeroExchange{outcome: exchangeDefect, failure: xeroFailure(sdkgo.FailureLocalDefect, operation, errRequestNotBuilt.Error())}
	}
	request.Header.Set("Authorization", "Bearer "+accessToken.Reveal())
	request.Header.Set("Accept", "application/json")
	httpResponse, err := resolver.httpClient.Do(request)
	if err != nil {
		return nil, &xeroExchange{outcome: exchangeRetry,
			failure: xeroFailure(sdkgo.FailureTransport, operation, "Xero connections lookup failed before a response arrived")}
	}
	result := classifyHTTPResponse(operation, httpResponse, accessToken.Reveal(), resolver.maxResponseBytes, now)
	switch result.outcome {
	case exchangeSucceeded:
	case exchangeServerError:
		// No idempotency key is involved, so an internal error on this read is retried like any other.
		result.outcome = exchangeRetry
		return nil, &result
	case exchangeInvalid, exchangeNotFound:
		result.outcome = exchangeRejected
		return nil, &result
	default:
		return nil, &result
	}
	var connections []connectionWire
	if err := json.Unmarshal(result.response.body, &connections); err != nil {
		return nil, &xeroExchange{outcome: exchangeRejected, response: result.response,
			failure: xeroFailure(sdkgo.FailureProtocol, operation, "Xero returned a connections list that is not a JSON array")}
	}
	return connections, nil
}

// selectTenant chooses one organisation without echoing organisation names into the Failure.
func (resolver *organisationTenantResolver) selectTenant(connections []connectionWire, operation string) (string, *sdkgo.Failure) {
	var candidates []connectionWire
	for _, connection := range connections {
		if connection.TenantType != organisationTenantType || !uuidPattern.MatchString(connection.TenantID) {
			continue
		}
		if resolver.organisation != "" && (connection.TenantName == nil ||
			!strings.EqualFold(strings.TrimSpace(*connection.TenantName), resolver.organisation)) {
			continue
		}
		candidates = append(candidates, connection)
	}
	switch {
	case len(candidates) == 1:
		return strings.ToLower(candidates[0].TenantID), nil
	case resolver.organisation == "" && len(candidates) == 0:
		return "", xeroFailurePointer(sdkgo.FailureValidation, operation,
			"the Xero authorization connects no organisation; authorize again and select one")
	case resolver.organisation == "":
		return "", xeroFailurePointer(sdkgo.FailureValidation, operation, fmt.Sprintf(
			"the Xero authorization connects %d organisations; set organisation to the name or tenant ID of one", len(candidates)))
	case len(candidates) == 0:
		return "", xeroFailurePointer(sdkgo.FailureValidation, operation,
			"no organisation connected to the Xero authorization has the configured name")
	default:
		return "", xeroFailurePointer(sdkgo.FailureValidation, operation,
			"several organisations connected to the Xero authorization have the configured name; set organisation to the tenant ID")
	}
}

// validateOrganisation checks the configured organisation name or tenant ID.
func validateOrganisation(organisation string) error {
	if err := validateSingleLineText("organisation", organisation, MaxOrganisationCharacters); err != nil {
		return fmt.Errorf("Xero %w", err)
	}
	return nil
}
