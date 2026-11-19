// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package alibaba

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const emptyPayloadSHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
const signedHeaderNames = "host;x-acs-action;x-acs-content-sha256;x-acs-date;x-acs-signature-nonce;x-acs-version"

// DescribeRegionsInput has no business parameters; the connector lists available ECS regions.
type DescribeRegionsInput struct{}

// ECSRegion is a region advertised by ECS.
type ECSRegion struct {
	// RegionID is the canonical ECS region ID.
	RegionID string `json:"regionId"`
	// LocalName is the provider's localized region name.
	LocalName string `json:"localName"`
	// Endpoint is the public ECS API host for the region.
	Endpoint string `json:"endpoint"`
	// Status is available or soldOut.
	Status string `json:"status"`
}

// DescribeRegionsOutput contains the single-page ECS region list.
type DescribeRegionsOutput struct {
	// Regions contains all returned ECS regions, including sold-out entries.
	Regions []ECSRegion `json:"regions"`
}

// DescribeRegionsOperation lists ECS regions.
type DescribeRegionsOperation struct{ client *Client }

// Definition returns the generated ECS regions Query contract.
func (DescribeRegionsOperation) Definition() sdkgo.QueryDefinition { return DescribeRegionsDefinition }

// Invoke signs and makes one read-only ECS DescribeRegions call.
func (operation DescribeRegionsOperation) Invoke(call sdkgo.Call, _ DescribeRegionsInput) sdkgo.QueryAttempt[DescribeRegionsOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidCredential(credentials.AccessKeyID.Reveal()) || !isValidCredential(credentials.AccessKeySecret.Reveal()) {
		return regionsBranch(DescribeRegionsBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	nonceBytes := make([]byte, 16)
	if _, err := io.ReadFull(operation.client.nonceSource, nonceBytes); err != nil {
		return regionsRetry(sdkgo.FailureAvailability, "request nonce unavailable", 0)
	}
	nonce := hex.EncodeToString(nonceBytes)
	requestDate := operation.client.clock().UTC().Format("2006-01-02T15:04:05Z")
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.baseURL+"/", nil)
	if err != nil {
		return regionsBranch(DescribeRegionsBranchDefect, sdkgo.FailureValidation, "ECS request is invalid")
	}
	signature := signACS3Request(credentials.AccessKeySecret.Reveal(), http.MethodGet, operation.client.endpointHost, "DescribeRegions", "", requestDate, nonce)
	request.Header.Set("x-acs-action", "DescribeRegions")
	request.Header.Set("x-acs-content-sha256", emptyPayloadSHA256)
	request.Header.Set("x-acs-date", requestDate)
	request.Header.Set("x-acs-signature-nonce", nonce)
	request.Header.Set("x-acs-version", "2014-05-26")
	request.Header.Set("Authorization", "ACS3-HMAC-SHA256 Credential="+credentials.AccessKeyID.Reveal()+",SignedHeaders="+signedHeaderNames+",Signature="+signature)
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return regionsRetry(sdkgo.FailureAvailability, "ECS request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return regionsRetry(sdkgo.FailureRateLimit, "ECS rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return regionsRetry(sdkgo.FailureAvailability, "ECS is temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return regionsBranch(DescribeRegionsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "ECS response exceeded the configured limit")
	}
	if err != nil {
		return regionsRetry(sdkgo.FailureAvailability, "ECS response was interrupted", 0)
	}
	var wire struct {
		Code    string `json:"Code"`
		Regions *struct {
			Region []struct {
				RegionID  string `json:"RegionId"`
				LocalName string `json:"LocalName"`
				Endpoint  string `json:"RegionEndpoint"`
				Status    string `json:"Status"`
			} `json:"Region"`
		} `json:"Regions"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return regionsBranch(DescribeRegionsBranchInvalidResponse, sdkgo.FailureProtocol, "ECS returned invalid JSON")
	}
	if strings.HasPrefix(wire.Code, "Throttling") {
		return regionsRetry(sdkgo.FailureRateLimit, "ECS rate limited the request", retryAfter)
	}
	if wire.Code != "" || response.StatusCode != http.StatusOK {
		return regionsBranch(DescribeRegionsBranchProviderRejected, sdkgo.FailureProviderRejection, "ECS rejected the regions request")
	}
	if wire.Regions == nil || wire.Regions.Region == nil {
		return regionsBranch(DescribeRegionsBranchInvalidResponse, sdkgo.FailureProtocol, "ECS omitted its regions list")
	}
	output := DescribeRegionsOutput{Regions: make([]ECSRegion, 0, len(wire.Regions.Region))}
	for _, region := range wire.Regions.Region {
		if region.RegionID == "" || region.LocalName == "" || region.Endpoint == "" || (region.Status != "available" && region.Status != "soldOut") {
			return regionsBranch(DescribeRegionsBranchInvalidResponse, sdkgo.FailureProtocol, "ECS returned an incomplete region")
		}
		output.Regions = append(output.Regions, ECSRegion{RegionID: region.RegionID, LocalName: region.LocalName, Endpoint: region.Endpoint, Status: region.Status})
	}
	return sdkgo.NewQueryBranch(DescribeRegionsBranchFetched, output, nil, sdkgo.Receipt{})
}

func signACS3Request(secret, method, host, action, canonicalQuery, requestDate, nonce string) string {
	canonicalHeaders := "host:" + host + "\n" +
		"x-acs-action:" + action + "\n" +
		"x-acs-content-sha256:" + emptyPayloadSHA256 + "\n" +
		"x-acs-date:" + requestDate + "\n" +
		"x-acs-signature-nonce:" + nonce + "\n" +
		"x-acs-version:2014-05-26\n"
	canonicalRequest := method + "\n/\n" + canonicalQuery + "\n" + canonicalHeaders + "\n" + signedHeaderNames + "\n" + emptyPayloadSHA256
	digest := sha256.Sum256([]byte(canonicalRequest))
	stringToSign := "ACS3-HMAC-SHA256\n" + hex.EncodeToString(digest[:])
	signature := hmac.New(sha256.New, []byte(secret))
	_, _ = signature.Write([]byte(stringToSign))
	return hex.EncodeToString(signature.Sum(nil))
}
func isValidCredential(value string) bool {
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
func regionsBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[DescribeRegionsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "alibaba", Operation: "describeRegions", Message: message}
	return sdkgo.NewQueryBranch(branch, DescribeRegionsOutput{}, &failure, sdkgo.Receipt{})
}
func regionsRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[DescribeRegionsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "alibaba", Operation: "describeRegions", Message: message}
	return sdkgo.NewQueryRetry[DescribeRegionsOutput](failure, delay)
}
