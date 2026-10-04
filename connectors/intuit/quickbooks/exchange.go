// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package quickbooks

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	// faultCodeObjectNotFound is QuickBooks's "Object Not Found" validation code.
	faultCodeObjectNotFound = "610"
	// faultCodeDuplicateName is QuickBooks's "Duplicate Name Exists Error" validation code.
	faultCodeDuplicateName = "6240"
	// faultCodeDuplicateDocNumber is QuickBooks's "Duplicate Document Number Error" validation code.
	faultCodeDuplicateDocNumber = "6140"
	// faultCodeDuplicateRequestID is "Duplicate Request ID", retried because a repeated requestid should get the original response.
	faultCodeDuplicateRequestID = "600"
	// faultCodeInsufficientScope is QuickBooks's 403 code for a token without the accounting scope.
	faultCodeInsufficientScope = "3100"
	// defaultThrottleDelay is the wait QuickBooks documents after HTTP 429 when no Retry-After is sent.
	defaultThrottleDelay = 60 * time.Second
	// maximumReportedFaultErrors bounds the error count read from one Fault.
	maximumReportedFaultErrors = 1000
)

var (
	errResponseNotJSONObject = errors.New("response is not a JSON object")
	faultTypePattern         = regexp.MustCompile(`^[A-Za-z]{1,64}$`)
	faultCodePattern         = regexp.MustCompile(`^[0-9]{1,10}$`)
)

// quickbooksRequest is one Accounting API request below /v3/company/{realmId}; a mutation carries the Step's requestid.
type quickbooksRequest struct {
	method     string
	path       string
	query      url.Values
	payload    any
	isMutation bool
}

// quickbooksResponse holds the safe parts of one response; body is kept only when it is usable.
type quickbooksResponse struct {
	statusCode    int
	header        http.Header
	body          []byte
	transactionID string
}

// exchangeOutcome is the provider-neutral meaning of one QuickBooks request that each operation maps to a branch.
type exchangeOutcome uint8

const (
	exchangeSucceeded exchangeOutcome = iota + 1
	exchangeNotFound
	// exchangeRetry is safe for reads and, under the Step's requestid, for every mutation.
	exchangeRetry
	exchangeRejected
	// exchangeNameConflict is Fault code 6240, which createCustomer reports on its own branch.
	exchangeNameConflict
	// exchangeInvalid is an unusable 2xx: invalidResponse for a read, uncertain for a mutation.
	exchangeInvalid
	exchangeDefect
)

// quickbooksExchange is one classified request; failure is set for every outcome except exchangeSucceeded.
type quickbooksExchange struct {
	outcome    exchangeOutcome
	response   quickbooksResponse
	failure    sdkgo.Failure
	retryAfter time.Duration
	metadata   map[string]string
}

// faultSummary holds only machine-readable tokens and a count from a QuickBooks Fault.
type faultSummary struct {
	faultType  string
	codes      []string
	errorCount int
}

// faultClassification is one failed QuickBooks response being mapped to an exchange outcome.
type faultClassification struct {
	operation string
	response  quickbooksResponse
	summary   faultSummary
	status    int
}

// faultEnvelopeWire is QuickBooks's error body; Message and Detail are never decoded.
type faultEnvelopeWire struct {
	Fault *struct {
		Type  string `json:"type"`
		Error []struct {
			Code json.RawMessage `json:"code"`
		} `json:"Error"`
	} `json:"Fault"`
}

// classifyHTTPResponse reads a bounded body and maps the status; it closes the body.
func classifyHTTPResponse(operation string, httpResponse *http.Response, secret string, maxResponseBytes int64, now time.Time) quickbooksExchange {
	defer func() {
		// The body is read to its bound below; a close failure cannot change the classified response.
		_ = httpResponse.Body.Close()
	}()
	response := quickbooksResponse{statusCode: httpResponse.StatusCode, header: httpResponse.Header.Clone()}
	if transactionID := httpResponse.Header.Get(intuitTransactionIDHeader); transactionIDPattern.MatchString(transactionID) {
		response.transactionID = transactionID
	}
	if !isSuccessStatus(httpResponse.StatusCode) {
		// An error body is truncated, not rejected; only its tokens are read.
		body, err := io.ReadAll(io.LimitReader(httpResponse.Body, providerhttp.MaxErrorBodyBytes))
		if err != nil {
			body = nil
		}
		return classifyFailedStatus(operation, response, summarizeFault(body), now)
	}
	body, err := providerhttp.ReadBoundedBody(httpResponse.Body, maxResponseBytes)
	switch {
	case errors.Is(err, providerhttp.ErrBodyTooLarge):
		return quickbooksExchange{outcome: exchangeInvalid, response: response,
			failure: quickbooksFailure(sdkgo.FailureResponseTooLarge, operation, "QuickBooks response exceeds the configured maxResponseBytes limit")}
	case err != nil:
		return quickbooksExchange{outcome: exchangeRetry, response: response,
			failure: quickbooksFailure(sdkgo.FailureTransport, operation, "QuickBooks response was interrupted")}
	case secret != "" && bytes.Contains(body, []byte(secret)):
		return quickbooksExchange{outcome: exchangeInvalid, response: response,
			failure: quickbooksFailure(sdkgo.FailureProtocol, operation, "QuickBooks response reflected the connection credential")}
	}
	// QuickBooks can answer a request it rejected with a Fault body and HTTP 200.
	if summary := summarizeFault(body); summary.faultType != "" || summary.errorCount != 0 {
		return classifyFault(faultClassification{operation: operation, response: response, summary: summary, status: http.StatusOK})
	}
	response.body = body
	return quickbooksExchange{outcome: exchangeSucceeded, response: response}
}

// classifyFailedStatus maps a non-2xx QuickBooks response; QuickBooks's own message text is never read.
func classifyFailedStatus(operation string, response quickbooksResponse, summary faultSummary, now time.Time) quickbooksExchange {
	status := response.statusCode
	classification := faultClassification{operation: operation, response: response, summary: summary, status: status}
	switch {
	case status == http.StatusTooManyRequests:
		throttled := classification.classifiedExchange(exchangeRetry, sdkgo.FailureRateLimit, "QuickBooks throttled the request")
		throttled.retryAfter = providerhttp.ParseRetryAfter(response.header.Get("Retry-After"), now)
		if throttled.retryAfter <= 0 {
			throttled.retryAfter = defaultThrottleDelay
		}
		return throttled
	case status == http.StatusNotImplemented:
		return classification.classifiedExchange(exchangeRejected, sdkgo.FailureProviderRejection, "QuickBooks does not implement the request")
	case status == http.StatusRequestTimeout || status >= 500:
		return classification.classifiedExchange(exchangeRetry, sdkgo.FailureAvailability, "QuickBooks is temporarily unavailable")
	case status == http.StatusUnauthorized:
		return classification.classifiedExchange(exchangeRejected, sdkgo.FailureAuthentication, "QuickBooks rejected the access token")
	case status == http.StatusForbidden && summary.hasCode(faultCodeInsufficientScope):
		return classification.classifiedExchange(exchangeRejected, sdkgo.FailureAuthorization, "QuickBooks token lacks the com.intuit.quickbooks.accounting scope")
	case status == http.StatusForbidden:
		return classification.classifiedExchange(exchangeRejected, sdkgo.FailureAuthorization, "QuickBooks denied access to the company or the request")
	case status == http.StatusNotFound:
		return classification.classifiedExchange(exchangeNotFound, sdkgo.FailureNotFound, "QuickBooks found no such resource")
	case status == http.StatusBadRequest:
		return classifyFault(classification)
	case status >= 300 && status < 400:
		return classification.classifiedExchange(exchangeRejected, sdkgo.FailureProtocol, "QuickBooks redirected the request")
	default:
		return classification.classifiedExchange(exchangeRejected, sdkgo.FailureProviderRejection, "QuickBooks rejected the request")
	}
}

// classifyFault maps a QuickBooks validation Fault by its error code.
func classifyFault(classification faultClassification) quickbooksExchange {
	summary := classification.summary
	switch {
	case summary.hasCode(faultCodeDuplicateRequestID):
		return classification.classifiedExchange(exchangeRetry, sdkgo.FailureConflict, "QuickBooks is still answering an earlier attempt with this requestid")
	case summary.hasCode(faultCodeObjectNotFound):
		return classification.classifiedExchange(exchangeNotFound, sdkgo.FailureNotFound, "QuickBooks found no such object")
	case summary.hasCode(faultCodeDuplicateName):
		return classification.classifiedExchange(exchangeNameConflict, sdkgo.FailureConflict, "QuickBooks already has a name list entry with that display name")
	case summary.hasCode(faultCodeDuplicateDocNumber):
		return classification.classifiedExchange(exchangeRejected, sdkgo.FailureConflict, "QuickBooks already has a transaction with that document number")
	case strings.EqualFold(summary.faultType, "AuthenticationFault"):
		return classification.classifiedExchange(exchangeRejected, sdkgo.FailureAuthentication, "QuickBooks rejected the authentication")
	case strings.EqualFold(summary.faultType, "AuthorizationFault"):
		return classification.classifiedExchange(exchangeRejected, sdkgo.FailureAuthorization, "QuickBooks denied the request")
	case strings.EqualFold(summary.faultType, "SystemFault"):
		return classification.classifiedExchange(exchangeRetry, sdkgo.FailureAvailability, "QuickBooks failed internally")
	default:
		return classification.classifiedExchange(exchangeRejected, sdkgo.FailureValidation, "QuickBooks rejected the request")
	}
}

// classifiedExchange names the HTTP status and Fault codes in the failure, never QuickBooks's message text.
func (classification faultClassification) classifiedExchange(outcome exchangeOutcome, kind sdkgo.FailureKind, message string) quickbooksExchange {
	return quickbooksExchange{outcome: outcome, response: classification.response, metadata: classification.summary.receiptMetadata(),
		failure: quickbooksFailure(kind, classification.operation,
			withFaultSummary(fmt.Sprintf("%s (HTTP %d)", message, classification.status), classification.summary))}
}

// summarizeFault reads the Fault type, the error codes, and their count.
func summarizeFault(body []byte) faultSummary {
	var envelope faultEnvelopeWire
	if len(bytes.TrimSpace(body)) == 0 || json.Unmarshal(body, &envelope) != nil || envelope.Fault == nil {
		return faultSummary{}
	}
	summary := faultSummary{}
	if faultTypePattern.MatchString(envelope.Fault.Type) {
		summary.faultType = envelope.Fault.Type
	}
	summary.errorCount = min(len(envelope.Fault.Error), maximumReportedFaultErrors)
	for _, faultError := range envelope.Fault.Error {
		code := strings.Trim(string(bytes.TrimSpace(faultError.Code)), `"`)
		if faultCodePattern.MatchString(code) && len(summary.codes) < 5 {
			summary.codes = append(summary.codes, code)
		}
	}
	return summary
}

func (summary faultSummary) hasCode(code string) bool {
	for _, candidate := range summary.codes {
		if candidate == code {
			return true
		}
	}
	return false
}

// receiptMetadata exposes the first Fault code so an application can branch on it without parsing text.
func (summary faultSummary) receiptMetadata() map[string]string {
	if len(summary.codes) == 0 {
		return nil
	}
	return map[string]string{FaultCodeReceiptKey: summary.codes[0]}
}

func withFaultSummary(message string, summary faultSummary) string {
	var parts []string
	if summary.faultType != "" {
		parts = append(parts, summary.faultType)
	}
	if len(summary.codes) != 0 {
		parts = append(parts, "code "+strings.Join(summary.codes, ", "))
	}
	switch summary.errorCount {
	case 0, 1:
	default:
		parts = append(parts, strconv.Itoa(summary.errorCount)+" errors")
	}
	if len(parts) == 0 {
		return message
	}
	return message + " [" + strings.Join(parts, "; ") + "]"
}

func withMetadata(metadata map[string]string, key string, value string) map[string]string {
	if value == "" {
		return metadata
	}
	if metadata == nil {
		metadata = map[string]string{}
	}
	metadata[key] = value
	return metadata
}

// isConnectionNeverEstablished reports a dial failure, after which QuickBooks cannot have received the request.
func isConnectionNeverEstablished(err error) bool {
	var operationError *net.OpError
	return errors.As(err, &operationError) && operationError.Op == "dial"
}

func isSuccessStatus(status int) bool { return status >= 200 && status < 300 }

func quickbooksFailure(kind sdkgo.FailureKind, operation string, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: providerName, Operation: operation, Message: message}
}

func quickbooksFailurePointer(kind sdkgo.FailureKind, operation string, message string) *sdkgo.Failure {
	failure := quickbooksFailure(kind, operation, message)
	return &failure
}
