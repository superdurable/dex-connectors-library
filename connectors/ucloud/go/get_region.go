// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ucloud

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"net/http"
	"sort"
	"strconv"
	"time"
)

// GetRegionInput has no operation parameters; UCloud scopes the response to the API key.
type GetRegionInput struct{}

// RegionInfo identifies one UCloud region and availability zone accessible to the account.
type RegionInfo struct {
	// RegionID is the provider's data-center identifier.
	RegionID int64 `json:"regionId"`
	// RegionName is the data-center display name.
	RegionName string `json:"regionName"`
	// IsDefault reports the account's current default data center.
	IsDefault bool `json:"isDefault"`
	// BitMaps contains the provider's permission bits for this data center.
	BitMaps string `json:"bitMaps"`
	// Region is the provider region code, such as cn-bj.
	Region string `json:"region"`
	// Zone is the provider availability-zone code, such as cn-bj-01.
	Zone string `json:"zone"`
}

// GetRegionOutput contains accessible region and zone entries.
type GetRegionOutput struct {
	// Regions is empty when UCloud returns no entries.
	Regions []RegionInfo `json:"regions"`
}

// GetRegionOperation reads the account's region information.
type GetRegionOperation struct{ client *Client }

// Definition returns the generated GetRegion contract.
func (GetRegionOperation) Definition() sdkgo.QueryDefinition { return GetRegionDefinition }

// Invoke signs and executes GetRegion, then classifies transport and RetCode outcomes.
func (operation GetRegionOperation) Invoke(call sdkgo.Call, _ GetRegionInput) sdkgo.QueryAttempt[GetRegionOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidCredential(credentials.PublicKey.Reveal()) || !isValidCredential(credentials.PrivateKey.Reveal()) {
		return regionBranch(GetRegionBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	publicKey := credentials.PublicKey.Reveal()
	body, err := json.Marshal(map[string]string{"Action": "GetRegion", "PublicKey": publicKey, "Signature": getRegionSignature(publicKey, credentials.PrivateKey.Reveal())})
	if err != nil {
		return regionBranch(GetRegionBranchDefect, sdkgo.FailureValidation, "GetRegion request is invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL, bytes.NewReader(body))
	if err != nil {
		return regionBranch(GetRegionBranchDefect, sdkgo.FailureValidation, "GetRegion request is invalid")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return regionRetry(sdkgo.FailureAvailability, "UCloud request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return regionRetry(sdkgo.FailureRateLimit, "UCloud rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= http.StatusInternalServerError {
		return regionRetry(sdkgo.FailureAvailability, "UCloud is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		kind := sdkgo.FailureProviderRejection
		if response.StatusCode == http.StatusUnauthorized {
			kind = sdkgo.FailureAuthentication
		}
		if response.StatusCode == http.StatusForbidden {
			kind = sdkgo.FailureAuthorization
		}
		return regionBranch(GetRegionBranchProviderRejected, kind, "UCloud rejected the request with HTTP "+strconv.Itoa(response.StatusCode))
	}
	responseBody, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return regionBranch(GetRegionBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "UCloud response exceeded the configured limit")
	}
	if err != nil {
		return regionRetry(sdkgo.FailureAvailability, "UCloud response was interrupted", 0)
	}
	output, retCode, err := decodeGetRegion(responseBody)
	if err != nil {
		return regionBranch(GetRegionBranchInvalidResponse, sdkgo.FailureProtocol, "UCloud returned an invalid GetRegion response")
	}
	switch retCode {
	case 0:
		return sdkgo.NewQueryBranch(GetRegionBranchFound, output, nil, sdkgo.Receipt{})
	case 110, 130, 150:
		return regionRetry(sdkgo.FailureAvailability, "UCloud reported a temporary API failure", retryAfter)
	case 153:
		return regionRetry(sdkgo.FailureRateLimit, "UCloud reported rate limiting", retryAfter)
	case 170, 171, 172, 174:
		return regionBranch(GetRegionBranchProviderRejected, sdkgo.FailureAuthentication, "UCloud rejected the API credentials")
	case 293, 294, 295, 299:
		return regionBranch(GetRegionBranchProviderRejected, sdkgo.FailureAuthorization, "UCloud rejected access to GetRegion")
	default:
		return regionBranch(GetRegionBranchProviderRejected, sdkgo.FailureProviderRejection, "UCloud rejected GetRegion with RetCode "+strconv.Itoa(retCode))
	}
}
func getRegionSignature(publicKey string, privateKey string) string {
	return signUCloudParameters(map[string]string{"Action": "GetRegion", "PublicKey": publicKey}, privateKey)
}

func signUCloudParameters(parameters map[string]string, privateKey string) string {
	parameterNames := make([]string, 0, len(parameters))
	for name := range parameters {
		parameterNames = append(parameterNames, name)
	}
	sort.Strings(parameterNames)
	var signed bytes.Buffer
	for _, name := range parameterNames {
		signed.WriteString(name)
		signed.WriteString(parameters[name])
	}
	signed.WriteString(privateKey)
	sum := sha1.Sum(signed.Bytes())
	return hex.EncodeToString(sum[:])
}
func decodeGetRegion(body []byte) (GetRegionOutput, int, error) {
	var wire struct {
		Action  string `json:"Action"`
		RetCode *int   `json:"RetCode"`
		Regions []struct {
			RegionID   *int64  `json:"RegionId"`
			RegionName string  `json:"RegionName"`
			IsDefault  *bool   `json:"IsDefault"`
			BitMaps    *string `json:"BitMaps"`
			Region     string  `json:"Region"`
			Zone       string  `json:"Zone"`
		} `json:"Regions"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.RetCode == nil || wire.Action != "GetRegionResponse" {
		return GetRegionOutput{}, 0, errors.New("invalid UCloud response envelope")
	}
	if *wire.RetCode != 0 {
		return GetRegionOutput{}, *wire.RetCode, nil
	}
	output := GetRegionOutput{Regions: make([]RegionInfo, 0, len(wire.Regions))}
	for _, item := range wire.Regions {
		if item.RegionID == nil || item.RegionName == "" || item.IsDefault == nil || item.BitMaps == nil || item.Region == "" || item.Zone == "" {
			return GetRegionOutput{}, 0, errors.New("invalid UCloud region")
		}
		output.Regions = append(output.Regions, RegionInfo{RegionID: *item.RegionID, RegionName: item.RegionName, IsDefault: *item.IsDefault, BitMaps: *item.BitMaps, Region: item.Region, Zone: item.Zone})
	}
	return output, 0, nil
}
func regionBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[GetRegionOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "ucloud", Operation: "getRegion", Message: message}
	return sdkgo.NewQueryBranch(branch, GetRegionOutput{Regions: []RegionInfo{}}, &failure, sdkgo.Receipt{})
}
func regionRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[GetRegionOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "ucloud", Operation: "getRegion", Message: message}
	return sdkgo.NewQueryRetry[GetRegionOutput](failure, delay)
}
