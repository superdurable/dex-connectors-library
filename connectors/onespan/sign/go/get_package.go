// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package onespansign

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetPackageInput selects a package by its non-secret ID.
type GetPackageInput struct {
	// PackageID is the ID returned by OneSpan Sign when a package is created.
	PackageID string `json:"packageId"`
}

// PackageStatus excludes signer identities and document contents.
type PackageStatus struct {
	// ID is the OneSpan Sign package ID.
	ID string `json:"id"`
	// Name is the package name.
	Name string `json:"name"`
	// Status is a OneSpan Sign package state such as COMPLETED.
	Status string `json:"status"`
}

// GetPackageOperation reads one package's identity and state.
type GetPackageOperation struct{ client *Client }

// GetPackage binds the Query to this client.
func (client *Client) GetPackage() GetPackageOperation { return GetPackageOperation{client} }

// Definition returns the manifest-generated Query contract.
func (GetPackageOperation) Definition() sdkgo.QueryDefinition { return GetPackageDefinition }

// Invoke reads one package and classifies retryable and definitive outcomes.
func (operation GetPackageOperation) Invoke(call sdkgo.Call, input GetPackageInput) sdkgo.QueryAttempt[PackageStatus] {
	if !validPackageID(input.PackageID) {
		return packageBranch(GetPackageBranchDefect, sdkgo.FailureValidation, "package ID must be 1–256 printable characters")
	}
	response := operation.client.get(call, "/packages/"+url.PathEscape(input.PackageID))
	if errors.Is(response.err, errCredential) {
		return packageBranch(GetPackageBranchDefect, sdkgo.FailureAuthentication, "OneSpan Sign credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[PackageStatus](packageFailure(sdkgo.FailureTransport, "OneSpan Sign request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[PackageStatus](packageFailure(sdkgo.FailureRateLimit, "OneSpan Sign rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[PackageStatus](packageFailure(sdkgo.FailureAvailability, "OneSpan Sign is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return packageBranch(GetPackageBranchNotFound, sdkgo.FailureNotFound, "OneSpan Sign did not expose the package")
	}
	if response.status != http.StatusOK {
		return packageBranch(GetPackageBranchProviderRejected, sdkgo.FailureProviderRejection, "OneSpan Sign rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return packageBranch(GetPackageBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "OneSpan Sign response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[PackageStatus](packageFailure(sdkgo.FailureTransport, "OneSpan Sign response was interrupted"), 0)
	}
	packageStatus, err := decodePackage(response.body, input.PackageID)
	if err != nil {
		return packageBranch(GetPackageBranchInvalidResponse, sdkgo.FailureProtocol, "OneSpan Sign returned invalid package JSON")
	}
	return sdkgo.NewQueryBranch(GetPackageBranchFound, packageStatus, nil, sdkgo.Receipt{})
}

func validPackageID(packageID string) bool {
	if len(packageID) == 0 || len(packageID) > 256 || !utf8.ValidString(packageID) {
		return false
	}
	for _, character := range packageID {
		if unicode.IsControl(character) || character == '/' || character == '?' || character == '#' {
			return false
		}
	}
	return true
}

func decodePackage(body []byte, requestedPackageID string) (PackageStatus, error) {
	var wire struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return PackageStatus{}, err
	}
	if wire.ID != requestedPackageID || strings.TrimSpace(wire.Name) == "" || strings.TrimSpace(wire.Status) == "" {
		return PackageStatus{}, fmt.Errorf("package identity or status missing")
	}
	return PackageStatus{ID: wire.ID, Name: wire.Name, Status: wire.Status}, nil
}

func packageFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "onespan-sign", Operation: "getPackage", Message: message}
}

func packageBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[PackageStatus] {
	failure := packageFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, PackageStatus{}, &failure, sdkgo.Receipt{})
}
