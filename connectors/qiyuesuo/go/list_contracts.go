// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package qiyuesuo

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const contractsPath = "/v2/contract/list"

// ListContractsInput selects one zero-based contract window; zero limit uses 20.
type ListContractsInput struct {
	// Offset is the zero-based first contract to request.
	Offset int `json:"offset"`
	// Limit is the number of contracts requested, from 1 to 100.
	Limit int `json:"limit"`
}

// ContractSummary contains only a contract ID and its workflow status.
type ContractSummary struct {
	// ID is the decimal contract ID, encoded as a string to preserve all digits.
	ID string `json:"id"`
	// Status is the documented contract workflow state.
	Status string `json:"status"`
}

// ListContractsOutput contains one contract window, the total count, and the next offset.
type ListContractsOutput struct {
	// Contracts are the returned contract statuses in provider order.
	Contracts []ContractSummary `json:"contracts"`
	// TotalCount is the total matching contract count.
	TotalCount int `json:"totalCount"`
	// NextOffset is the next zero-based offset, or zero if the page is terminal.
	NextOffset int `json:"nextOffset"`
}

// ListContractsOperation reads the Qiyuesuo contract directory.
type ListContractsOperation struct{ client *Client }

// Definition returns the generated contract-status Query contract.
func (ListContractsOperation) Definition() sdkgo.QueryDefinition { return ListContractsDefinition }

// Invoke lists contract statuses and classifies provider and transport outcomes.
func (operation ListContractsOperation) Invoke(call sdkgo.Call, input ListContractsInput) sdkgo.QueryAttempt[ListContractsOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidCredential(credentials.AppToken.Reveal()) || !isValidCredential(credentials.AppSecret.Reveal()) {
		return contractStatusBranch(ListContractsBranchDefect, sdkgo.FailureAuthentication, "Qiyuesuo credentials are unavailable or invalid")
	}
	limit := input.Limit
	if limit == 0 {
		limit = 20
	}
	if input.Offset < 0 || input.Offset > 1000000 || limit < 1 || limit > 100 {
		return contractStatusBranch(ListContractsBranchDefect, sdkgo.FailureValidation, "offset or limit is outside the supported range")
	}
	nonce, err := newRequestNonce()
	if err != nil {
		return contractStatusRetry(sdkgo.FailureAvailability, "request nonce could not be generated", 0)
	}
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	values := url.Values{}
	values.Set("selectOffset", strconv.Itoa(input.Offset))
	values.Set("selectLimit", strconv.Itoa(limit))
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.baseURL+contractsPath+"?"+values.Encode(), nil)
	if err != nil {
		return contractStatusBranch(ListContractsBranchDefect, sdkgo.FailureValidation, "contract-list request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("x-qys-open-accesstoken", credentials.AppToken.Reveal())
	request.Header.Set("x-qys-open-timestamp", timestamp)
	request.Header.Set("x-qys-open-nonce", nonce)
	request.Header.Set("x-qys-open-signature", signRequest(credentials.AppToken.Reveal(), credentials.AppSecret.Reveal(), timestamp, nonce))
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return contractStatusRetry(sdkgo.FailureAvailability, "Qiyuesuo request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return contractStatusRetry(sdkgo.FailureRateLimit, "Qiyuesuo rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return contractStatusRetry(sdkgo.FailureAvailability, "Qiyuesuo temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return contractStatusBranch(ListContractsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "contract response exceeded the configured limit")
	}
	if err != nil {
		return contractStatusRetry(sdkgo.FailureAvailability, "contract response was interrupted", 0)
	}
	if response.StatusCode != http.StatusOK {
		kind := sdkgo.FailureProviderRejection
		if response.StatusCode == http.StatusUnauthorized || response.StatusCode == 440 || response.StatusCode == 441 || response.StatusCode == 442 || response.StatusCode == 446 {
			kind = sdkgo.FailureAuthentication
		}
		if response.StatusCode == http.StatusForbidden || response.StatusCode == 449 {
			kind = sdkgo.FailureAuthorization
		}
		return contractStatusBranch(ListContractsBranchProviderRejected, kind, "Qiyuesuo rejected the request with HTTP "+strconv.Itoa(response.StatusCode))
	}
	var wire struct {
		ResponseCode string `json:"responseCode"`
		Result       *struct {
			TotalCount int `json:"totalCount"`
			List       []struct {
				ID     json.Number `json:"id"`
				Status string      `json:"status"`
			} `json:"list"`
		} `json:"result"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	if decoder.Decode(&wire) != nil || wire.ResponseCode == "" {
		return contractStatusBranch(ListContractsBranchInvalidResponse, sdkgo.FailureProtocol, "Qiyuesuo returned invalid contract data")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return contractStatusBranch(ListContractsBranchInvalidResponse, sdkgo.FailureProtocol, "Qiyuesuo returned trailing contract data")
	}
	if wire.ResponseCode != "00000000" {
		return classifyResponseCode(wire.ResponseCode)
	}
	if wire.Result == nil || wire.Result.List == nil || wire.Result.TotalCount < 0 || len(wire.Result.List) > limit || wire.Result.TotalCount < input.Offset+len(wire.Result.List) {
		return contractStatusBranch(ListContractsBranchInvalidResponse, sdkgo.FailureProtocol, "Qiyuesuo returned invalid contract page")
	}
	contracts := make([]ContractSummary, 0, len(wire.Result.List))
	for _, item := range wire.Result.List {
		id, err := strconv.ParseInt(item.ID.String(), 10, 64)
		if err != nil || id <= 0 || !isContractStatus(item.Status) {
			return contractStatusBranch(ListContractsBranchInvalidResponse, sdkgo.FailureProtocol, "Qiyuesuo returned an invalid contract")
		}
		contracts = append(contracts, ContractSummary{ID: strconv.FormatInt(id, 10), Status: item.Status})
	}
	nextOffset := 0
	if input.Offset+len(contracts) < wire.Result.TotalCount {
		if len(contracts) == 0 || input.Offset+len(contracts) > 1000000 {
			return contractStatusBranch(ListContractsBranchInvalidResponse, sdkgo.FailureProtocol, "Qiyuesuo pagination cannot advance")
		}
		nextOffset = input.Offset + len(contracts)
	}
	return sdkgo.NewQueryBranch(ListContractsBranchListed, ListContractsOutput{Contracts: contracts, TotalCount: wire.Result.TotalCount, NextOffset: nextOffset}, nil, sdkgo.Receipt{})
}
func isValidCredential(value string) bool {
	if value == "" || len(value) > 512 {
		return false
	}
	for _, character := range value {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}
func newRequestNonce() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", bytes[0:4], bytes[4:6], bytes[6:8], bytes[8:10], bytes[10:16]), nil
}
func signRequest(token, secret, timestamp, nonce string) string {
	digest := md5.Sum([]byte(token + secret + timestamp + nonce))
	return hex.EncodeToString(digest[:])
}
func isContractStatus(status string) bool {
	switch status {
	case "DRAFT", "RECALLED", "SIGNING", "REJECTED", "COMPLETE", "EXPIRED", "FILLING", "INVALIDING", "INVALIDED", "FORCE_END":
		return true
	}
	return false
}
func classifyResponseCode(code string) sdkgo.QueryAttempt[ListContractsOutput] {
	switch code {
	case "11990006":
		return contractStatusRetry(sdkgo.FailureRateLimit, "Qiyuesuo rate limited the request", 0)
	case "21990001":
		return contractStatusRetry(sdkgo.FailureAvailability, "Qiyuesuo temporarily unavailable", 0)
	case "11990009", "11990440", "11990441", "11990442", "11990446", "11990448":
		return contractStatusBranch(ListContractsBranchProviderRejected, sdkgo.FailureAuthentication, "Qiyuesuo rejected authentication, code "+code)
	case "14990002", "14990021", "11990403", "11990449":
		return contractStatusBranch(ListContractsBranchProviderRejected, sdkgo.FailureAuthorization, "Qiyuesuo denied access, code "+code)
	default:
		return contractStatusBranch(ListContractsBranchProviderRejected, sdkgo.FailureProviderRejection, "Qiyuesuo rejected the request, code "+code)
	}
}
func contractStatusBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ListContractsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "qiyuesuo", Operation: "listContracts", Message: message}
	return sdkgo.NewQueryBranch(branch, ListContractsOutput{}, &failure, sdkgo.Receipt{})
}
func contractStatusRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ListContractsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "qiyuesuo", Operation: "listContracts", Message: message}
	return sdkgo.NewQueryRetry[ListContractsOutput](failure, delay)
}
