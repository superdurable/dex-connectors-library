// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package threatbook

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// CheckIPReputationInput identifies one inbound IPv4 or IPv6 source address.
type CheckIPReputationInput struct {
	// IPAddress is a single IPv4 or IPv6 address from an inbound access event.
	IPAddress string `json:"ipAddress"`
}

// CheckIPReputationOutput contains only the main verdict and limited threat labels.
type CheckIPReputationOutput struct {
	// IsMalicious is ThreatBook's inbound-risk verdict.
	IsMalicious bool `json:"isMalicious"`
	// ConfidenceLevel is low, medium, or high when supplied.
	ConfidenceLevel string `json:"confidenceLevel"`
	// Severity is critical, high, medium, low, or info when supplied.
	Severity string `json:"severity"`
	// Judgments are at most twenty threat-type labels.
	Judgments []string `json:"judgments"`
}

// CheckIPReputationOperation reads an inbound IP risk verdict without modifying provider state.
type CheckIPReputationOperation struct{ client *Client }

// Definition returns the generated IP reputation contract.
func (CheckIPReputationOperation) Definition() sdkgo.QueryDefinition {
	return CheckIPReputationDefinition
}

// Invoke queries one IP and excludes the query-string Key from failures and results.
func (operation CheckIPReputationOperation) Invoke(call sdkgo.Call, input CheckIPReputationInput) sdkgo.QueryAttempt[CheckIPReputationOutput] {
	parsed := net.ParseIP(input.IPAddress)
	if parsed == nil {
		return reputationBranch(CheckIPReputationBranchDefect, sdkgo.FailureValidation, "ThreatBook requires one valid IP address")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAPIKey(credentials.APIKey.Reveal()) {
		return reputationBranch(CheckIPReputationBranchDefect, sdkgo.FailureAuthentication, "ThreatBook API Key is unavailable or invalid")
	}
	query := url.Values{"apikey": {credentials.APIKey.Reveal()}, "resource": {parsed.String()}, "lang": {"en"}}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, ipReputationEndpoint+"?"+query.Encode(), nil)
	if err != nil {
		return reputationBranch(CheckIPReputationBranchDefect, sdkgo.FailureValidation, "ThreatBook reputation request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return reputationRetry(sdkgo.FailureAvailability, "ThreatBook reputation request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return reputationRetry(sdkgo.FailureRateLimit, "ThreatBook rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return reputationRetry(sdkgo.FailureAvailability, "ThreatBook reputation service is temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return reputationBranch(CheckIPReputationBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "ThreatBook response exceeded the configured limit")
	}
	if err != nil {
		return reputationRetry(sdkgo.FailureAvailability, "ThreatBook response was interrupted", 0)
	}
	var wire struct {
		ResponseCode   *int   `json:"response_code"`
		VerboseMessage string `json:"verbose_msg"`
		Data           map[string]struct {
			IsMalicious     *bool    `json:"is_malicious"`
			ConfidenceLevel string   `json:"confidence_level"`
			Severity        string   `json:"severity"`
			Judgments       []string `json:"judgments"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &wire) == nil && wire.ResponseCode != nil {
		switch *wire.ResponseCode {
		case 0:
		case 2:
			return reputationBranch(CheckIPReputationBranchNoData, sdkgo.FailureNotFound, "ThreatBook has no intelligence for this IP")
		case -4:
			if wire.VerboseMessage == "Frequent Limitation" {
				return reputationRetry(sdkgo.FailureRateLimit, "ThreatBook frequency limit reached", retryAfter)
			}
			return reputationBranch(CheckIPReputationBranchProviderRejected, sdkgo.FailureProviderRejection, "ThreatBook quota exhausted")
		case -5, -11:
			return reputationRetry(sdkgo.FailureAvailability, "ThreatBook reputation service is temporarily unavailable", retryAfter)
		default:
			return reputationBranch(CheckIPReputationBranchProviderRejected, sdkgo.FailureProviderRejection, "ThreatBook rejected reputation request with code "+strconv.Itoa(*wire.ResponseCode))
		}
		if response.StatusCode != http.StatusOK {
			return reputationBranch(CheckIPReputationBranchInvalidResponse, sdkgo.FailureProtocol, "ThreatBook returned success with unexpected HTTP status")
		}
		if len(wire.Data) != 1 {
			return reputationBranch(CheckIPReputationBranchInvalidResponse, sdkgo.FailureProtocol, "ThreatBook returned invalid IP reputation data")
		}
		for address, info := range wire.Data {
			if net.ParseIP(address) == nil || !net.ParseIP(address).Equal(parsed) || info.IsMalicious == nil || len(info.Judgments) > 20 || len(info.ConfidenceLevel) > 16 || len(info.Severity) > 16 {
				return reputationBranch(CheckIPReputationBranchInvalidResponse, sdkgo.FailureProtocol, "ThreatBook returned invalid IP reputation data")
			}
			for _, judgment := range info.Judgments {
				if judgment == "" || len(judgment) > 80 {
					return reputationBranch(CheckIPReputationBranchInvalidResponse, sdkgo.FailureProtocol, "ThreatBook returned invalid threat labels")
				}
			}
			return sdkgo.NewQueryBranch(CheckIPReputationBranchFound, CheckIPReputationOutput{IsMalicious: *info.IsMalicious, ConfidenceLevel: info.ConfidenceLevel, Severity: info.Severity, Judgments: info.Judgments}, nil, sdkgo.Receipt{})
		}
	}
	if response.StatusCode != http.StatusOK {
		return classifyReputationHTTPStatus(response.StatusCode)
	}
	return reputationBranch(CheckIPReputationBranchInvalidResponse, sdkgo.FailureProtocol, "ThreatBook returned invalid IP reputation data")
}
func classifyReputationHTTPStatus(status int) sdkgo.QueryAttempt[CheckIPReputationOutput] {
	kind := sdkgo.FailureProviderRejection
	if status == http.StatusUnauthorized {
		kind = sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		kind = sdkgo.FailureAuthorization
	}
	return reputationBranch(CheckIPReputationBranchProviderRejected, kind, "ThreatBook rejected reputation request with HTTP "+strconv.Itoa(status))
}
func reputationBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[CheckIPReputationOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "threatbook", Operation: "checkIPReputation", Message: message}
	return sdkgo.NewQueryBranch(branch, CheckIPReputationOutput{}, &failure, sdkgo.Receipt{})
}
func reputationRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[CheckIPReputationOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "threatbook", Operation: "checkIPReputation", Message: message}
	return sdkgo.NewQueryRetry[CheckIPReputationOutput](failure, delay)
}
