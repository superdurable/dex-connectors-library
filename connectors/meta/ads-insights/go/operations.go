// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package metaadsinsights

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetAdInput identifies one Meta Marketing API ad.
type GetAdInput struct {
	// AdID is the numeric ad ID from Ads Manager.
	AdID string `json:"adId"`
}

// AdMetadata contains selected ad identity and delivery status fields.
type AdMetadata struct {
	// ID is the stable numeric Meta ad ID.
	ID string `json:"id"`
	// Name is the ad name displayed in Ads Manager.
	Name string `json:"name"`
	// Status is the configured ad status.
	Status string `json:"status"`
	// EffectiveStatus is the actual delivery status after parent and review effects.
	EffectiveStatus string `json:"effectiveStatus"`
}

// GetAdOperation reads one ad's identity and delivery status.
type GetAdOperation struct{ client *Client }

// GetAd binds the ad Query to this client.
func (client *Client) GetAd() GetAdOperation { return GetAdOperation{client} }

// Definition returns the manifest-generated ad Query contract.
func (GetAdOperation) Definition() sdkgo.QueryDefinition { return GetAdDefinition }

var numericID = regexp.MustCompile(`^[0-9]{1,40}$`)

// Invoke reads one ad while projecting only selected Marketing API fields.
func (operation GetAdOperation) Invoke(call sdkgo.Call, input GetAdInput) sdkgo.QueryAttempt[AdMetadata] {
	if !numericID.MatchString(input.AdID) {
		return queryBranch("getAd", GetAdBranchDefect, AdMetadata{}, sdkgo.FailureValidation, "ad ID is invalid")
	}
	query := url.Values{"fields": {"id,name,status,effective_status"}}
	response := operation.client.get(call, "/"+input.AdID+"?"+query.Encode())
	if decision, handled := classifyGraphResponse("getAd", response, AdMetadata{}, GetAdBranchNotFound, GetAdBranchProviderRejected, GetAdBranchInvalidResponse, GetAdBranchDefect); handled {
		return decision
	}
	var wire adWire
	if err := json.Unmarshal(response.body, &wire); err != nil {
		return queryBranch("getAd", GetAdBranchInvalidResponse, AdMetadata{}, sdkgo.FailureProtocol, "Meta returned invalid ad JSON")
	}
	metadata, valid := projectAd(wire, input.AdID)
	if !valid {
		return queryBranch("getAd", GetAdBranchInvalidResponse, AdMetadata{}, sdkgo.FailureProtocol, "Meta returned incomplete ad metadata")
	}
	return sdkgo.NewQueryBranch(GetAdBranchFound, metadata, nil, sdkgo.Receipt{})
}

type adWire struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Status          string `json:"status"`
	EffectiveStatus string `json:"effective_status"`
}

func projectAd(wire adWire, requestedID string) (AdMetadata, bool) {
	if wire.ID != requestedID || wire.Name == "" || wire.Status == "" || wire.EffectiveStatus == "" {
		return AdMetadata{}, false
	}
	return AdMetadata{ID: wire.ID, Name: wire.Name, Status: wire.Status, EffectiveStatus: wire.EffectiveStatus}, true
}

func classifyGraphResponse[Output any](operation string, response graphResponse, empty Output, notFound, rejected, invalid, defect sdkgo.BranchID) (sdkgo.QueryAttempt[Output], bool) {
	if errors.Is(response.err, errCredential) {
		return queryBranch(operation, defect, empty, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid"), true
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[Output](queryFailure(operation, sdkgo.FailureTransport, "Meta request failed"), 0), true
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[Output](queryFailure(operation, sdkgo.FailureRateLimit, "Meta rate limited the request"), response.retryAfter), true
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[Output](queryFailure(operation, sdkgo.FailureAvailability, "Meta is temporarily unavailable"), response.retryAfter), true
	}
	if graphErr, ok := decodeGraphError(response.body); response.status >= 400 && response.err == nil && ok {
		if graphErr.isThrottled() {
			return sdkgo.NewQueryRetry[Output](queryFailure(operation, sdkgo.FailureRateLimit, "Meta rate limited the request"), response.retryAfter), true
		}
		if graphErr.isTransient {
			return sdkgo.NewQueryRetry[Output](queryFailure(operation, sdkgo.FailureAvailability, "Meta reported a transient error"), response.retryAfter), true
		}
	}
	if response.status == http.StatusNotFound {
		return queryBranch(operation, notFound, empty, sdkgo.FailureNotFound, "Meta did not find the resource"), true
	}
	if response.status != http.StatusOK {
		return queryBranch(operation, rejected, empty, sdkgo.FailureProviderRejection, "Meta rejected HTTP "+strconv.Itoa(response.status)), true
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return queryBranch(operation, invalid, empty, sdkgo.FailureResponseTooLarge, "Meta response exceeded the configured limit"), true
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[Output](queryFailure(operation, sdkgo.FailureTransport, "Meta response was interrupted"), 0), true
	}
	return sdkgo.QueryAttempt[Output]{}, false
}

func queryBranch[Output any](operation string, branch sdkgo.BranchID, value Output, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[Output] {
	failure := queryFailure(operation, kind, message)
	return sdkgo.NewQueryBranch(branch, value, &failure, sdkgo.Receipt{})
}

func queryFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "meta-ads-insights", Operation: operation, Message: message}
}
