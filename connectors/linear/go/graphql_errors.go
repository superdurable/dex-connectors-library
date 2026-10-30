// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package linear

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// graphQLErrorSummary holds only machine-readable tokens from Linear's errors array, never message text.
type graphQLErrorSummary struct {
	code      string
	errorType string
}

var errorCodePattern = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,64}$`)

// knownErrorTypes are the extensions.type values Linear's SDK parses; any other value is ignored.
var knownErrorTypes = map[string]bool{
	"feature not accessible": true, "invalid input": true, "ratelimited": true, "network error": true,
	"authentication error": true, "forbidden": true, "bootstrap error": true, "unknown": true,
	"internal error": true, "other": true, "user error": true, "graphql error": true,
	"lock timeout": true, "usage limit exceeded": true,
}

var (
	rateLimitErrorCodes      = map[string]bool{"RATELIMITED": true}
	authenticationErrorCodes = map[string]bool{"AUTHENTICATION_ERROR": true, "UNAUTHENTICATED": true}
	authorizationErrorCodes  = map[string]bool{"FORBIDDEN": true}
	// documentErrorCodes mean Linear could not parse or validate the connector's own GraphQL request.
	documentErrorCodes = map[string]bool{
		"GRAPHQL_PARSE_FAILED": true, "GRAPHQL_VALIDATION_FAILED": true, "BAD_REQUEST": true, "OPERATION_RESOLUTION_FAILURE": true,
	}
	unavailableErrorCodes = map[string]bool{"INTERNAL_SERVER_ERROR": true, "INTERNAL_ERROR": true, "LOCK_TIMEOUT": true}
	inputErrorCodes       = map[string]bool{"INPUT_ERROR": true, "BAD_USER_INPUT": true, "INVALID_INPUT": true}
)

// summarizeGraphQLErrors reads the first error's extensions.code and extensions.type; message text is never read.
func summarizeGraphQLErrors(body []byte, secret string) graphQLErrorSummary {
	var envelope struct {
		Errors []struct {
			Extensions struct {
				Code string `json:"code"`
				Type string `json:"type"`
			} `json:"extensions"`
		} `json:"errors"`
	}
	if json.Unmarshal(body, &envelope) != nil || len(envelope.Errors) == 0 {
		return graphQLErrorSummary{}
	}
	extensions := envelope.Errors[0].Extensions
	summary := graphQLErrorSummary{}
	if errorCodePattern.MatchString(extensions.Code) && (secret == "" || !strings.Contains(extensions.Code, secret)) {
		summary.code = extensions.Code
	}
	if knownErrorTypes[extensions.Type] {
		summary.errorType = extensions.Type
	}
	return summary
}

// classifyFailedResponse lets the error code and type decide first, because Linear rate limits with HTTP 400.
func classifyFailedResponse(operation string, response linearResponse, summary graphQLErrorSummary, retryAfter time.Duration) linearExchange {
	status := response.statusCode
	describe := func(verb string) string {
		return withErrorSummary(fmt.Sprintf("Linear %s (HTTP %d)", verb, status), summary)
	}
	outcome := func(outcome exchangeOutcome, kind sdkgo.FailureKind, message string) linearExchange {
		return linearExchange{outcome: outcome, response: response, retryAfter: retryAfter, failure: linearFailure(kind, operation, message)}
	}
	code, errorType := summary.code, summary.errorType
	switch {
	case rateLimitErrorCodes[code] || errorType == "ratelimited":
		return outcome(exchangeRetry, sdkgo.FailureRateLimit, describe("rate limited the request"))
	case authenticationErrorCodes[code] || errorType == "authentication error":
		return outcome(exchangeRejected, sdkgo.FailureAuthentication, describe("rejected the credential"))
	case authorizationErrorCodes[code] || errorType == "forbidden" || errorType == "feature not accessible":
		return outcome(exchangeRejected, sdkgo.FailureAuthorization, describe("denied permission"))
	case errorType == "usage limit exceeded":
		return outcome(exchangeRejected, sdkgo.FailureQuotaExhausted, describe("reported a workspace usage limit"))
	case documentErrorCodes[code] || errorType == "graphql error":
		return outcome(exchangeDefect, sdkgo.FailureProtocol, describe("rejected the connector's GraphQL request"))
	case unavailableErrorCodes[code] || errorType == "internal error" || errorType == "lock timeout" || errorType == "network error" || errorType == "bootstrap error":
		return outcome(exchangeRetry, sdkgo.FailureAvailability, describe("could not complete the request"))
	case inputErrorCodes[code] || errorType == "invalid input" || errorType == "user error":
		return outcome(exchangeRejected, sdkgo.FailureValidation, describe("rejected the input"))
	}
	switch {
	case status == http.StatusTooManyRequests:
		return outcome(exchangeRetry, sdkgo.FailureRateLimit, describe("rate limited the request"))
	case status == http.StatusRequestTimeout || status >= 500:
		return outcome(exchangeRetry, sdkgo.FailureAvailability, describe("could not complete the request"))
	case status == http.StatusUnauthorized:
		return outcome(exchangeRejected, sdkgo.FailureAuthentication, describe("rejected the credential"))
	case status == http.StatusForbidden:
		return outcome(exchangeRejected, sdkgo.FailureAuthorization, describe("denied permission"))
	case status == http.StatusBadRequest && code == "" && errorType == "":
		return outcome(exchangeDefect, sdkgo.FailureProtocol, describe("rejected the connector's GraphQL request"))
	case status >= 300 && status < 400:
		return outcome(exchangeRejected, sdkgo.FailureProtocol, describe("redirected the request"))
	default:
		return outcome(exchangeRejected, sdkgo.FailureProviderRejection, describe("rejected the request"))
	}
}

func withErrorSummary(message string, summary graphQLErrorSummary) string {
	var parts []string
	if summary.code != "" {
		parts = append(parts, summary.code)
	}
	if summary.errorType != "" {
		parts = append(parts, summary.errorType)
	}
	if len(parts) == 0 {
		return message
	}
	return message + " [" + strings.Join(parts, "; ") + "]"
}

// isReadBackWorthy reports a write failure after which Linear may still hold the record by its client ID.
func isReadBackWorthy(result linearExchange) bool {
	switch result.outcome {
	case exchangeInvalid:
		return true
	case exchangeRetry:
		return result.failure.Kind == sdkgo.FailureAvailability || result.failure.Kind == sdkgo.FailureTransport
	case exchangeRejected:
		return result.failure.Kind == sdkgo.FailureValidation || result.failure.Kind == sdkgo.FailureProviderRejection ||
			result.failure.Kind == sdkgo.FailureQuotaExhausted
	default:
		return false
	}
}

// queryBranches names an operation's branches for the shared read outcome mapping.
type queryBranches struct {
	providerRejected sdkgo.BranchID
	invalidResponse  sdkgo.BranchID
	defect           sdkgo.BranchID
}

// queryAttemptForExchange maps every outcome except success; reads are always safe to retry.
func queryAttemptForExchange[OUT any](result linearExchange, output OUT, receipt sdkgo.Receipt, branches queryBranches) sdkgo.QueryAttempt[OUT] {
	switch result.outcome {
	case exchangeRetry:
		return sdkgo.NewQueryRetry[OUT](result.failure, result.retryAfter)
	case exchangeInvalid:
		return sdkgo.NewQueryBranch(branches.invalidResponse, output, &result.failure, receipt)
	case exchangeDefect:
		return sdkgo.NewQueryBranch(branches.defect, output, &result.failure, receipt)
	default:
		return sdkgo.NewQueryBranch(branches.providerRejected, output, &result.failure, receipt)
	}
}
