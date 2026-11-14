// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package tuya

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const emptyBodySHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
const tokenPath = "/v1.0/token?grant_type=1"

var errTuyaTransient = errors.New("transient Tuya response")

// GetDeviceDetailsInput identifies one device linked to the project.
type GetDeviceDetailsInput struct {
	// DeviceID is the non-secret device identifier from the Tuya project Devices tab.
	DeviceID string `json:"deviceId"`
}

// GetDeviceDetailsOutput contains only safe metadata, excluding local_key and status values.
type GetDeviceDetailsOutput struct {
	// DeviceID is the provider's stable device identifier.
	DeviceID string `json:"deviceId"`
	// Name is the device's display name.
	Name string `json:"name"`
	// Online reports current connectivity.
	Online bool `json:"online"`
	// Category is the provider product-category code.
	Category string `json:"category"`
	// ProductID is the provider product identifier.
	ProductID string `json:"productId"`
	// CreatedAtUnix is the first-pair timestamp in Unix seconds.
	CreatedAtUnix int64 `json:"createdAtUnix"`
	// UpdatedAtUnix is the last-update timestamp in Unix seconds.
	UpdatedAtUnix int64 `json:"updatedAtUnix"`
}

// GetDeviceDetailsOperation obtains a simple-mode token and reads one linked device.
type GetDeviceDetailsOperation struct{ client *Client }

// Definition returns the generated device-details contract.
func (GetDeviceDetailsOperation) Definition() sdkgo.QueryDefinition {
	return GetDeviceDetailsDefinition
}

// Invoke signs both GET requests and keeps token, project secret, and device local_key out of durable results.
func (operation GetDeviceDetailsOperation) Invoke(call sdkgo.Call, input GetDeviceDetailsInput) sdkgo.QueryAttempt[GetDeviceDetailsOutput] {
	if !isValidDeviceID(input.DeviceID) {
		return deviceBranch(GetDeviceDetailsBranchDefect, sdkgo.FailureValidation, "deviceId is invalid", GetDeviceDetailsOutput{})
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidCredential(credentials.ClientID.Reveal()) || !isValidCredential(credentials.ClientSecret.Reveal()) {
		return deviceBranch(GetDeviceDetailsBranchDefect, sdkgo.FailureAuthentication, "project credentials are unavailable or invalid", GetDeviceDetailsOutput{})
	}
	tokenBody, tokenStatus, tokenErr := operation.client.signedGET(call, credentials, tokenPath, "")
	if tokenErr != nil {
		return *tokenErr
	}
	if tokenStatus != http.StatusOK {
		return classifyStatus(tokenStatus, "token request")
	}
	token, tokenFailure := decodeToken(tokenBody)
	if tokenFailure != nil {
		return *tokenFailure
	}
	devicePath := "/v1.0/devices/" + input.DeviceID
	deviceBody, deviceStatus, deviceErr := operation.client.signedGET(call, credentials, devicePath, token)
	if deviceErr != nil {
		return *deviceErr
	}
	if deviceStatus != http.StatusOK {
		return classifyStatus(deviceStatus, "device request")
	}
	output, branch, decodeErr := decodeDevice(deviceBody, input.DeviceID)
	if errors.Is(decodeErr, errTuyaTransient) {
		return deviceRetry(sdkgo.FailureAvailability, "Tuya device query is temporarily unavailable", 0)
	}
	if decodeErr != nil {
		return deviceBranch(GetDeviceDetailsBranchInvalidResponse, sdkgo.FailureProtocol, "Tuya returned invalid device metadata", GetDeviceDetailsOutput{})
	}
	if branch == GetDeviceDetailsBranchNotFound {
		return deviceBranch(branch, sdkgo.FailureNotFound, "Tuya did not find the device", GetDeviceDetailsOutput{})
	}
	if branch == GetDeviceDetailsBranchProviderRejected {
		return deviceBranch(branch, sdkgo.FailureProviderRejection, "Tuya rejected the device query", GetDeviceDetailsOutput{})
	}
	return sdkgo.NewQueryBranch(GetDeviceDetailsBranchFound, output, nil, sdkgo.Receipt{})
}
func (client *Client) signedGET(call sdkgo.Call, credentials Credentials, path, accessToken string) ([]byte, int, *sdkgo.QueryAttempt[GetDeviceDetailsOutput]) {
	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		attempt := deviceRetry(sdkgo.FailureAvailability, "request nonce could not be generated", 0)
		return nil, 0, &attempt
	}
	nonce := hex.EncodeToString(nonceBytes)
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, client.endpoint+path, nil)
	if err != nil {
		attempt := deviceBranch(GetDeviceDetailsBranchDefect, sdkgo.FailureValidation, "Tuya request is invalid", GetDeviceDetailsOutput{})
		return nil, 0, &attempt
	}
	request.Header.Set("client_id", credentials.ClientID.Reveal())
	request.Header.Set("t", timestamp)
	request.Header.Set("nonce", nonce)
	request.Header.Set("sign_method", "HMAC-SHA256")
	request.Header.Set("sign", tuyaSignature(credentials.ClientID.Reveal(), credentials.ClientSecret.Reveal(), accessToken, timestamp, nonce, "", path))
	if accessToken != "" {
		request.Header.Set("access_token", accessToken)
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.httpClient.Do(request)
	if err != nil {
		attempt := deviceRetry(sdkgo.FailureAvailability, "Tuya request failed", 0)
		return nil, 0, &attempt
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		attempt := deviceRetry(sdkgo.FailureRateLimit, "Tuya rate limited the request", retryAfter)
		return nil, 0, &attempt
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		attempt := deviceRetry(sdkgo.FailureAvailability, "Tuya is temporarily unavailable", retryAfter)
		return nil, 0, &attempt
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		attempt := deviceBranch(GetDeviceDetailsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Tuya response exceeded the configured limit", GetDeviceDetailsOutput{})
		return nil, 0, &attempt
	}
	if err != nil {
		attempt := deviceRetry(sdkgo.FailureAvailability, "Tuya response was interrupted", 0)
		return nil, 0, &attempt
	}
	return body, response.StatusCode, nil
}
func tuyaSignature(clientID, secret, accessToken, timestamp, nonce, optionalSignatureKey, path string) string {
	stringToSign := "GET\n" + emptyBodySHA256 + "\n" + optionalSignatureKey + "\n" + path
	message := clientID + accessToken + timestamp + nonce + stringToSign
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(message))
	return strings.ToUpper(hex.EncodeToString(mac.Sum(nil)))
}
func decodeToken(body []byte) (string, *sdkgo.QueryAttempt[GetDeviceDetailsOutput]) {
	var wire struct {
		Success *bool `json:"success"`
		Code    int   `json:"code"`
		Result  struct {
			AccessToken string `json:"access_token"`
			ExpireTime  int64  `json:"expire_time"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Success == nil {
		attempt := deviceBranch(GetDeviceDetailsBranchInvalidResponse, sdkgo.FailureProtocol, "Tuya returned an invalid token response", GetDeviceDetailsOutput{})
		return "", &attempt
	}
	if !*wire.Success {
		if wire.Code == 500 || wire.Code == 1010 {
			attempt := deviceRetry(sdkgo.FailureAvailability, "Tuya token request is temporarily unavailable", 0)
			return "", &attempt
		}
		attempt := deviceBranch(GetDeviceDetailsBranchProviderRejected, sdkgo.FailureAuthentication, "Tuya rejected the token request with code "+strconv.Itoa(wire.Code), GetDeviceDetailsOutput{})
		return "", &attempt
	}
	if !isValidCredential(wire.Result.AccessToken) || wire.Result.ExpireTime <= 0 {
		attempt := deviceBranch(GetDeviceDetailsBranchInvalidResponse, sdkgo.FailureProtocol, "Tuya returned an invalid token", GetDeviceDetailsOutput{})
		return "", &attempt
	}
	return wire.Result.AccessToken, nil
}
func decodeDevice(body []byte, requestedID string) (GetDeviceDetailsOutput, sdkgo.BranchID, error) {
	var wire struct {
		Success *bool `json:"success"`
		Code    int   `json:"code"`
		Result  *struct {
			ID         string `json:"id"`
			Name       string `json:"name"`
			Online     *bool  `json:"online"`
			Category   string `json:"category"`
			ProductID  string `json:"product_id"`
			CreateTime int64  `json:"create_time"`
			UpdateTime int64  `json:"update_time"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Success == nil {
		return GetDeviceDetailsOutput{}, "", errors.New("invalid device envelope")
	}
	if !*wire.Success {
		if wire.Code == 500 || wire.Code == 1010 {
			return GetDeviceDetailsOutput{}, "", errTuyaTransient
		}
		if wire.Code == 1000 {
			return GetDeviceDetailsOutput{}, GetDeviceDetailsBranchNotFound, nil
		}
		return GetDeviceDetailsOutput{}, GetDeviceDetailsBranchProviderRejected, nil
	}
	if wire.Result == nil || wire.Result.ID != requestedID || wire.Result.Name == "" || wire.Result.Online == nil || wire.Result.CreateTime < 0 || wire.Result.UpdateTime < 0 {
		return GetDeviceDetailsOutput{}, "", errors.New("invalid device metadata")
	}
	item := wire.Result
	return GetDeviceDetailsOutput{DeviceID: item.ID, Name: item.Name, Online: *item.Online, Category: item.Category, ProductID: item.ProductID, CreatedAtUnix: item.CreateTime, UpdatedAtUnix: item.UpdateTime}, GetDeviceDetailsBranchFound, nil
}
func classifyStatus(status int, phase string) sdkgo.QueryAttempt[GetDeviceDetailsOutput] {
	kind := sdkgo.FailureProviderRejection
	if status == http.StatusUnauthorized {
		kind = sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		kind = sdkgo.FailureAuthorization
	}
	if status == http.StatusNotFound && phase == "device request" {
		return deviceBranch(GetDeviceDetailsBranchNotFound, sdkgo.FailureNotFound, "Tuya did not find the "+phase, GetDeviceDetailsOutput{})
	}
	return deviceBranch(GetDeviceDetailsBranchProviderRejected, kind, "Tuya rejected the "+phase+" with HTTP "+strconv.Itoa(status), GetDeviceDetailsOutput{})
}
func deviceBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string, output GetDeviceDetailsOutput) sdkgo.QueryAttempt[GetDeviceDetailsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "tuya", Operation: "getDeviceDetails", Message: message}
	return sdkgo.NewQueryBranch(branch, output, &failure, sdkgo.Receipt{})
}
func deviceRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[GetDeviceDetailsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "tuya", Operation: "getDeviceDetails", Message: message}
	return sdkgo.NewQueryRetry[GetDeviceDetailsOutput](failure, delay)
}
