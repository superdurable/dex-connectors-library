// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package vivo

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetOnlineVersionInput identifies one Android package in vivo App Store.
type GetOnlineVersionInput struct {
	// PackageName is the exact Java package name published in vivo App Store.
	PackageName string `json:"packageName"`
}

// GetOnlineVersionOutput contains published version metadata without app contents.
type GetOnlineVersionOutput struct {
	// PackageName is the requested Android package name.
	PackageName string `json:"packageName"`
	// VersionName is the user-visible published version.
	VersionName string `json:"versionName"`
	// VersionCode is the integer Android version code.
	VersionCode int64 `json:"versionCode"`
}

// GetOnlineVersionOperation queries the currently published app version.
type GetOnlineVersionOperation struct{ client *Client }

// Definition returns the generated online-version Query contract.
func (GetOnlineVersionOperation) Definition() sdkgo.QueryDefinition {
	return GetOnlineVersionDefinition
}

// Invoke signs a timestamped read request and returns only version metadata.
func (operation GetOnlineVersionOperation) Invoke(call sdkgo.Call, input GetOnlineVersionInput) sdkgo.QueryAttempt[GetOnlineVersionOutput] {
	if len(input.PackageName) > 255 || !packageNamePattern.MatchString(input.PackageName) {
		return versionBranch(GetOnlineVersionBranchDefect, sdkgo.FailureValidation, "vivo package name is invalid")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidCredential(credentials.Account.Reveal()) || !isValidCredential(credentials.Secret.Reveal()) {
		return versionBranch(GetOnlineVersionBranchDefect, sdkgo.FailureAuthentication, "vivo developer credentials are unavailable or invalid")
	}
	timestamp := operation.client.clock().UnixMilli()
	payload := struct {
		Account   string `json:"account"`
		Timestamp int64  `json:"timestamp"`
		Sign      string `json:"sign"`
		BizParam  struct {
			PackageName string `json:"packageName"`
		} `json:"bizParam"`
	}{Account: credentials.Account.Reveal(), Timestamp: timestamp}
	payload.BizParam.PackageName = input.PackageName
	payload.Sign = onlineVersionSignature(payload.Account, input.PackageName, timestamp, credentials.Secret.Reveal())
	body, err := json.Marshal(payload)
	if err != nil {
		return versionBranch(GetOnlineVersionBranchDefect, sdkgo.FailureValidation, "vivo request is invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, onlineVersionEndpoint, bytes.NewReader(body))
	if err != nil {
		return versionBranch(GetOnlineVersionBranchDefect, sdkgo.FailureValidation, "vivo request is invalid")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return versionRetry(sdkgo.FailureAvailability, "vivo version request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return versionRetry(sdkgo.FailureRateLimit, "vivo rate limited version request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return versionRetry(sdkgo.FailureAvailability, "vivo API is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return versionBranch(GetOnlineVersionBranchProviderRejected, sdkgo.FailureProviderRejection, "vivo rejected version request with HTTP "+strconv.Itoa(response.StatusCode))
	}
	content, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return versionBranch(GetOnlineVersionBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "vivo response exceeded configured limit")
	}
	if err != nil {
		return versionRetry(sdkgo.FailureAvailability, "vivo response was interrupted", 0)
	}
	var wire struct {
		Code *int `json:"code"`
		Data *struct {
			PackageName string          `json:"packageName"`
			VersionName string          `json:"versionName"`
			VersionCode json.RawMessage `json:"versionCode"`
		} `json:"data"`
	}
	if json.Unmarshal(content, &wire) != nil || wire.Code == nil {
		return versionBranch(GetOnlineVersionBranchInvalidResponse, sdkgo.FailureProtocol, "vivo returned malformed version data")
	}
	switch *wire.Code {
	case 0:
	case 11001, 11002, 11003, 11004:
		return sdkgo.NewQueryBranch(GetOnlineVersionBranchNoData, GetOnlineVersionOutput{PackageName: input.PackageName}, nil, sdkgo.Receipt{})
	case 10003, 10006, 500:
		return versionRetry(sdkgo.FailureAvailability, "vivo reported temporary query failure", 0)
	default:
		return versionBranch(GetOnlineVersionBranchProviderRejected, sdkgo.FailureProviderRejection, "vivo rejected version request with code "+strconv.Itoa(*wire.Code))
	}
	if wire.Data == nil || wire.Data.PackageName != input.PackageName || wire.Data.VersionName == "" {
		return versionBranch(GetOnlineVersionBranchInvalidResponse, sdkgo.FailureProtocol, "vivo returned mismatched version data")
	}
	var code int64
	if len(wire.Data.VersionCode) == 0 {
		return versionBranch(GetOnlineVersionBranchInvalidResponse, sdkgo.FailureProtocol, "vivo omitted version code")
	}
	if wire.Data.VersionCode[0] == '"' {
		var text string
		if json.Unmarshal(wire.Data.VersionCode, &text) != nil {
			return versionBranch(GetOnlineVersionBranchInvalidResponse, sdkgo.FailureProtocol, "vivo returned invalid version code")
		}
		code, err = strconv.ParseInt(text, 10, 64)
	} else {
		err = json.Unmarshal(wire.Data.VersionCode, &code)
	}
	if err != nil || code <= 0 {
		return versionBranch(GetOnlineVersionBranchInvalidResponse, sdkgo.FailureProtocol, "vivo returned invalid version code")
	}
	return sdkgo.NewQueryBranch(GetOnlineVersionBranchFound, GetOnlineVersionOutput{PackageName: input.PackageName, VersionName: wire.Data.VersionName, VersionCode: code}, nil, sdkgo.Receipt{})
}

func onlineVersionSignature(account, packageName string, timestamp int64, secret string) string {
	// The official Java example serializes bizParam as compact JSON before HMAC.
	signatureInput := "account=" + account + "&bizParam={\"packageName\":\"" + packageName + "\"}&timestamp=" + strconv.FormatInt(timestamp, 10)
	digest := hmac.New(sha256.New, []byte(secret))
	// hash.Hash Write cannot fail for an in-memory HMAC.
	_, _ = digest.Write([]byte(signatureInput))
	return hex.EncodeToString(digest.Sum(nil))
}
func versionBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[GetOnlineVersionOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "vivo", Operation: "getOnlineVersion", Message: message}
	return sdkgo.NewQueryBranch(branch, GetOnlineVersionOutput{}, &failure, sdkgo.Receipt{})
}
func versionRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[GetOnlineVersionOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "vivo", Operation: "getOnlineVersion", Message: message}
	return sdkgo.NewQueryRetry[GetOnlineVersionOutput](failure, delay)
}
