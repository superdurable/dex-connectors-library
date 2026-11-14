// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package qiniu

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListBucketsInput has no parameters; Qiniu lists buckets owned by the signing key.
type ListBucketsInput struct{}

// ListBucketsOutput contains the names of buckets owned by the signing account.
type ListBucketsOutput struct {
	// Buckets is empty when the account owns no buckets.
	Buckets []string `json:"buckets"`
}

// ListBucketsOperation reads the bucket list.
type ListBucketsOperation struct{ client *Client }

// Definition returns the generated bucket-list contract.
func (ListBucketsOperation) Definition() sdkgo.QueryDefinition { return ListBucketsDefinition }

// Invoke signs and executes one bucket-list request and classifies provider and transport outcomes.
func (operation ListBucketsOperation) Invoke(call sdkgo.Call, _ ListBucketsInput) sdkgo.QueryAttempt[ListBucketsOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidCredential(credentials.AccessKey.Reveal()) || !isValidCredential(credentials.SecretKey.Reveal()) {
		return bucketBranch(ListBucketsBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.baseURL+"/buckets", nil)
	if err != nil {
		return bucketBranch(ListBucketsBranchDefect, sdkgo.FailureValidation, "bucket-list request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-Qiniu-Date", operation.client.now().UTC().Format(qiniuDateLayout))
	request.Header.Set("Authorization", signBucketRequest(request, credentials.AccessKey.Reveal(), credentials.SecretKey.Reveal()))
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return bucketRetry(sdkgo.FailureAvailability, "Qiniu request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return bucketRetry(sdkgo.FailureRateLimit, "Qiniu rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= http.StatusInternalServerError {
		return bucketRetry(sdkgo.FailureAvailability, "Qiniu is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		failureKind := sdkgo.FailureProviderRejection
		if response.StatusCode == http.StatusUnauthorized {
			failureKind = sdkgo.FailureAuthentication
		}
		if response.StatusCode == http.StatusForbidden {
			failureKind = sdkgo.FailureAuthorization
		}
		return bucketBranch(ListBucketsBranchProviderRejected, failureKind, "Qiniu rejected the request with HTTP "+strconv.Itoa(response.StatusCode))
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return bucketBranch(ListBucketsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Qiniu response exceeded the configured limit")
	}
	if err != nil {
		return bucketRetry(sdkgo.FailureAvailability, "Qiniu response was interrupted", 0)
	}
	var bucketNames []string
	if err := json.Unmarshal(body, &bucketNames); err != nil || bucketNames == nil {
		return bucketBranch(ListBucketsBranchInvalidResponse, sdkgo.FailureProtocol, "Qiniu returned an invalid bucket list")
	}
	for _, bucketName := range bucketNames {
		if bucketName == "" || len(bucketName) > 255 {
			return bucketBranch(ListBucketsBranchInvalidResponse, sdkgo.FailureProtocol, "Qiniu returned an invalid bucket name")
		}
	}
	return sdkgo.NewQueryBranch(ListBucketsBranchListed, ListBucketsOutput{Buckets: bucketNames}, nil, sdkgo.Receipt{})
}

// qiniuDateLayout is the ISO 8601 basic UTC form Qiniu requires for X-Qiniu-Date.
const qiniuDateLayout = "20060102T150405Z"

// signBucketRequest builds the Qiniu management signature for a bodiless request
// without Content-Type; X-Qiniu-Date is the only X-Qiniu-* header sent.
func signBucketRequest(request *http.Request, accessKey string, secretKey string) string {
	canonical := request.Method + " " + request.URL.RequestURI() + "\nHost: " + request.URL.Host + "\nX-Qiniu-Date: " + request.Header.Get("X-Qiniu-Date") + "\n\n"
	mac := hmac.New(sha1.New, []byte(secretKey))
	_, _ = mac.Write([]byte(canonical))
	return "Qiniu " + accessKey + ":" + base64.URLEncoding.EncodeToString(mac.Sum(nil))
}

func bucketBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ListBucketsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "qiniu", Operation: "listBuckets", Message: message}
	return sdkgo.NewQueryBranch(branch, ListBucketsOutput{Buckets: []string{}}, &failure, sdkgo.Receipt{})
}

func bucketRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ListBucketsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "qiniu", Operation: "listBuckets", Message: message}
	return sdkgo.NewQueryRetry[ListBucketsOutput](failure, delay)
}
