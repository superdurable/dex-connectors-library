// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package wapi

import (
	"crypto/md5"
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

// ListVehicleBrandsInput requests the public brand-price list for an authorized app.
type ListVehicleBrandsInput struct{}

// VehicleBrandPrice is one provider brand and maintenance-record query price.
type VehicleBrandPrice struct {
	// ID is the Wapi brand identifier.
	ID string `json:"id"`
	// Name is the display name of the brand.
	Name string `json:"name"`
	// PriceFen is the quoted query price in Chinese fen.
	PriceFen int `json:"priceFen"`
}

// ListVehicleBrandsOutput contains the provider count and at most 200 brands.
type ListVehicleBrandsOutput struct {
	// Total is the number of brands returned by the provider.
	Total int `json:"total"`
	// Brands are the first 200 brand prices.
	Brands []VehicleBrandPrice `json:"brands"`
}

// ListVehicleBrandsOperation reads Wapi vehicle brand prices.
type ListVehicleBrandsOperation struct{ client *Client }

// Definition returns the generated brand-price Query contract.
func (ListVehicleBrandsOperation) Definition() sdkgo.QueryDefinition {
	return ListVehicleBrandsDefinition
}

// Invoke signs and reads the brand list without ordering maintenance records.
func (operation ListVehicleBrandsOperation) Invoke(call sdkgo.Call, _ ListVehicleBrandsInput) sdkgo.QueryAttempt[ListVehicleBrandsOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAppID(credentials.AppID) || !isValidAppSecret(credentials.AppSecret.Reveal()) {
		return brandsBranch(ListVehicleBrandsBranchDefect, sdkgo.FailureAuthentication, "Wapi app credentials are unavailable or invalid")
	}
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	digest := md5.Sum([]byte("appid" + credentials.AppID + "formatjson" + "time" + timestamp + credentials.AppSecret.Reveal()))
	endpoint, err := url.Parse(brandPriceEndpoint)
	if err != nil {
		return brandsBranch(ListVehicleBrandsBranchDefect, sdkgo.FailureValidation, "Wapi endpoint is invalid")
	}
	query := endpoint.Query()
	query.Set("appid", credentials.AppID)
	query.Set("format", "json")
	query.Set("time", timestamp)
	query.Set("sign", hex.EncodeToString(digest[:]))
	endpoint.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return brandsBranch(ListVehicleBrandsBranchDefect, sdkgo.FailureValidation, "Wapi request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return brandsRetry(sdkgo.FailureAvailability, "Wapi request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return brandsRetry(sdkgo.FailureRateLimit, "Wapi rate limited brand request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return brandsRetry(sdkgo.FailureAvailability, "Wapi temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return classifyBrandsHTTPStatus(response.StatusCode)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return brandsBranch(ListVehicleBrandsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Wapi response exceeded configured limit")
	}
	if err != nil {
		return brandsRetry(sdkgo.FailureAvailability, "Wapi response was interrupted", 0)
	}
	var wire struct {
		CodeID string `json:"codeid"`
		Data   []struct {
			ID       string `json:"ppid"`
			Name     string `json:"pp_name"`
			PriceFen int    `json:"pp_price"`
		} `json:"retdata"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.CodeID == "" {
		return brandsBranch(ListVehicleBrandsBranchInvalidResponse, sdkgo.FailureProtocol, "Wapi returned invalid brand data")
	}
	if wire.CodeID == "10021" || wire.CodeID == "10014" {
		return brandsRetry(sdkgo.FailureAvailability, "Wapi returned temporary server error", 0)
	}
	if wire.CodeID != "10000" {
		return brandsBranch(ListVehicleBrandsBranchProviderRejected, sdkgo.FailureProviderRejection, "Wapi rejected brand request with code "+wire.CodeID)
	}
	if wire.Data == nil || len(wire.Data) > 5000 {
		return brandsBranch(ListVehicleBrandsBranchInvalidResponse, sdkgo.FailureProtocol, "Wapi returned invalid brand list")
	}
	brands := make([]VehicleBrandPrice, 0, min(len(wire.Data), 200))
	for _, item := range wire.Data {
		if item.ID == "" || len(item.ID) > 50 || item.Name == "" || len(item.Name) > 256 || item.PriceFen < 0 {
			return brandsBranch(ListVehicleBrandsBranchInvalidResponse, sdkgo.FailureProtocol, "Wapi returned invalid brand price")
		}
		if len(brands) < 200 {
			brands = append(brands, VehicleBrandPrice{ID: item.ID, Name: item.Name, PriceFen: item.PriceFen})
		}
	}
	return sdkgo.NewQueryBranch(ListVehicleBrandsBranchFound, ListVehicleBrandsOutput{Total: len(wire.Data), Brands: brands}, nil, sdkgo.Receipt{})
}
func classifyBrandsHTTPStatus(status int) sdkgo.QueryAttempt[ListVehicleBrandsOutput] {
	kind := sdkgo.FailureProviderRejection
	if status == http.StatusUnauthorized {
		kind = sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		kind = sdkgo.FailureAuthorization
	}
	return brandsBranch(ListVehicleBrandsBranchProviderRejected, kind, "Wapi rejected brand request with HTTP "+strconv.Itoa(status))
}
func brandsBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ListVehicleBrandsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "wapi", Operation: "listVehicleBrands", Message: message}
	return sdkgo.NewQueryBranch(branch, ListVehicleBrandsOutput{}, &failure, sdkgo.Receipt{})
}
func brandsRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ListVehicleBrandsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "wapi", Operation: "listVehicleBrands", Message: message}
	return sdkgo.NewQueryRetry[ListVehicleBrandsOutput](failure, delay)
}
