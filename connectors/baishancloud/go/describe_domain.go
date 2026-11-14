// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package baishancloud

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

var domainNamePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]*[a-z0-9])?)+$`)

// DescribeDomainInput identifies one CDN domain by name.
type DescribeDomainInput struct {
	// Domain is an ASCII DNS name, without protocol or path.
	Domain string `json:"domain"`
}

// DomainDetail is a CDN domain without its origin or cache configuration.
type DomainDetail struct {
	// ID is the BaishanCloud domain identifier.
	ID string `json:"id"`
	// Domain is the CDN domain name.
	Domain string `json:"domain"`
	// Type is the acceleration service type.
	Type string `json:"type"`
	// Status is the provider's service status.
	Status string `json:"status"`
	// CNAME is the DNS alias assigned by BaishanCloud.
	CNAME string `json:"cname"`
}

// DescribeDomainOperation reads one CDN domain through BaishanCloud v2.
type DescribeDomainOperation struct{ client *Client }

// Definition returns the generated domain-detail contract.
func (DescribeDomainOperation) Definition() sdkgo.QueryDefinition { return DescribeDomainDefinition }

// Invoke reads one domain and classifies provider outcomes.
func (operation DescribeDomainOperation) Invoke(call sdkgo.Call, input DescribeDomainInput) sdkgo.QueryAttempt[DomainDetail] {
	domain := strings.ToLower(strings.TrimSpace(input.Domain))
	if len(domain) > 253 || !domainNamePattern.MatchString(domain) {
		return domainBranch(DescribeDomainBranchDefect, sdkgo.FailureValidation, "BaishanCloud domain name is invalid")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !validToken(credentials.Token.Reveal()) {
		return domainBranch(DescribeDomainBranchDefect, sdkgo.FailureAuthentication, "BaishanCloud token is unavailable or invalid")
	}
	// BaishanCloud GET APIs require token in the query: https://portal.baishancloud.com/track/document/api/1/1217
	query := url.Values{"token": {credentials.Token.Reveal()}, "domains": {domain}}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.baseURL+"/v2/domain?"+query.Encode(), nil)
	if err != nil {
		return domainBranch(DescribeDomainBranchDefect, sdkgo.FailureValidation, "BaishanCloud domain request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return domainRetry(sdkgo.FailureAvailability, "BaishanCloud domain request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if retryAfter == 0 {
		retryAfter = providerhttp.ParseRetryAfter(response.Header.Get("X-Retry-After"), time.Now())
	}
	if response.StatusCode == http.StatusTooManyRequests {
		return domainRetry(sdkgo.FailureRateLimit, "BaishanCloud rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return domainRetry(sdkgo.FailureAvailability, "BaishanCloud is temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return domainBranch(DescribeDomainBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "BaishanCloud response exceeded the configured limit")
	}
	if err != nil {
		return domainRetry(sdkgo.FailureAvailability, "BaishanCloud response was interrupted", 0)
	}
	if response.StatusCode != 200 {
		return domainBranch(DescribeDomainBranchProviderRejected, classifyDomainHTTP(response.StatusCode), "BaishanCloud rejected the query with HTTP "+strconv.Itoa(response.StatusCode))
	}
	detail, found, code, err := decodeDomainDetail(body, domain)
	if err != nil {
		return domainBranch(DescribeDomainBranchInvalidResponse, sdkgo.FailureProtocol, "BaishanCloud returned an invalid domain response")
	}
	if code != 0 {
		return domainBranch(DescribeDomainBranchProviderRejected, sdkgo.FailureProviderRejection, "BaishanCloud rejected the query with code "+strconv.Itoa(code))
	}
	if !found {
		return domainBranch(DescribeDomainBranchNotFound, sdkgo.FailureNotFound, "BaishanCloud domain was not found")
	}
	return sdkgo.NewQueryBranch(DescribeDomainBranchFound, detail, nil, sdkgo.Receipt{})
}
func decodeDomainDetail(body []byte, requested string) (DomainDetail, bool, int, error) {
	var wire struct {
		Code *int           `json:"code"`
		Data []DomainDetail `json:"data"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Code == nil {
		return DomainDetail{}, false, 0, errors.New("invalid BaishanCloud JSON")
	}
	if *wire.Code != 0 {
		return DomainDetail{}, false, *wire.Code, nil
	}
	if wire.Data == nil || len(wire.Data) > 1 {
		return DomainDetail{}, false, 0, errors.New("invalid domain list")
	}
	if len(wire.Data) == 0 {
		return DomainDetail{}, false, 0, nil
	}
	detail := wire.Data[0]
	if detail.ID == "" || !strings.EqualFold(detail.Domain, requested) || detail.Type == "" || detail.Status == "" || detail.CNAME == "" {
		return DomainDetail{}, false, 0, errors.New("invalid domain detail")
	}
	return detail, true, 0, nil
}
func validToken(value string) bool {
	if value == "" || len(value) > 512 || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character <= 0x20 || character >= 0x7f {
			return false
		}
	}
	return true
}
func classifyDomainHTTP(status int) sdkgo.FailureKind {
	switch status {
	case 401:
		return sdkgo.FailureAuthentication
	case 403:
		return sdkgo.FailureAuthorization
	case 400, 404:
		return sdkgo.FailureValidation
	default:
		return sdkgo.FailureProviderRejection
	}
}
func domainBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[DomainDetail] {
	failure := sdkgo.Failure{Kind: kind, Provider: "baishancloud", Operation: "describeDomain", Message: message}
	return sdkgo.NewQueryBranch(branch, DomainDetail{}, &failure, sdkgo.Receipt{})
}
func domainRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[DomainDetail] {
	failure := sdkgo.Failure{Kind: kind, Provider: "baishancloud", Operation: "describeDomain", Message: message}
	return sdkgo.NewQueryRetry[DomainDetail](failure, delay)
}
