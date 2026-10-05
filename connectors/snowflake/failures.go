// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package snowflake

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Snowflake response codes the connector recognizes; every other code is reported as received.
const (
	// statementNotFoundCode is Snowflake's "Statement <handle> not found".
	statementNotFoundCode = "000709"
	// statementCanceledCode is Snowflake's "SQL execution canceled".
	statementCanceledCode = "000604"
)

var (
	errCallContextMissing = errors.New("the Dex call context is missing")
	errRequestNotBuilt    = errors.New("the Snowflake SQL API request could not be built")

	snowflakeCodePattern = regexp.MustCompile(`^[0-9]{6}$`)
	sqlStatePattern      = regexp.MustCompile(`^[0-9A-Z]{5}$`)
)

// credentialError describes unusable credentials without repeating any credential value.
type credentialError struct{ message string }

// Error returns the safe description.
func (err *credentialError) Error() string { return err.message }

// transportError reports that no complete HTTP response was received; it never repeats a URL or header.
type transportError struct{}

// Error returns a safe description.
func (*transportError) Error() string {
	return "the Snowflake SQL API request failed or its response could not be read"
}

// statementStatus omits Snowflake's message, which can repeat SQL text and values.
type statementStatus struct {
	Code            string `json:"code"`
	SQLState        string `json:"sqlState"`
	StatementHandle string `json:"statementHandle"`
	CreatedOn       *int64 `json:"createdOn"`
}

// failureDisposition is how an operation reports one classified failure.
type failureDisposition int

const (
	dispositionRetry failureDisposition = iota
	dispositionRejected
	dispositionInvalidResponse
	dispositionDefect
)

// classifiedFailure is a secret-free description of one failed request.
type classifiedFailure struct {
	disposition failureDisposition
	kind        sdkgo.FailureKind
	message     string
	retryAfter  time.Duration
	status      statementStatus
}

// classifyUnsuccessfulResponse maps a status code outside the operation's success codes; body is at most a bounded prefix.
func classifyUnsuccessfulResponse(response *http.Response, body []byte, now time.Time) classifiedFailure {
	status := decodeStatementStatus(body)
	failure := classifiedFailure{disposition: dispositionRejected, kind: sdkgo.FailureProviderRejection, status: status}
	switch code := response.StatusCode; {
	case code == http.StatusTooManyRequests:
		failure.disposition, failure.kind = dispositionRetry, sdkgo.FailureRateLimit
		failure.retryAfter = providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), now)
	case code == http.StatusInternalServerError || code == http.StatusBadGateway ||
		code == http.StatusServiceUnavailable || code == http.StatusGatewayTimeout:
		failure.disposition, failure.kind = dispositionRetry, sdkgo.FailureAvailability
		failure.retryAfter = providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), now)
	case code == http.StatusUnauthorized:
		failure.kind = sdkgo.FailureAuthentication
	case code == http.StatusForbidden:
		failure.kind = sdkgo.FailureAuthorization
	case code == http.StatusNotFound:
		failure.kind = sdkgo.FailureNotFound
	case code == http.StatusBadRequest:
		failure.kind = sdkgo.FailureValidation
	case code == http.StatusRequestTimeout:
		failure.kind = sdkgo.FailureAvailability
	case code == http.StatusUnprocessableEntity:
		failure.kind = statementFailureKind(status)
	case code >= 300 && code < 400:
		failure.kind = sdkgo.FailureProtocol
	}
	failure.message = describeHTTPFailure(response.StatusCode, status)
	switch {
	case response.StatusCode == http.StatusRequestTimeout || status.Code == statementCanceledCode:
		failure.message += "; the statement exceeded statementTimeoutSeconds or was canceled"
	case response.StatusCode >= 300 && response.StatusCode < 400:
		failure.message += "; the connector never follows redirects, so check accountIdentifier"
	}
	return failure
}

// statementFailureKind classifies a 422 QueryFailureStatus by Snowflake code and SQLSTATE class.
func statementFailureKind(status statementStatus) sdkgo.FailureKind {
	switch {
	case status.Code == statementNotFoundCode || status.SQLState == "02000":
		return sdkgo.FailureNotFound
	case status.Code == statementCanceledCode || status.SQLState == "57014":
		return sdkgo.FailureAvailability
	case len(status.SQLState) == 5 && status.SQLState[:2] == "28":
		return sdkgo.FailureAuthentication
	case status.SQLState == "42501":
		return sdkgo.FailureAuthorization
	case len(status.SQLState) == 5 && status.SQLState[:2] == "23":
		return sdkgo.FailureConflict
	case len(status.SQLState) == 5 && (status.SQLState[:2] == "22" || status.SQLState[:2] == "42" || status.SQLState[:2] == "07"):
		return sdkgo.FailureValidation
	default:
		return sdkgo.FailureProviderRejection
	}
}

func describeHTTPFailure(statusCode int, status statementStatus) string {
	message := fmt.Sprintf("Snowflake returned HTTP %d", statusCode)
	if isSnowflakeCode(status.Code) {
		message += " with code " + status.Code
	}
	if isSQLState(status.SQLState) {
		message += " and SQLSTATE " + status.SQLState
	}
	return message
}

// decodeStatementStatus keeps only codes that match Snowflake's formats, so no provider text reaches a Failure.
func decodeStatementStatus(body []byte) statementStatus {
	var status statementStatus
	if json.Unmarshal(body, &status) != nil {
		return statementStatus{}
	}
	if !isSnowflakeCode(status.Code) {
		status.Code = ""
	}
	if !isSQLState(status.SQLState) {
		status.SQLState = ""
	}
	if !statementHandlePattern.MatchString(status.StatementHandle) {
		status.StatementHandle = ""
	}
	return status
}

// readErrorBody reads a bounded prefix of a status or error body; a truncated body simply yields no codes.
func readErrorBody(response *http.Response) []byte {
	body, err := io.ReadAll(io.LimitReader(response.Body, providerhttp.MaxErrorBodyBytes))
	if err != nil {
		return nil
	}
	return body
}

func transportFailure() classifiedFailure {
	return classifiedFailure{disposition: dispositionRetry, kind: sdkgo.FailureTransport, message: (&transportError{}).Error()}
}

func invalidResponseFailure(message string) classifiedFailure {
	return classifiedFailure{disposition: dispositionInvalidResponse, kind: sdkgo.FailureProtocol, message: message}
}

// requestFailure classifies an error from Client.send, which happens before any response was read.
func requestFailure(err error) classifiedFailure {
	var credentials *credentialError
	if errors.As(err, &credentials) {
		return classifiedFailure{disposition: dispositionDefect, kind: sdkgo.FailureAuthentication, message: credentials.Error()}
	}
	var transport *transportError
	if errors.As(err, &transport) {
		return transportFailure()
	}
	return classifiedFailure{disposition: dispositionDefect, kind: sdkgo.FailureLocalDefect, message: err.Error()}
}

func isSnowflakeCode(value string) bool { return snowflakeCodePattern.MatchString(value) }

func isSQLState(value string) bool { return sqlStatePattern.MatchString(value) }

func failurePointer(operationID string, failure classifiedFailure) *sdkgo.Failure {
	value := sdkgo.Failure{Kind: failure.kind, Provider: ConnectorID, Operation: operationID, Message: failure.message}
	return &value
}

func sdkFailure(operationID string, failure classifiedFailure) sdkgo.Failure {
	return *failurePointer(operationID, failure)
}

func createdOnTime(createdOn *int64) *time.Time {
	if createdOn == nil || *createdOn <= 0 {
		return nil
	}
	created := time.UnixMilli(*createdOn).UTC()
	return &created
}
