// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package lianlianpay

import (
	"bytes"
	"crypto"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// QueryPaymentStatusInput identifies a payment order owned by the authorized merchant.
type QueryPaymentStatusInput struct {
	// MerchantOrderID is the merchant's unique order number, at most 32 characters.
	MerchantOrderID string `json:"merchantOrderId"`
}

// PaymentStatusResult contains the merchant order number and processing status only.
type PaymentStatusResult struct {
	// MerchantOrderID is the merchant's original order number.
	MerchantOrderID string `json:"merchantOrderId"`
	// Status is SUCCESS, FAILURE, PROCESSING, CLOSED, or CANCEL.
	Status string `json:"status"`
}

// QueryPaymentStatusOperation reads a merchant order's processing status.
type QueryPaymentStatusOperation struct{ client *Client }

// Definition returns the generated payment-status Query contract.
func (QueryPaymentStatusOperation) Definition() sdkgo.QueryDefinition {
	return QueryPaymentStatusDefinition
}

// Invoke signs and sends one query, then classifies provider and transport outcomes.
func (operation QueryPaymentStatusOperation) Invoke(call sdkgo.Call, input QueryPaymentStatusInput) sdkgo.QueryAttempt[PaymentStatusResult] {
	if !validMerchantOrderID(input.MerchantOrderID) {
		return paymentBranch(QueryPaymentStatusBranchDefect, sdkgo.FailureValidation, "LianLian Pay merchant order number is invalid")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !validMerchantID(credentials.MerchantID) {
		return paymentBranch(QueryPaymentStatusBranchDefect, sdkgo.FailureAuthentication, "LianLian Pay credentials are unavailable or invalid")
	}
	privateKey, err := parseMerchantPrivateKey(credentials.PrivateKey.Reveal())
	if err != nil {
		return paymentBranch(QueryPaymentStatusBranchDefect, sdkgo.FailureAuthentication, "LianLian Pay private key is invalid")
	}
	body, err := json.Marshal(struct {
		MerchantID      string `json:"mch_id"`
		MerchantOrderID string `json:"txn_seqno"`
	}{credentials.MerchantID, input.MerchantOrderID})
	if err != nil {
		return paymentBranch(QueryPaymentStatusBranchDefect, sdkgo.FailureValidation, "LianLian Pay order request is invalid")
	}
	signature, err := signPaymentRequest(privateKey, body)
	if err != nil {
		return paymentBranch(QueryPaymentStatusBranchDefect, sdkgo.FailureAuthentication, "LianLian Pay request could not be signed")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+"/query/mch/v1/accp/txn/order-query", bytes.NewReader(body))
	if err != nil {
		return paymentBranch(QueryPaymentStatusBranchDefect, sdkgo.FailureValidation, "LianLian Pay order request is invalid")
	}
	request.Header.Set("Signature-Type", "RSA")
	request.Header.Set("Signature-Data", signature)
	request.Header.Set("mch_id", credentials.MerchantID)
	request.Header.Set("timestamp", time.Now().In(time.FixedZone("CST", 8*60*60)).Format("20060102150405"))
	request.Header.Set("Content-Type", "application/json;charset=utf-8")
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return paymentRetry(sdkgo.FailureAvailability, "LianLian Pay order request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return paymentRetry(sdkgo.FailureRateLimit, "LianLian Pay rate limited order query", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return paymentRetry(sdkgo.FailureAvailability, "LianLian Pay order API is temporarily unavailable", retryAfter)
	}
	responseBody, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return paymentBranch(QueryPaymentStatusBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "LianLian Pay response exceeded the configured limit")
	}
	if err != nil {
		return paymentRetry(sdkgo.FailureAvailability, "LianLian Pay order response was interrupted", 0)
	}
	if response.StatusCode != http.StatusOK {
		return paymentBranch(QueryPaymentStatusBranchProviderRejected, classifyPaymentHTTP(response.StatusCode), "LianLian Pay rejected order query with HTTP "+strconv.Itoa(response.StatusCode))
	}
	result, isRejected, err := decodePaymentStatus(responseBody, input)
	if err != nil {
		return paymentBranch(QueryPaymentStatusBranchInvalidResponse, sdkgo.FailureProtocol, "LianLian Pay returned an invalid order response")
	}
	if isRejected {
		return paymentBranch(QueryPaymentStatusBranchProviderRejected, sdkgo.FailureProviderRejection, "LianLian Pay rejected order query")
	}
	return sdkgo.NewQueryBranch(QueryPaymentStatusBranchFound, result, nil, sdkgo.Receipt{})
}

func decodePaymentStatus(body []byte, input QueryPaymentStatusInput) (PaymentStatusResult, bool, error) {
	var envelope struct {
		ReturnCode      string `json:"ret_code"`
		MerchantOrderID string `json:"txn_seqno"`
		Status          string `json:"txn_status"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.ReturnCode == "" {
		return PaymentStatusResult{}, false, errors.New("invalid LianLian response")
	}
	if envelope.ReturnCode != "0000" {
		return PaymentStatusResult{}, true, nil
	}
	if envelope.MerchantOrderID != input.MerchantOrderID {
		return PaymentStatusResult{}, false, errors.New("mismatched LianLian order")
	}
	switch envelope.Status {
	case "SUCCESS", "FAILURE", "PROCESSING", "CLOSED", "CANCEL":
	default:
		return PaymentStatusResult{}, false, errors.New("invalid LianLian order status")
	}
	return PaymentStatusResult{MerchantOrderID: envelope.MerchantOrderID, Status: envelope.Status}, false, nil
}

func parseMerchantPrivateKey(value string) (*rsa.PrivateKey, error) {
	if len(value) > 16384 {
		return nil, errors.New("private key too large")
	}
	block, rest := pem.Decode([]byte(value))
	if block == nil || len(bytes.TrimSpace(rest)) != 0 {
		return nil, errors.New("invalid PEM")
	}
	if block.Type == "RSA PRIVATE KEY" {
		return x509.ParsePKCS1PrivateKey(block.Bytes)
	}
	if block.Type == "PRIVATE KEY" {
		parsedKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		privateKey, ok := parsedKey.(*rsa.PrivateKey)
		if !ok {
			return nil, errors.New("RSA key required")
		}
		return privateKey, nil
	}
	return nil, errors.New("RSA key required")
}

func signPaymentRequest(privateKey *rsa.PrivateKey, body []byte) (string, error) {
	firstDigest := md5.Sum(body)
	secondDigest := md5.Sum([]byte(hex.EncodeToString(firstDigest[:])))
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.MD5, secondDigest[:])
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(signature), nil
}

func validMerchantOrderID(value string) bool {
	if len(value) == 0 || len(value) > 32 || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}
func validMerchantID(value string) bool {
	if len(value) < 16 || len(value) > 18 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}
func classifyPaymentHTTP(status int) sdkgo.FailureKind {
	switch status {
	case 401:
		return sdkgo.FailureAuthentication
	case 403:
		return sdkgo.FailureAuthorization
	case 400, 404, 412:
		return sdkgo.FailureValidation
	default:
		return sdkgo.FailureProviderRejection
	}
}
func paymentBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[PaymentStatusResult] {
	failure := sdkgo.Failure{Kind: kind, Provider: "lianlianpay", Operation: "queryPaymentStatus", Message: message}
	return sdkgo.NewQueryBranch(branch, PaymentStatusResult{}, &failure, sdkgo.Receipt{})
}
func paymentRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[PaymentStatusResult] {
	failure := sdkgo.Failure{Kind: kind, Provider: "lianlianpay", Operation: "queryPaymentStatus", Message: message}
	return sdkgo.NewQueryRetry[PaymentStatusResult](failure, delay)
}
