// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package textgen

import (
	"fmt"
	"net/http"
	"regexp"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

var providerTokenPattern = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,64}$`)

type errorDisposition uint8

const (
	errorDispositionInvalid errorDisposition = iota
	errorDispositionRetry
	errorDispositionProviderRejected
	errorDispositionInvalidResponse
	errorDispositionBlocked
)

// ErrorOutcome is how the pipeline handles one non-2xx provider response or
// ProviderReportedError. Build it with RetryOutcome, ProviderRejectedOutcome,
// QuotaExhaustedOutcome, BlockedOutcome, or InvalidResponseOutcome; the zero
// value is invalid.
type ErrorOutcome struct {
	disposition errorDisposition
	kind        sdkgo.FailureKind
}

// RetryOutcome returns Retry with kind. The retry waits for the wire format's
// ReadErrorRetryDelay result when positive, otherwise for the Retry-After
// header, capped at one hour; with neither, the Step's retry policy applies.
func RetryOutcome(kind sdkgo.FailureKind) ErrorOutcome {
	return ErrorOutcome{disposition: errorDispositionRetry, kind: kind}
}

// ProviderRejectedOutcome selects the providerRejected branch with kind.
func ProviderRejectedOutcome(kind sdkgo.FailureKind) ErrorOutcome {
	return ErrorOutcome{disposition: errorDispositionProviderRejected, kind: kind}
}

// QuotaExhaustedOutcome selects the providerRejected branch with
// sdkgo.FailureQuotaExhausted, for credit, spend, or billing exhaustion that a
// retry cannot fix.
func QuotaExhaustedOutcome() ErrorOutcome {
	return ProviderRejectedOutcome(sdkgo.FailureQuotaExhausted)
}

// BlockedOutcome selects the blocked branch with sdkgo.FailureProviderRejection
// and FinishReasonContentPolicy, for a provider that reports a content-policy
// block as an error, such as a 400 with the token "content_filter", instead
// of a finish reason. Applications then handle every provider's content-policy
// stops on one branch.
func BlockedOutcome() ErrorOutcome {
	return ErrorOutcome{disposition: errorDispositionBlocked, kind: sdkgo.FailureProviderRejection}
}

// InvalidResponseOutcome selects the invalidResponse branch with
// sdkgo.FailureProtocol, for a status the provider documents as a contract
// violation rather than a rejection.
func InvalidResponseOutcome() ErrorOutcome {
	return ErrorOutcome{disposition: errorDispositionInvalidResponse, kind: sdkgo.FailureProtocol}
}

// ErrorRule classifies non-2xx responses that match its status and error
// token. The pipeline evaluates a wire format's rules in order and uses the
// first match. When no rule matches, it applies these defaults:
//
//   - 408, 429, and 5xx other than 501 return Retry (rate limit for 429,
//     availability otherwise);
//   - 401 selects providerRejected with authentication, 403 with
//     authorization, 404 with not found, and 409 with conflict;
//   - 402 selects providerRejected with quota exhausted;
//   - 3xx, which the hardened client never follows, selects invalidResponse;
//   - every other status selects providerRejected with provider rejection.
//
// Rules whose StatusCode is zero also classify a ProviderReportedError, an
// error object inside a 2xx body or event stream. An unmatched
// ProviderReportedError selects invalidResponse.
type ErrorRule struct {
	// StatusCode is the HTTP status the rule matches. Zero matches every
	// non-2xx status and every ProviderReportedError.
	StatusCode int
	// ErrorToken matches when one of the tokens read at the wire format's
	// ErrorTokenPointers equals it exactly. Empty matches every body. A
	// non-empty value must match ^[A-Za-z0-9_.:-]{1,64}$.
	ErrorToken string
	// Outcome is the classification the rule selects.
	Outcome ErrorOutcome
}

// ProviderReportedError reports that a 2xx response body, or one event of a
// 2xx event stream, carries a provider error object instead of output. A
// wire format's DecodeResponse or DecodeStream returns it, possibly wrapped,
// so the pipeline can classify the error through the ErrorRules whose
// StatusCode is zero instead of treating it as a malformed response.
type ProviderReportedError struct {
	// ErrorTokens are the machine-readable tokens of the error object, read
	// with providerhttp.ReadErrorTokens at the wire format's
	// ErrorTokenPointers. The pipeline drops any token that does not match
	// ^[A-Za-z0-9_.:-]{1,64}$, so message text never reaches a Failure.
	ErrorTokens []string
}

// Error returns a fixed description without the error tokens.
func (*ProviderReportedError) Error() string {
	return "provider reported an error inside a 2xx response"
}

func validateErrorRules(rules []ErrorRule) error {
	for index, rule := range rules {
		if rule.StatusCode != 0 && (rule.StatusCode < 300 || rule.StatusCode > 599) {
			return fmt.Errorf("error rule %d status must be zero or a non-2xx status from 300 through 599", index)
		}
		if rule.ErrorToken != "" && !providerTokenPattern.MatchString(rule.ErrorToken) {
			return fmt.Errorf("error rule %d token must match %s", index, providerTokenPattern)
		}
		if rule.Outcome.disposition == errorDispositionInvalid || rule.Outcome.kind == "" {
			return fmt.Errorf("error rule %d outcome is invalid; use a textgen outcome constructor", index)
		}
	}
	return nil
}

func classifyErrorResponse(rules []ErrorRule, status int, tokens []string) ErrorOutcome {
	for _, rule := range rules {
		if rule.StatusCode != 0 && rule.StatusCode != status {
			continue
		}
		if rule.ErrorToken != "" && !containsToken(tokens, rule.ErrorToken) {
			continue
		}
		return rule.Outcome
	}
	return defaultErrorOutcome(status)
}

// classifyReportedError applies only status-independent rules, because a 2xx status carries no error class.
func classifyReportedError(rules []ErrorRule, tokens []string) ErrorOutcome {
	for _, rule := range rules {
		if rule.StatusCode != 0 {
			continue
		}
		if rule.ErrorToken != "" && !containsToken(tokens, rule.ErrorToken) {
			continue
		}
		return rule.Outcome
	}
	return InvalidResponseOutcome()
}

func defaultErrorOutcome(status int) ErrorOutcome {
	switch {
	case status == http.StatusTooManyRequests:
		return RetryOutcome(sdkgo.FailureRateLimit)
	case status == http.StatusRequestTimeout || (status >= 500 && status != http.StatusNotImplemented):
		return RetryOutcome(sdkgo.FailureAvailability)
	case status == http.StatusUnauthorized:
		return ProviderRejectedOutcome(sdkgo.FailureAuthentication)
	case status == http.StatusForbidden:
		return ProviderRejectedOutcome(sdkgo.FailureAuthorization)
	case status == http.StatusNotFound:
		return ProviderRejectedOutcome(sdkgo.FailureNotFound)
	case status == http.StatusConflict:
		return ProviderRejectedOutcome(sdkgo.FailureConflict)
	case status == http.StatusPaymentRequired:
		return QuotaExhaustedOutcome()
	case status >= 300 && status < 400:
		return InvalidResponseOutcome()
	default:
		return ProviderRejectedOutcome(sdkgo.FailureProviderRejection)
	}
}

func containsToken(tokens []string, token string) bool {
	for _, candidate := range tokens {
		if candidate == token {
			return true
		}
	}
	return false
}
