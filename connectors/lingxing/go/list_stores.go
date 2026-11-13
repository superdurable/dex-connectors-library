// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package lingxing

import (
	"crypto/aes"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const maximumReturnedStores = 200

// ListStoresInput requests all stores authorized to the Lingxing account.
type ListStoresInput struct{}

// Store contains selected Amazon store metadata without seller account identifiers.
type Store struct {
	// StoreID is Lingxing's stable seller ID.
	StoreID int64 `json:"storeID"`
	// Name is the configured store display name.
	Name string `json:"name"`
	// Region is the Amazon region abbreviation.
	Region string `json:"region"`
	// Country is the Amazon marketplace country.
	Country string `json:"country"`
	// Status is Lingxing's synchronization state: 0 stopped, 1 normal, 2 authorization error, or 3 payment suspension.
	Status int `json:"status"`
}

// StoreList contains up to 200 authorized stores; Truncated reports additional omitted stores.
type StoreList struct {
	// Stores contains at most 200 stores.
	Stores []Store `json:"stores"`
	// Truncated is true when the provider returned more than 200 stores.
	Truncated bool `json:"truncated"`
}

// ListStoresOperation reads authorized Amazon store metadata.
type ListStoresOperation struct{ client *Client }

// Definition returns the generated store-list Query contract.
func (ListStoresOperation) Definition() sdkgo.QueryDefinition { return ListStoresDefinition }

// Invoke obtains a token, signs a store-list request, and classifies provider outcomes.
func (operation ListStoresOperation) Invoke(call sdkgo.Call, _ ListStoresInput) sdkgo.QueryAttempt[StoreList] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !validAppID(credentials.AppID.Reveal()) || !validCredential(credentials.AppSecret.Reveal()) {
		return storeBranch(ListStoresBranchDefect, sdkgo.FailureAuthentication, "Lingxing AppID or AppSecret is unavailable or invalid")
	}
	token, issue := operation.client.getAccessToken(call, credentials)
	if issue != nil {
		if issue.retry {
			return storeRetry(issue.kind, issue.message, issue.delay)
		}
		return storeBranch(issue.branch, issue.kind, issue.message)
	}
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	appID := credentials.AppID.Reveal()
	signature, err := signStoreRequest(map[string]string{"access_token": token, "app_key": appID, "timestamp": timestamp}, appID)
	if err != nil {
		return storeBranch(ListStoresBranchDefect, sdkgo.FailureValidation, "Lingxing AppID cannot sign requests")
	}
	query := url.Values{"app_key": {appID}, "timestamp": {timestamp}, "sign": {signature}}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.baseURL+"/erp/sc/data/seller/lists?"+query.Encode(), nil)
	if err != nil {
		return storeBranch(ListStoresBranchDefect, sdkgo.FailureValidation, "Lingxing store request is invalid")
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return storeRetry(sdkgo.FailureAvailability, "Lingxing store request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	delay := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return storeRetry(sdkgo.FailureRateLimit, "Lingxing store request was rate limited", delay)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return storeRetry(sdkgo.FailureAvailability, "Lingxing store service is unavailable", delay)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return storeBranch(ListStoresBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Lingxing store response exceeded the configured limit")
	}
	if err != nil {
		return storeRetry(sdkgo.FailureAvailability, "Lingxing store response was interrupted", 0)
	}
	if response.StatusCode != http.StatusOK {
		return storeBranch(ListStoresBranchProviderRejected, classifyStoreHTTP(response.StatusCode), "Lingxing rejected store request with HTTP "+strconv.Itoa(response.StatusCode))
	}
	stores, code, err := decodeStoreList(body)
	if err != nil {
		return storeBranch(ListStoresBranchInvalidResponse, sdkgo.FailureProtocol, "Lingxing returned invalid store data")
	}
	if code == 3001008 {
		return storeRetry(sdkgo.FailureRateLimit, "Lingxing store request was rate limited", 0)
	}
	if code != 0 {
		return storeBranch(ListStoresBranchProviderRejected, sdkgo.FailureProviderRejection, "Lingxing rejected store request")
	}
	return sdkgo.NewQueryBranch(ListStoresBranchListed, stores, nil, sdkgo.Receipt{})
}
func signStoreRequest(parameters map[string]string, appID string) (string, error) {
	block, err := aes.NewCipher([]byte(appID))
	if err != nil {
		return "", err
	}
	names := make([]string, 0, len(parameters))
	for name := range parameters {
		names = append(names, name)
	}
	sort.Strings(names)
	pairs := make([]string, 0, len(names))
	for _, name := range names {
		if parameters[name] != "" {
			pairs = append(pairs, name+"="+parameters[name])
		}
	}
	digest := md5.Sum([]byte(strings.Join(pairs, "&")))
	upper := strings.ToUpper(hex.EncodeToString(digest[:]))
	plain := []byte(upper)
	pad := block.BlockSize() - len(plain)%block.BlockSize()
	for i := 0; i < pad; i++ {
		plain = append(plain, byte(pad))
	}
	encrypted := make([]byte, len(plain))
	for offset := 0; offset < len(plain); offset += block.BlockSize() {
		block.Encrypt(encrypted[offset:offset+block.BlockSize()], plain[offset:offset+block.BlockSize()])
	}
	return base64.StdEncoding.EncodeToString(encrypted), nil
}
func decodeStoreList(body []byte) (StoreList, int, error) {
	var wire struct {
		Code json.RawMessage `json:"code"`
		Data []struct {
			ID      int64  `json:"sid"`
			Name    string `json:"name"`
			Region  string `json:"region"`
			Country string `json:"country"`
			Status  *int   `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return StoreList{}, 0, err
	}
	code, ok := parseProviderCode(wire.Code)
	if !ok {
		return StoreList{}, 0, errors.New("invalid Lingxing status")
	}
	if code != 0 {
		return StoreList{}, code, nil
	}
	if wire.Data == nil {
		return StoreList{}, 0, errors.New("missing Lingxing store data")
	}
	output := StoreList{Stores: make([]Store, 0, min(len(wire.Data), maximumReturnedStores)), Truncated: len(wire.Data) > maximumReturnedStores}
	for index, item := range wire.Data {
		if item.ID <= 0 || item.Name == "" || item.Status == nil || *item.Status < 0 || *item.Status > 3 {
			return StoreList{}, 0, errors.New("invalid Lingxing store")
		}
		if index < maximumReturnedStores {
			output.Stores = append(output.Stores, Store{StoreID: item.ID, Name: item.Name, Region: item.Region, Country: item.Country, Status: *item.Status})
		}
	}
	return output, 0, nil
}
func parseProviderCode(raw json.RawMessage) (int, bool) {
	if len(raw) == 0 {
		return 0, false
	}
	var number int
	if err := json.Unmarshal(raw, &number); err == nil {
		return number, true
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return 0, false
	}
	number, err := strconv.Atoi(value)
	return number, err == nil
}
func validAppID(value string) bool {
	return (len(value) == 16 || len(value) == 24 || len(value) == 32) && validCredential(value)
}
func validCredential(value string) bool {
	if value == "" || len(value) > 2048 || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character <= 0x20 || character >= 0x7f {
			return false
		}
	}
	return true
}
func validToken(value string) bool { return validCredential(value) }
func classifyStoreHTTP(status int) sdkgo.FailureKind {
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
func storeBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[StoreList] {
	failure := sdkgo.Failure{Kind: kind, Provider: "lingxing", Operation: "listStores", Message: message}
	return sdkgo.NewQueryBranch(branch, StoreList{}, &failure, sdkgo.Receipt{})
}
func storeRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[StoreList] {
	failure := sdkgo.Failure{Kind: kind, Provider: "lingxing", Operation: "listStores", Message: message}
	return sdkgo.NewQueryRetry[StoreList](failure, delay)
}
