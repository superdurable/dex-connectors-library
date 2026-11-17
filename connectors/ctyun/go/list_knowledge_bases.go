// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ctyun

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const knowledgeBasesPath = "/openapi/v1/infobases/list"

// ListKnowledgeBasesInput requests the accessible CTYun Knowledge Base Q&A resources.
type ListKnowledgeBasesInput struct{}

// KnowledgeBaseSummary excludes CTYun documents and private account information.
type KnowledgeBaseSummary struct {
	// ID is the provider's integer knowledge-base ID.
	ID int64 `json:"id"`
	// Name is the resource name.
	Name string `json:"name"`
	// FileCount is the number of files in this resource.
	FileCount int `json:"fileCount"`
}

// ListKnowledgeBasesOutput contains accessible knowledge-base resource metadata.
type ListKnowledgeBasesOutput struct {
	// KnowledgeBases are the returned resource summaries, possibly empty.
	KnowledgeBases []KnowledgeBaseSummary `json:"knowledgeBases"`
}

// ListKnowledgeBasesOperation reads CTYun Knowledge Base Q&A metadata.
type ListKnowledgeBasesOperation struct{ client *Client }

// Definition returns the generated knowledge-base-list Query contract.
func (ListKnowledgeBasesOperation) Definition() sdkgo.QueryDefinition {
	return ListKnowledgeBasesDefinition
}

// Invoke signs a read-only CTYun request and filters out document contents.
func (operation ListKnowledgeBasesOperation) Invoke(call sdkgo.Call, _ ListKnowledgeBasesInput) sdkgo.QueryAttempt[ListKnowledgeBasesOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidCredential(credentials.AccessKey.Reveal()) || !isValidCredential(credentials.SecurityKey.Reveal()) || !isValidCredential(credentials.TenantID.Reveal()) {
		return knowledgeBaseBranch(ListKnowledgeBasesBranchDefect, sdkgo.FailureAuthentication, "CTYun credentials or tenant ID are unavailable or invalid")
	}
	requestID, err := operation.client.requestID()
	if err != nil {
		return knowledgeBaseRetry(sdkgo.FailureAvailability, "CTYun request ID could not be generated", 0)
	}
	beijing := time.FixedZone("CST", 8*3600)
	date := operation.client.now().In(beijing).Format("20060102T150405Z")
	signature := signAuthorization(credentials.AccessKey.Reveal(), credentials.SecurityKey.Reveal(), requestID, date)
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.baseURL+knowledgeBasesPath, nil)
	if err != nil {
		return knowledgeBaseBranch(ListKnowledgeBasesBranchDefect, sdkgo.FailureValidation, "CTYun request is invalid")
	}
	request.Header.Set("ctyun-eop-request-id", requestID)
	request.Header.Set("eop-date", date)
	request.Header.Set("tenantId", credentials.TenantID.Reveal())
	request.Header.Set("Eop-Authorization", signature)
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return knowledgeBaseRetry(sdkgo.FailureAvailability, "CTYun knowledge-base transport failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == 429 {
		return knowledgeBaseRetry(sdkgo.FailureRateLimit, "CTYun knowledge-base request rate limited", retryAfter)
	}
	if response.StatusCode == 408 || response.StatusCode >= 500 {
		return knowledgeBaseRetry(sdkgo.FailureAvailability, "CTYun knowledge-base service temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return knowledgeBaseBranch(ListKnowledgeBasesBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "CTYun knowledge-base response exceeded the configured limit")
	}
	if err != nil {
		return knowledgeBaseRetry(sdkgo.FailureAvailability, "CTYun knowledge-base response was interrupted", 0)
	}
	if response.StatusCode != 200 {
		kind := sdkgo.FailureProviderRejection
		if response.StatusCode == 401 || response.StatusCode == 403 {
			kind = sdkgo.FailureAuthentication
		}
		return knowledgeBaseBranch(ListKnowledgeBasesBranchProviderRejected, kind, "CTYun rejected knowledge-base request with HTTP "+strconv.Itoa(response.StatusCode))
	}
	var wire struct {
		StatusCode json.RawMessage `json:"statusCode"`
		Error      string          `json:"error"`
		ReturnObj  []struct {
			ID    int64  `json:"id"`
			Name  string `json:"name"`
			Count int    `json:"count"`
		} `json:"returnObj"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	if decoder.Decode(&wire) != nil || wire.StatusCode == nil {
		return knowledgeBaseBranch(ListKnowledgeBasesBranchInvalidResponse, sdkgo.FailureProtocol, "CTYun returned invalid knowledge-base JSON")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return knowledgeBaseBranch(ListKnowledgeBasesBranchInvalidResponse, sdkgo.FailureProtocol, "CTYun returned trailing knowledge-base JSON")
	}
	status, ok := parseStatusCode(wire.StatusCode)
	if !ok {
		return knowledgeBaseBranch(ListKnowledgeBasesBranchInvalidResponse, sdkgo.FailureProtocol, "CTYun returned invalid status code")
	}
	if status != "200" {
		kind := sdkgo.FailureProviderRejection
		if status == "40001" || status == "40003" || status == "40016" || strings.HasPrefix(wire.Error, "CTAPI_100") {
			kind = sdkgo.FailureAuthentication
		}
		if status == "40002" || status == "40004" {
			return knowledgeBaseRetry(sdkgo.FailureAvailability, "CTYun knowledge-base service reported a temporary error", 0)
		}
		return knowledgeBaseBranch(ListKnowledgeBasesBranchProviderRejected, kind, "CTYun rejected knowledge-base request with status "+status)
	}
	if wire.ReturnObj == nil {
		return knowledgeBaseBranch(ListKnowledgeBasesBranchInvalidResponse, sdkgo.FailureProtocol, "CTYun knowledge-base list is missing")
	}
	knowledgeBases := make([]KnowledgeBaseSummary, 0, len(wire.ReturnObj))
	for _, item := range wire.ReturnObj {
		if item.ID <= 0 || item.Name == "" || len(item.Name) > 1024 || item.Count < 0 {
			return knowledgeBaseBranch(ListKnowledgeBasesBranchInvalidResponse, sdkgo.FailureProtocol, "CTYun returned invalid knowledge-base metadata")
		}
		knowledgeBases = append(knowledgeBases, KnowledgeBaseSummary{ID: item.ID, Name: item.Name, FileCount: item.Count})
	}
	return sdkgo.NewQueryBranch(ListKnowledgeBasesBranchListed, ListKnowledgeBasesOutput{KnowledgeBases: knowledgeBases}, nil, sdkgo.Receipt{})
}
func newRequestID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	bytes[6] = bytes[6]&0x0f | 0x40
	bytes[8] = bytes[8]&0x3f | 0x80
	encoded := hex.EncodeToString(bytes)
	return encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:], nil
}
func signAuthorization(accessKey, securityKey, requestID, date string) string {
	bodyHash := sha256.Sum256(nil)
	canonical := "ctyun-eop-request-id:" + requestID + "\neop-date:" + date + "\n\n" + hex.EncodeToString(bodyHash[:])
	timeKey := hmacSHA256([]byte(securityKey), []byte(date))
	accessKeyHash := hmacSHA256(timeKey, []byte(accessKey))
	dateKey := hmacSHA256(accessKeyHash, []byte(date[:8]))
	signature := base64.StdEncoding.EncodeToString(hmacSHA256(dateKey, []byte(canonical)))
	return accessKey + " headers=ctyun-eop-request-id;eop-date Signature=" + signature
}
func hmacSHA256(key, message []byte) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(message)
	return mac.Sum(nil)
}
func parseStatusCode(raw json.RawMessage) (string, bool) {
	var asString string
	if json.Unmarshal(raw, &asString) == nil {
		return asString, asString != ""
	}
	var asInt int
	if json.Unmarshal(raw, &asInt) == nil && asInt > 0 {
		return strconv.Itoa(asInt), true
	}
	return "", false
}
func isValidCredential(value string) bool {
	if value == "" || len(value) > 2048 {
		return false
	}
	for _, character := range value {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}
func knowledgeBaseBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ListKnowledgeBasesOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "ctyun", Operation: "listKnowledgeBases", Message: message}
	return sdkgo.NewQueryBranch(branch, ListKnowledgeBasesOutput{}, &failure, sdkgo.Receipt{})
}
func knowledgeBaseRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ListKnowledgeBasesOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "ctyun", Operation: "listKnowledgeBases", Message: message}
	return sdkgo.NewQueryRetry[ListKnowledgeBasesOutput](failure, delay)
}
