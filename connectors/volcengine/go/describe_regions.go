// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package volcengine

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// DescribeRegionsInput selects one ECS region page. MaxResults zero uses the provider default of 10.
type DescribeRegionsInput struct {
	// MaxResults is the requested page size from 1 to 20; zero uses 10.
	MaxResults int `json:"maxResults,omitempty"`
	// NextToken is the opaque pagination token from a previous response; empty starts at the first page.
	NextToken string `json:"nextToken,omitempty"`
}

// DescribeRegionsOutput contains one page of region IDs and an optional continuation token.
type DescribeRegionsOutput struct {
	// RegionIDs are ECS region IDs without account resource details.
	RegionIDs []string `json:"regionIds"`
	// NextToken is passed as the next call's NextToken; empty means the last page.
	NextToken string `json:"nextToken"`
}

// DescribeRegionsOperation reads one bounded ECS region page.
type DescribeRegionsOperation struct{ client *Client }

// Definition returns the generated DescribeRegions contract.
func (DescribeRegionsOperation) Definition() sdkgo.QueryDefinition { return DescribeRegionsDefinition }

// Invoke signs and sends one ECS DescribeRegions request and classifies provider and transport outcomes.
func (operation DescribeRegionsOperation) Invoke(call sdkgo.Call, input DescribeRegionsInput) sdkgo.QueryAttempt[DescribeRegionsOutput] {
	if input.MaxResults < 0 || input.MaxResults > 20 || len(input.NextToken) > 2048 || strings.TrimSpace(input.NextToken) != input.NextToken {
		return describeRegionsBranch(DescribeRegionsBranchDefect, sdkgo.FailureValidation, "region page size or token is invalid")
	}
	maxResults := input.MaxResults
	if maxResults == 0 {
		maxResults = 10
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAccessKey(credentials.AccessKeyID.Reveal()) || !isValidAccessKey(credentials.SecretAccessKey.Reveal()) {
		return describeRegionsBranch(DescribeRegionsBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	query := url.Values{"Action": {"DescribeRegions"}, "Version": {"2020-04-01"}, "MaxResults": {strconv.Itoa(maxResults)}}
	if input.NextToken != "" {
		query.Set("NextToken", input.NextToken)
	}
	canonicalQuery := canonicalVolcengineQuery(query)
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.baseURL+"/?"+canonicalQuery, nil)
	if err != nil {
		return describeRegionsBranch(DescribeRegionsBranchDefect, sdkgo.FailureValidation, "region request is invalid")
	}
	requestDate := operation.client.now().UTC().Format("20060102T150405Z")
	request.Header.Set("X-Date", requestDate)
	request.Header.Set("Region", operation.client.region)
	request.Header.Set("Service", operation.client.service)
	request.Header.Set("Authorization", signVolcengineRequest(http.MethodGet, "/", canonicalQuery, operation.client.host, requestDate, operation.client.region, operation.client.service, credentials.AccessKeyID.Reveal(), credentials.SecretAccessKey.Reveal()))
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return describeRegionsRetry(sdkgo.FailureAvailability, "ECS request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return describeRegionsRetry(sdkgo.FailureRateLimit, "ECS rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= http.StatusInternalServerError {
		return describeRegionsRetry(sdkgo.FailureAvailability, "ECS is temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return describeRegionsBranch(DescribeRegionsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "ECS response exceeded the configured limit")
	}
	if err != nil {
		return describeRegionsRetry(sdkgo.FailureAvailability, "ECS response was interrupted", 0)
	}
	output, providerError, err := decodeDescribeRegionsResponse(body)
	if err != nil {
		if response.StatusCode != http.StatusOK {
			return describeRegionsBranch(DescribeRegionsBranchProviderRejected, sdkgo.FailureProviderRejection, "ECS rejected the request with HTTP "+strconv.Itoa(response.StatusCode))
		}
		return describeRegionsBranch(DescribeRegionsBranchInvalidResponse, sdkgo.FailureProtocol, "ECS returned an invalid response")
	}
	if response.StatusCode != http.StatusOK || providerError != "" {
		if providerError == "FlowLimitExceeded" {
			return describeRegionsRetry(sdkgo.FailureRateLimit, "ECS rate limited the request", retryAfter)
		}
		if providerError == "InternalError" {
			return describeRegionsRetry(sdkgo.FailureAvailability, "ECS is temporarily unavailable", retryAfter)
		}
		return describeRegionsBranch(DescribeRegionsBranchProviderRejected, classifyVolcengineError(response.StatusCode, providerError), "ECS rejected DescribeRegions")
	}
	return sdkgo.NewQueryBranch(DescribeRegionsBranchListed, output, nil, sdkgo.Receipt{})
}

func decodeDescribeRegionsResponse(body []byte) (DescribeRegionsOutput, string, error) {
	var wire struct {
		ResponseMetadata *struct {
			Error *struct {
				Code string `json:"Code"`
			} `json:"Error"`
		} `json:"ResponseMetadata"`
		Result *struct {
			NextToken string `json:"NextToken"`
			Regions   []struct {
				RegionID string `json:"RegionId"`
			} `json:"Regions"`
		} `json:"Result"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.ResponseMetadata == nil {
		return DescribeRegionsOutput{}, "", errors.New("invalid ECS response envelope")
	}
	if wire.ResponseMetadata.Error != nil {
		if wire.ResponseMetadata.Error.Code == "" {
			return DescribeRegionsOutput{}, "", errors.New("missing ECS error code")
		}
		return DescribeRegionsOutput{}, wire.ResponseMetadata.Error.Code, nil
	}
	if wire.Result == nil || wire.Result.Regions == nil || len(wire.Result.Regions) > 20 {
		return DescribeRegionsOutput{}, "", errors.New("invalid ECS region page")
	}
	output := DescribeRegionsOutput{RegionIDs: make([]string, 0, len(wire.Result.Regions)), NextToken: wire.Result.NextToken}
	for _, region := range wire.Result.Regions {
		if region.RegionID == "" {
			return DescribeRegionsOutput{}, "", errors.New("missing ECS region ID")
		}
		output.RegionIDs = append(output.RegionIDs, region.RegionID)
	}
	return output, "", nil
}

func signVolcengineRequest(method, path, query, host, requestDate, region, service, accessKeyID, secretAccessKey string) string {
	payloadHash := sha256.Sum256(nil)
	canonicalHeaders := "host:" + host + "\n" + "x-date:" + requestDate + "\n"
	canonicalRequest := strings.Join([]string{method, path, query, canonicalHeaders, "host;x-date", hex.EncodeToString(payloadHash[:])}, "\n")
	canonicalHash := sha256.Sum256([]byte(canonicalRequest))
	credentialScope := requestDate[:8] + "/" + region + "/" + service + "/request"
	stringToSign := "HMAC-SHA256\n" + requestDate + "\n" + credentialScope + "\n" + hex.EncodeToString(canonicalHash[:])
	signingKey := hmacSHA256([]byte(secretAccessKey), requestDate[:8])
	signingKey = hmacSHA256(signingKey, region)
	signingKey = hmacSHA256(signingKey, service)
	signingKey = hmacSHA256(signingKey, "request")
	signature := hex.EncodeToString(hmacSHA256(signingKey, stringToSign))
	return "HMAC-SHA256 Credential=" + accessKeyID + "/" + credentialScope + ", SignedHeaders=host;x-date, Signature=" + signature
}
func hmacSHA256(key []byte, message string) []byte {
	hash := hmac.New(sha256.New, key)
	// SHA-256 Hash.Write cannot fail.
	_, _ = hash.Write([]byte(message))
	return hash.Sum(nil)
}
func canonicalVolcengineQuery(query url.Values) string {
	keys := make([]string, 0, len(query))
	for key := range query {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		for _, value := range query[key] {
			pairs = append(pairs, encodeRFC3986(key)+"="+encodeRFC3986(value))
		}
	}
	return strings.Join(pairs, "&")
}
func encodeRFC3986(value string) string {
	return strings.ReplaceAll(url.QueryEscape(value), "+", "%20")
}
func isValidAccessKey(value string) bool {
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
func isValidSigningScope(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-' || character == '_') {
			return false
		}
	}
	return true
}
func classifyVolcengineError(status int, code string) sdkgo.FailureKind {
	if status == http.StatusUnauthorized || strings.Contains(code, "Auth") || strings.Contains(code, "Signature") {
		return sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden || strings.Contains(code, "Permission") {
		return sdkgo.FailureAuthorization
	}
	if status == http.StatusBadRequest {
		return sdkgo.FailureValidation
	}
	return sdkgo.FailureProviderRejection
}
func describeRegionsBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[DescribeRegionsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "volcengine", Operation: "describeRegions", Message: message}
	return sdkgo.NewQueryBranch(branch, DescribeRegionsOutput{RegionIDs: []string{}}, &failure, sdkgo.Receipt{})
}
func describeRegionsRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[DescribeRegionsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "volcengine", Operation: "describeRegions", Message: message}
	return sdkgo.NewQueryRetry[DescribeRegionsOutput](failure, delay)
}
