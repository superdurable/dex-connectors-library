// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package docusign

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	productionAccountServerURL = "https://account.docusign.com"
	developerAccountServerURL  = "https://account-d.docusign.com"
	// developerAPIHost is the only eSignature host of the developer environment.
	developerAPIHost = "demo.docusign.net"
	// productionAPIHostSuffix ends every production eSignature host, such as na3.docusign.net.
	productionAPIHostSuffix = ".docusign.net"
	// burstRateLimitWindow is DocuSign's documented 30-second burst window.
	burstRateLimitWindow = 30 * time.Second
)

var (
	docusignGUIDPattern     = regexp.MustCompile(`^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}$`)
	productionHostLabel     = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
	docusignTimestampLayout = time.RFC3339Nano

	// docusignRateLimitCodes are the documented hourly and burst limit errors, compared without case.
	docusignRateLimitCodes = []string{
		"HOURLY_APIINVOCATION_LIMIT_EXCEEDED", "BURST_APIINVOCATION_LIMIT_EXCEEDED",
		"HOURLY_ENVELOPE_POLLING_LIMIT_EXCEEDED", "BURST_ENVELOPE_POLLING_LIMIT_EXCEEDED",
		"HOURLY_APIINVOCATION_ENVELOPE_LIMIT_EXCEEDED", "BURST_APIINVOCATION_ENVELOPE_LIMIT_EXCEEDED",
	}
	docusignAuthenticationCodes = []string{"USER_AUTHENTICATION_FAILED", "AUTHORIZATION_INVALID_TOKEN", "PARTNER_AUTHENTICATION_FAILED"}
	docusignAuthorizationCodes  = []string{
		"ACCOUNT_LACKS_PERMISSIONS", "USER_LACKS_PERMISSIONS", "USER_DOES_NOT_BELONG_TO_SPECIFIED_ACCOUNT",
		"ACCOUNT_NOT_AUTHORIZED_FOR_ENVELOPE", "USER_NOT_ENVELOPE_SENDER_OR_RECIPIENT", "USER_LACKS_MEMBERSHIP",
		"USER_NOT_ACCOUNT_ADMIN",
	}
	docusignNotFoundCodes = []string{"ENVELOPE_DOES_NOT_EXIST", "RESOURCE_NOT_FOUND", "DOCUMENT_DOES_NOT_EXIST"}
)

// docusignEnvironment is the account server and eSignature hosts of one authorization method.
type docusignEnvironment struct {
	accountServerURL string
	isDeveloper      bool
}

// resolvedAccount is the API account a connection's requests address and its validated base URI.
type resolvedAccount struct {
	accountID  string
	baseURI    string
	resolvedAt time.Time
}

// docusignEnvironmentFor maps a connection's authorization method to its environment.
func docusignEnvironmentFor(authMethodID string) (docusignEnvironment, bool) {
	switch authMethodID {
	case ProductionOAuthAuthMethodID:
		return docusignEnvironment{accountServerURL: productionAccountServerURL}, true
	case DeveloperOAuthAuthMethodID:
		return docusignEnvironment{accountServerURL: developerAccountServerURL, isDeveloper: true}, true
	default:
		return docusignEnvironment{}, false
	}
}

// isAllowedAPIHost accepts demo.docusign.net for developer, or one label below docusign.net for production.
func (environment docusignEnvironment) isAllowedAPIHost(host string) bool {
	if environment.isDeveloper {
		return host == developerAPIHost
	}
	label, isCut := strings.CutSuffix(host, productionAPIHostSuffix)
	return isCut && label != "demo" && productionHostLabel.MatchString(label)
}

// apiURL joins the account's REST base path, path, and query.
func (account resolvedAccount) apiURL(path string, query url.Values) string {
	target := account.baseURI + "/restapi/v2.1/accounts/" + url.PathEscape(account.accountID) + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	return target
}

// selectAccount picks the configured or default account from /oauth/userinfo and validates its base URI.
func selectAccount(body []byte, configuredAccountID string, environment docusignEnvironment) (resolvedAccount, error) {
	var decoded struct {
		Accounts []struct {
			AccountID string          `json:"account_id"`
			IsDefault json.RawMessage `json:"is_default"`
			BaseURI   string          `json:"base_uri"`
		} `json:"accounts"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil || len(decoded.Accounts) == 0 {
		return resolvedAccount{}, fmt.Errorf("%w: account information lists no accounts", errDocuSignResponseMalformed)
	}
	for _, account := range decoded.Accounts {
		isDefault := string(account.IsDefault) == "true" || string(account.IsDefault) == `"true"`
		isSelected := (configuredAccountID == "" && isDefault) ||
			(configuredAccountID != "" && strings.EqualFold(account.AccountID, configuredAccountID))
		if !isSelected {
			continue
		}
		if !isDocuSignGUID(account.AccountID) {
			return resolvedAccount{}, fmt.Errorf("%w: the account ID is not a GUID", errDocuSignResponseMalformed)
		}
		baseURI, err := validateBaseURI(account.BaseURI, environment)
		if err != nil {
			return resolvedAccount{}, err
		}
		return resolvedAccount{accountID: lowercaseGUID(account.AccountID), baseURI: baseURI}, nil
	}
	if configuredAccountID != "" {
		return resolvedAccount{}, errors.New("the authorizing DocuSign user does not belong to the configured accountId; correct accountId or authorize as a member of that account")
	}
	return resolvedAccount{}, errors.New("DocuSign reports no default account for the authorizing user; set the connection's accountId")
}

// validateBaseURI accepts exactly https://<DocuSign eSignature host> and returns it without a slash.
func validateBaseURI(value string, environment docusignEnvironment) (string, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Port() != "" ||
		parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") ||
		!environment.isAllowedAPIHost(parsed.Host) {
		return "", fmt.Errorf("%w: the account base URI is not a DocuSign eSignature host of this environment", errDocuSignResponseMalformed)
	}
	return "https://" + parsed.Host, nil
}

// newAccountResolutionKey binds a cache entry to the connection and a digest of its access token.
func newAccountResolutionKey(connection sdkgo.ConnectionRef, credentials Credentials) accountResolutionKey {
	return accountResolutionKey{connection: connection, accessTokenDigest: sha256.Sum256([]byte(credentials.AccessToken.Reveal()))}
}

// untilNextHour is the wait until DocuSign's hourly counters reset at the top of the hour.
func untilNextHour(now time.Time) time.Duration {
	return now.Truncate(time.Hour).Add(time.Hour).Sub(now)
}

// readDocuSignErrorCode returns the errorCode of an eSignature error or the error of an OAuth error.
func readDocuSignErrorCode(body []byte) string {
	tokens := providerhttp.ReadErrorTokens(body, []string{"/errorCode", "/error"})
	if len(tokens) == 0 {
		return ""
	}
	return tokens[0]
}

func isDocuSignRateLimitCode(errorCode string) bool {
	return isDocuSignCodeIn(errorCode, docusignRateLimitCodes)
}

func isDocuSignCodeIn(errorCode string, codes []string) bool {
	return errorCode != "" && slices.ContainsFunc(codes, func(code string) bool { return strings.EqualFold(code, errorCode) })
}

// docusignRateLimitDelay waits out a burst, X-RateLimit-Reset, Retry-After, or the hourly reset.
func docusignRateLimitDelay(errorCode string, header http.Header, now time.Time) time.Duration {
	if strings.HasPrefix(strings.ToUpper(errorCode), "BURST_") {
		return burstRateLimitWindow
	}
	if resetAt, err := strconv.ParseInt(strings.TrimSpace(header.Get("X-RateLimit-Reset")), 10, 64); err == nil && resetAt > 0 {
		if delay := time.Unix(resetAt, 0).Sub(now); delay > 0 {
			return min(delay, providerhttp.MaxRetryAfterDelay)
		}
	}
	if delay := providerhttp.ParseRetryAfter(header.Get("Retry-After"), now); delay > 0 {
		return delay
	}
	if strings.HasPrefix(strings.ToUpper(errorCode), "HOURLY_") {
		return untilNextHour(now)
	}
	return 0
}

// describeDocuSignError appends DocuSign's machine-readable errorCode, never its message text.
func describeDocuSignError(message string, errorCode string) string {
	if errorCode == "" {
		return message
	}
	return message + " (" + errorCode + ")"
}

func docusignFailure(operationID string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: ConnectorID, Operation: operationID, Message: message}
}

func docusignFailurePointer(operationID string, kind sdkgo.FailureKind, message string) *sdkgo.Failure {
	failure := docusignFailure(operationID, kind, message)
	return &failure
}

// responseFailureKind names a read or decode failure: an oversized body or a malformed one.
func responseFailureKind(err error) sdkgo.FailureKind {
	if errors.Is(err, errDocuSignResponseTooLarge) {
		return sdkgo.FailureResponseTooLarge
	}
	return sdkgo.FailureProtocol
}

func responseFailureMessage(err error) string {
	if errors.Is(err, errDocuSignResponseTooLarge) {
		return errDocuSignResponseTooLarge.Error()
	}
	return errDocuSignResponseMalformed.Error()
}

// isDocuSignGUID reports whether value is a hyphenated GUID, the form of envelope, template, and account IDs.
func isDocuSignGUID(value string) bool {
	return docusignGUIDPattern.MatchString(value)
}

// lowercaseGUID lowercases a GUID so IDs compare alike whatever case DocuSign or a caller used.
func lowercaseGUID(value string) string {
	return strings.ToLower(value)
}

// validateEnvelopeID checks an envelope ID before it becomes a URL path segment.
func validateEnvelopeID(envelopeID string) error {
	if !isDocuSignGUID(envelopeID) {
		return errors.New("envelopeId must be a DocuSign envelope ID GUID, such as 00000000-0000-0000-0000-000000000000")
	}
	return nil
}

// parseOptionalDocuSignTime reads a DocuSign timestamp such as 2016-10-05T21:18:12.3330000Z; blank is zero.
func parseOptionalDocuSignTime(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(docusignTimestampLayout, value)
	if err != nil {
		return nil, errDocuSignResponseMalformed
	}
	utc := parsed.UTC()
	return &utc, nil
}
