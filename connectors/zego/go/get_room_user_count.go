// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package zego

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetRoomUserCountInput identifies a ZIM room created by the application.
type GetRoomUserCountInput struct {
	// RoomID is the application-assigned ZIM room ID, at most 32 supported ASCII bytes.
	RoomID string `json:"roomId"`
}

// GetRoomUserCountOutput contains only the current member count.
type GetRoomUserCountOutput struct {
	// Count is the current number of users in the room.
	Count int64 `json:"count"`
}

// GetRoomUserCountOperation reads the current room member count.
type GetRoomUserCountOperation struct{ client *Client }

// Definition returns the generated room-count contract.
func (GetRoomUserCountOperation) Definition() sdkgo.QueryDefinition {
	return GetRoomUserCountDefinition
}

// Invoke signs one room query and discards the returned user list.
func (operation GetRoomUserCountOperation) Invoke(call sdkgo.Call, input GetRoomUserCountInput) sdkgo.QueryAttempt[GetRoomUserCountOutput] {
	if !isValidRoomID(input.RoomID) {
		return roomBranch(GetRoomUserCountBranchDefect, sdkgo.FailureValidation, "ZEGO RoomId is invalid")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidProjectCredentials(credentials) {
		return roomBranch(GetRoomUserCountBranchDefect, sdkgo.FailureAuthentication, "ZEGO project credentials are unavailable or invalid")
	}
	nonceBytes := make([]byte, 8)
	if _, err = rand.Read(nonceBytes); err != nil {
		return roomRetry(sdkgo.FailureAvailability, "ZEGO nonce generation failed", 0)
	}
	nonce := hex.EncodeToString(nonceBytes)
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	signature := roomSignature(credentials.AppID, nonce, credentials.ServerSecret.Reveal(), timestamp)
	query := url.Values{"Action": {"QueryRoomUserList"}, "AppId": {credentials.AppID}, "SignatureNonce": {nonce}, "Timestamp": {timestamp}, "SignatureVersion": {"2.0"}, "Signature": {signature}, "RoomId": {input.RoomID}, "Limit": {"1"}}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, roomUserEndpoint+"?"+query.Encode(), nil)
	if err != nil {
		return roomBranch(GetRoomUserCountBranchDefect, sdkgo.FailureValidation, "ZEGO room-count request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return roomRetry(sdkgo.FailureAvailability, "ZEGO room-count request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return roomRetry(sdkgo.FailureRateLimit, "ZEGO rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return roomRetry(sdkgo.FailureAvailability, "ZEGO room service is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return classifyRoomHTTPStatus(response.StatusCode)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return roomBranch(GetRoomUserCountBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "ZEGO response exceeded the configured limit")
	}
	if err != nil {
		return roomRetry(sdkgo.FailureAvailability, "ZEGO response was interrupted", 0)
	}
	var wire struct {
		Code  *int64 `json:"Code"`
		Count *int64 `json:"Count"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Code == nil {
		return roomBranch(GetRoomUserCountBranchInvalidResponse, sdkgo.FailureProtocol, "ZEGO returned invalid room data")
	}
	switch *wire.Code {
	case 0:
	case 660300006:
		return roomRetry(sdkgo.FailureRateLimit, "ZEGO room query rate limit was reached", 0)
	case 660300007, 100000004:
		return roomRetry(sdkgo.FailureAvailability, "ZEGO room query is temporarily unavailable", 0)
	default:
		return roomBranch(GetRoomUserCountBranchProviderRejected, sdkgo.FailureProviderRejection, "ZEGO rejected room query with code "+strconv.FormatInt(*wire.Code, 10))
	}
	if wire.Count == nil || *wire.Count < 0 {
		return roomBranch(GetRoomUserCountBranchInvalidResponse, sdkgo.FailureProtocol, "ZEGO returned incomplete room count")
	}
	return sdkgo.NewQueryBranch(GetRoomUserCountBranchFound, GetRoomUserCountOutput{Count: *wire.Count}, nil, sdkgo.Receipt{})
}
func roomSignature(appID, nonce, secret, timestamp string) string {
	digest := md5.Sum([]byte(appID + nonce + secret + timestamp))
	return hex.EncodeToString(digest[:])
}
func classifyRoomHTTPStatus(status int) sdkgo.QueryAttempt[GetRoomUserCountOutput] {
	kind := sdkgo.FailureProviderRejection
	if status == http.StatusUnauthorized {
		kind = sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		kind = sdkgo.FailureAuthorization
	}
	return roomBranch(GetRoomUserCountBranchProviderRejected, kind, "ZEGO rejected room query with HTTP "+strconv.Itoa(status))
}
func roomBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[GetRoomUserCountOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "zego", Operation: "getRoomUserCount", Message: message}
	return sdkgo.NewQueryBranch(branch, GetRoomUserCountOutput{}, &failure, sdkgo.Receipt{})
}
func roomRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[GetRoomUserCountOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "zego", Operation: "getRoomUserCount", Message: message}
	return sdkgo.NewQueryRetry[GetRoomUserCountOutput](failure, delay)
}
