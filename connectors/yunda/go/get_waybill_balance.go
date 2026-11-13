// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package yunda

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetWaybillBalanceInput needs no operation-specific values.
type GetWaybillBalanceInput struct{}

// GetWaybillBalanceOutput contains remaining ordinary Baima waybill counts.
type GetWaybillBalanceOutput struct {
	// FreeCount is the number of free waybills remaining.
	FreeCount int64 `json:"freeCount"`
	// PaidCount is the number of paid waybills remaining.
	PaidCount int64 `json:"paidCount"`
}

// GetWaybillBalanceOperation queries the Baima electronic waybill balance.
type GetWaybillBalanceOperation struct{ client *Client }

// Definition returns the generated waybill balance Query contract.
func (GetWaybillBalanceOperation) Definition() sdkgo.QueryDefinition {
	return GetWaybillBalanceDefinition
}

// Invoke sends a signed read request and returns only remaining counts.
func (operation GetWaybillBalanceOperation) Invoke(call sdkgo.Call, _ GetWaybillBalanceInput) sdkgo.QueryAttempt[GetWaybillBalanceOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidCredential(credentials.AppKey.Reveal()) || !isValidCredential(credentials.AppSecret.Reveal()) || !isValidCredential(credentials.PartnerID.Reveal()) || !isValidCredential(credentials.BaimaSecret.Reveal()) {
		return balanceBranch(GetWaybillBalanceBranchDefect, sdkgo.FailureAuthentication, "Yunda application or Baima credentials are unavailable or invalid")
	}
	body, err := json.Marshal(struct {
		AppID     string `json:"appid"`
		PartnerID string `json:"partner_id"`
		Secret    string `json:"secret"`
		Type      string `json:"type"`
	}{AppID: credentials.AppKey.Reveal(), PartnerID: credentials.PartnerID.Reveal(), Secret: credentials.BaimaSecret.Reveal(), Type: "common"})
	if err != nil {
		return balanceBranch(GetWaybillBalanceBranchDefect, sdkgo.FailureValidation, "Yunda balance request is invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, waybillBalanceEndpoint, bytes.NewReader(body))
	if err != nil {
		return balanceBranch(GetWaybillBalanceBranchDefect, sdkgo.FailureValidation, "Yunda balance request is invalid")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("app-key", credentials.AppKey.Reveal())
	request.Header.Set("sign", waybillBalanceSignature(body, credentials.AppSecret.Reveal()))
	request.Header.Set("req-time", strconv.FormatInt(operation.client.clock().UnixMilli(), 10))
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return balanceRetry(sdkgo.FailureAvailability, "Yunda balance request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryDelay := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return balanceRetry(sdkgo.FailureRateLimit, "Yunda rate limited the balance request", retryDelay)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return balanceRetry(sdkgo.FailureAvailability, "Yunda balance API is temporarily unavailable", retryDelay)
	}
	if response.StatusCode != http.StatusOK {
		return balanceBranch(GetWaybillBalanceBranchProviderRejected, sdkgo.FailureProviderRejection, "Yunda rejected balance request with HTTP "+strconv.Itoa(response.StatusCode))
	}
	content, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return balanceBranch(GetWaybillBalanceBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Yunda balance response exceeded configured limit")
	}
	if err != nil {
		return balanceRetry(sdkgo.FailureAvailability, "Yunda balance response was interrupted", 0)
	}
	var wire struct {
		Code   *string `json:"code"`
		Result *bool   `json:"result"`
		Data   *struct {
			Status    string `json:"status"`
			FreeCount string `json:"free_num"`
			PaidCount string `json:"remain_num"`
		} `json:"data"`
	}
	if json.Unmarshal(content, &wire) != nil || wire.Code == nil || wire.Result == nil {
		return balanceBranch(GetWaybillBalanceBranchInvalidResponse, sdkgo.FailureProtocol, "Yunda returned malformed balance data")
	}
	if *wire.Code == "7777" {
		return balanceRetry(sdkgo.FailureAvailability, "Yunda reported internal balance failure", retryDelay)
	}
	if *wire.Code != "0000" || !*wire.Result {
		return balanceBranch(GetWaybillBalanceBranchProviderRejected, sdkgo.FailureProviderRejection, "Yunda rejected the balance request")
	}
	if wire.Data == nil || wire.Data.Status != "1" {
		return balanceBranch(GetWaybillBalanceBranchInvalidResponse, sdkgo.FailureProtocol, "Yunda omitted a successful Baima balance")
	}
	freeCount, freeErr := parseWaybillCount(wire.Data.FreeCount)
	paidCount, paidErr := parseWaybillCount(wire.Data.PaidCount)
	if freeErr != nil || paidErr != nil {
		return balanceBranch(GetWaybillBalanceBranchInvalidResponse, sdkgo.FailureProtocol, "Yunda returned invalid waybill counts")
	}
	return sdkgo.NewQueryBranch(GetWaybillBalanceBranchFound, GetWaybillBalanceOutput{FreeCount: freeCount, PaidCount: paidCount}, nil, sdkgo.Receipt{})
}

func waybillBalanceSignature(body []byte, appSecret string) string {
	signedBytes := append(append([]byte{}, body...), '_')
	signedBytes = append(signedBytes, appSecret...)
	digest := md5.Sum(signedBytes)
	return hex.EncodeToString(digest[:])
}

func parseWaybillCount(value string) (int64, error) {
	if value == "" || len(value) > 19 {
		return 0, errors.New("invalid waybill count")
	}
	count, err := strconv.ParseInt(value, 10, 64)
	if err != nil || count < 0 {
		return 0, errors.New("invalid waybill count")
	}
	return count, nil
}

func balanceBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[GetWaybillBalanceOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "yunda", Operation: "getWaybillBalance", Message: message}
	return sdkgo.NewQueryBranch(branch, GetWaybillBalanceOutput{}, &failure, sdkgo.Receipt{})
}
func balanceRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[GetWaybillBalanceOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "yunda", Operation: "getWaybillBalance", Message: message}
	return sdkgo.NewQueryRetry[GetWaybillBalanceOutput](failure, delay)
}
