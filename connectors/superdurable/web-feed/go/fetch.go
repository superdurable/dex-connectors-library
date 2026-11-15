// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package webfeed

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	// maxRedirects bounds the redirect hops one read follows, which also ends redirect loops.
	maxRedirects = 5
	// maxURLBytes bounds input, redirect, and output URLs.
	maxURLBytes = 2048
	// maxValidatorBytes bounds an etag or lastModified validator in either direction.
	maxValidatorBytes = 1024
)

// addressRefusalMessage starts every refused-address failure, whether the literal or the dialer refused it.
const addressRefusalMessage = "the URL resolves to an address the connector will not connect to: "

var (
	awsIPv6MetadataAddress = netip.MustParseAddr("fd00:ec2::254")
	sharedAddressSpace     = netip.MustParsePrefix("100.64.0.0/10")
)

var mediaTypePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9.+-]{0,63}/[a-z0-9][a-z0-9.+-]{0,63}$`)

// fetchClass is the outcome class of one read, before an operation maps it to its own branch.
type fetchClass int

const (
	fetchRetry fetchClass = iota + 1
	fetchNotFound
	fetchProviderRejected
	fetchUnreachable
	fetchInvalidResponse
	fetchDefect
)

// fetchFailure is a classified failure of one read. It never holds a URL, a header, or body text.
type fetchFailure struct {
	class      fetchClass
	kind       sdkgo.FailureKind
	message    string
	retryAfter time.Duration
}

// operationBranches names the branch each failure class selects for one operation.
type operationBranches struct {
	operationID      string
	notFound         sdkgo.BranchID
	providerRejected sdkgo.BranchID
	unreachable      sdkgo.BranchID
	invalidResponse  sdkgo.BranchID
	defect           sdkgo.BranchID
}

// validators are the HTTP cache validators of a previous read, sent on a conditional GET.
type validators struct {
	etag         string
	lastModified string
}

// documentRequest describes one GET that follows redirects and reads one body.
type documentRequest struct {
	target     *url.URL
	accept     string
	validators validators
	maxBytes   int64
	// isPrefixRead reads only the first maxBytes and reports truncation instead of rejecting a longer body.
	isPrefixRead bool
	// mayBeGzip decompresses a body that starts with the gzip magic number.
	mayBeGzip        bool
	acceptsMediaType func(mediaType string) bool
	expectedContent  string
}

// fetchedDocument is a successful read, or a 304 answer to a conditional GET.
type fetchedDocument struct {
	finalURL      *url.URL
	mediaType     string
	charsetLabel  string
	body          []byte
	isTruncated   bool
	isNotModified bool
	validators    validators
}

// htmlContentType rebuilds the page's content type for HTML charset detection.
func (document fetchedDocument) htmlContentType() string {
	if document.charsetLabel == "" {
		return "text/html"
	}
	return "text/html; charset=" + document.charsetLabel
}

// attemptFromFailure converts a classified failure into the operation's Query attempt.
func attemptFromFailure[T any](branches operationBranches, failure *fetchFailure) sdkgo.QueryAttempt[T] {
	if failure.class == fetchRetry {
		return sdkgo.NewQueryRetry[T](newFailure(branches.operationID, failure.kind, failure.message), failure.retryAfter)
	}
	branch := branches.defect
	switch failure.class {
	case fetchNotFound:
		branch = branches.notFound
	case fetchProviderRejected:
		branch = branches.providerRejected
	case fetchUnreachable:
		branch = branches.unreachable
	case fetchInvalidResponse:
		branch = branches.invalidResponse
	}
	return failedBranch[T](branches.operationID, branch, failure.kind, failure.message)
}

// fetchDocument sends the GET, follows redirects through the address policy, and reads the final body.
func (client *Client) fetchDocument(ctx context.Context, request documentRequest) (fetchedDocument, *fetchFailure) {
	current := request.target
	for hop := 0; ; hop++ {
		if reason := refusedAddressLiteralReason(current.Hostname()); reason != "" {
			return fetchedDocument{}, &fetchFailure{class: fetchUnreachable, kind: sdkgo.FailureValidation, message: addressRefusalMessage + reason}
		}
		response, failure := client.send(ctx, current, request)
		if failure != nil {
			return fetchedDocument{}, failure
		}
		if !isRedirectStatus(response.StatusCode) {
			// Closing a fully or partly read GET response cannot change the read's outcome.
			defer func() { _ = response.Body.Close() }()
			return client.readResponse(response, current, request)
		}
		location := response.Header.Get("Location")
		// A redirect body is never read, so a failed close cannot change the outcome.
		_ = response.Body.Close()
		if hop == maxRedirects {
			return fetchedDocument{}, invalidResponse(fmt.Sprintf("the URL redirected more than %d times", maxRedirects))
		}
		next, err := resolveRedirectLocation(current, location)
		if err != nil {
			return fetchedDocument{}, invalidResponse("a redirect named no usable http or https URL")
		}
		current = next
	}
}

func (client *Client) send(ctx context.Context, target *url.URL, request documentRequest) (*http.Response, *fetchFailure) {
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, &fetchFailure{class: fetchDefect, kind: sdkgo.FailureValidation, message: "the URL cannot be requested"}
	}
	httpRequest.Header.Set("Accept", request.accept)
	httpRequest.Header.Set("User-Agent", client.userAgent)
	if request.validators.etag != "" {
		httpRequest.Header.Set("If-None-Match", request.validators.etag)
	}
	if request.validators.lastModified != "" {
		httpRequest.Header.Set("If-Modified-Since", request.validators.lastModified)
	}
	response, err := client.httpClient.Do(httpRequest)
	if err != nil {
		return nil, classifyTransportError(err)
	}
	return response, nil
}

// classifyTransportError separates failures that a repeat cannot fix from transient ones.
func classifyTransportError(err error) *fetchFailure {
	var refusal *providerhttp.AddressRefusal
	if errors.As(err, &refusal) {
		return &fetchFailure{class: fetchUnreachable, kind: sdkgo.FailureValidation, message: addressRefusalMessage + refusal.Reason}
	}
	var dnsError *net.DNSError
	if errors.As(err, &dnsError) && dnsError.IsNotFound {
		return &fetchFailure{class: fetchUnreachable, kind: sdkgo.FailureTransport, message: "the host does not exist"}
	}
	if isCertificateError(err) {
		return &fetchFailure{class: fetchUnreachable, kind: sdkgo.FailureTransport, message: "the site's TLS certificate failed verification"}
	}
	var recordHeaderError tls.RecordHeaderError
	if errors.As(err, &recordHeaderError) {
		return &fetchFailure{class: fetchUnreachable, kind: sdkgo.FailureTransport, message: "the site does not serve https on the requested port"}
	}
	return &fetchFailure{class: fetchRetry, kind: sdkgo.FailureTransport, message: "the request failed before a complete response arrived"}
}

// refusedAddressLiteralReason applies the PublicHTTPS rules to an IP-literal host before any request, so a
// refused literal selects unreachable without a connection attempt. Named hosts are checked at dial time.
func refusedAddressLiteralReason(hostname string) string {
	address, err := netip.ParseAddr(hostname)
	if err != nil {
		return ""
	}
	address = address.WithZone("").Unmap()
	switch {
	case address.IsLinkLocalUnicast(), address.IsLinkLocalMulticast(), address == awsIPv6MetadataAddress:
		return "link-local addresses, which include the cloud metadata address, are never allowed"
	case address.IsUnspecified(), address.IsMulticast():
		return "only unicast addresses are allowed"
	case address.IsLoopback(), address.IsPrivate(), sharedAddressSpace.Contains(address):
		return "only public addresses are allowed"
	}
	return ""
}

func isCertificateError(err error) bool {
	var verificationError *tls.CertificateVerificationError
	var unknownAuthority x509.UnknownAuthorityError
	var hostnameError x509.HostnameError
	var invalidCertificate x509.CertificateInvalidError
	return errors.As(err, &verificationError) || errors.As(err, &unknownAuthority) ||
		errors.As(err, &hostnameError) || errors.As(err, &invalidCertificate)
}

// readResponse classifies the status of the final response and reads its body within the request's bound.
func (client *Client) readResponse(response *http.Response, finalURL *url.URL, request documentRequest) (fetchedDocument, *fetchFailure) {
	status := response.StatusCode
	switch {
	case status == http.StatusNotModified:
		if request.validators == (validators{}) {
			return fetchedDocument{}, invalidResponse("the site answered 304 Not Modified to a request without validators")
		}
		refreshed := readValidators(response.Header)
		if refreshed.etag == "" {
			refreshed.etag = request.validators.etag
		}
		if refreshed.lastModified == "" {
			refreshed.lastModified = request.validators.lastModified
		}
		return fetchedDocument{finalURL: finalURL, isNotModified: true, validators: refreshed}, nil
	case status == http.StatusNotFound || status == http.StatusGone:
		return fetchedDocument{}, &fetchFailure{class: fetchNotFound, kind: sdkgo.FailureNotFound, message: "the site answered HTTP " + strconv.Itoa(status)}
	case status == http.StatusTooManyRequests:
		return fetchedDocument{}, &fetchFailure{class: fetchRetry, kind: sdkgo.FailureRateLimit, message: "the site answered HTTP 429", retryAfter: client.retryAfter(response)}
	case status == http.StatusRequestTimeout || status == http.StatusTooEarly || status >= 500:
		return fetchedDocument{}, &fetchFailure{class: fetchRetry, kind: sdkgo.FailureAvailability, message: "the site answered HTTP " + strconv.Itoa(status), retryAfter: client.retryAfter(response)}
	case status >= 400:
		return fetchedDocument{}, &fetchFailure{class: fetchProviderRejected, kind: sdkgo.FailureProviderRejection, message: "the site refused the request with HTTP " + strconv.Itoa(status)}
	case status < 200 || status >= 300 || status == http.StatusNoContent:
		return fetchedDocument{}, invalidResponse("the site answered HTTP " + strconv.Itoa(status) + " instead of " + request.expectedContent)
	}
	mediaType, charsetLabel := parseContentType(response.Header.Get("Content-Type"))
	if !request.acceptsMediaType(mediaType) {
		return fetchedDocument{}, invalidResponse("the URL serves " + describeMediaType(mediaType) + ", not " + request.expectedContent)
	}
	if !request.isPrefixRead && response.ContentLength > request.maxBytes {
		return fetchedDocument{}, tooLarge(request.expectedContent)
	}
	body, isTruncated, failure := readBody(response.Body, request)
	if failure != nil {
		return fetchedDocument{}, failure
	}
	return fetchedDocument{
		finalURL: finalURL, mediaType: mediaType, charsetLabel: charsetLabel, body: body,
		isTruncated: isTruncated, validators: readValidators(response.Header),
	}, nil
}

func readBody(body io.Reader, request documentRequest) ([]byte, bool, *fetchFailure) {
	if request.isPrefixRead {
		contents, err := io.ReadAll(io.LimitReader(body, request.maxBytes+1))
		if err != nil {
			return nil, false, interruptedResponse()
		}
		if int64(len(contents)) > request.maxBytes {
			return contents[:request.maxBytes], true, nil
		}
		return contents, false, nil
	}
	contents, err := providerhttp.ReadBoundedBody(body, request.maxBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return nil, false, tooLarge(request.expectedContent)
	}
	if err != nil {
		return nil, false, interruptedResponse()
	}
	if request.mayBeGzip && len(contents) >= 2 && contents[0] == 0x1f && contents[1] == 0x8b {
		reader, err := gzip.NewReader(bytes.NewReader(contents))
		if err != nil {
			return nil, false, invalidResponse("the gzip-compressed body is malformed")
		}
		contents, err = providerhttp.ReadBoundedBody(reader, request.maxBytes)
		if errors.Is(err, providerhttp.ErrBodyTooLarge) {
			return nil, false, tooLarge(request.expectedContent)
		}
		if err != nil {
			return nil, false, invalidResponse("the gzip-compressed body is malformed")
		}
	}
	return contents, false, nil
}

func (client *Client) retryAfter(response *http.Response) time.Duration {
	return providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), client.clock())
}

// readValidators keeps the response's etag and Last-Modified only when each can be sent back unchanged.
func readValidators(header http.Header) validators {
	result := validators{etag: header.Get("ETag"), lastModified: header.Get("Last-Modified")}
	if !isPrintableHeaderText(result.etag, maxValidatorBytes) {
		result.etag = ""
	}
	if !isPrintableHeaderText(result.lastModified, maxValidatorBytes) {
		result.lastModified = ""
	}
	return result
}

// validatorsFromInput checks the validators an application passes back from a previous read.
func validatorsFromInput(etag, lastModified string) (validators, error) {
	if etag != "" && !isPrintableHeaderText(etag, maxValidatorBytes) {
		return validators{}, errors.New("etag must be at most 1024 printable ASCII characters, as a previous read returned it")
	}
	if lastModified != "" && !isPrintableHeaderText(lastModified, maxValidatorBytes) {
		return validators{}, errors.New("lastModified must be at most 1024 printable ASCII characters, as a previous read returned it")
	}
	return validators{etag: etag, lastModified: lastModified}, nil
}

func parseContentType(value string) (string, string) {
	if strings.TrimSpace(value) == "" {
		return "", ""
	}
	mediaType, parameters, err := mime.ParseMediaType(value)
	if err != nil {
		return "unparsable", ""
	}
	return strings.ToLower(mediaType), parameters["charset"]
}

// describeMediaType names a provider media type in a Failure only when it is a plain token.
func describeMediaType(mediaType string) string {
	if mediaTypePattern.MatchString(mediaType) {
		return mediaType
	}
	return "an unsupported media type"
}

func isRedirectStatus(status int) bool {
	switch status {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		return true
	}
	return false
}

// parseRequestURL returns the https URL to request. Errors never repeat the value, which may carry a token.
func parseRequestURL(value string) (*url.URL, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, errors.New("url is required")
	}
	if len(value) > maxURLBytes {
		return nil, errors.New("url must be at most 2048 bytes")
	}
	for _, character := range value {
		if character <= ' ' || character == 0x7F {
			return nil, errors.New("url cannot contain spaces or control characters")
		}
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return nil, errors.New("url must be an absolute http or https URL")
	}
	return upgradeToHTTPS(parsed)
}

// resolveRedirectLocation resolves a Location header against the URL that answered with it.
func resolveRedirectLocation(current *url.URL, location string) (*url.URL, error) {
	location = strings.TrimSpace(location)
	if location == "" || len(location) > maxURLBytes {
		return nil, errors.New("redirect location is missing or too long")
	}
	next, err := current.Parse(location)
	if err != nil {
		return nil, err
	}
	return upgradeToHTTPS(next)
}

// upgradeToHTTPS returns the https URL for an absolute http or https URL; plain http may name only port 80.
func upgradeToHTTPS(parsed *url.URL) (*url.URL, error) {
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Opaque != "" || parsed.Hostname() == "" {
		return nil, errors.New("url must be an absolute http or https URL with a host")
	}
	if parsed.User != nil {
		return nil, errors.New("url cannot contain user information")
	}
	upgraded := *parsed
	upgraded.Fragment, upgraded.RawFragment = "", ""
	if upgraded.Scheme == "http" {
		if port := upgraded.Port(); port != "" && port != "80" {
			return nil, errors.New("an http url can name only port 80, because the connector requests it over https")
		}
		upgraded.Scheme = "https"
		upgraded.Host = hostWithoutPort(upgraded.Hostname())
	}
	return &upgraded, nil
}

func hostWithoutPort(hostname string) string {
	if strings.Contains(hostname, ":") {
		return "[" + hostname + "]"
	}
	return hostname
}

func invalidResponse(message string) *fetchFailure {
	return &fetchFailure{class: fetchInvalidResponse, kind: sdkgo.FailureProtocol, message: message}
}

func tooLarge(expectedContent string) *fetchFailure {
	return &fetchFailure{class: fetchInvalidResponse, kind: sdkgo.FailureResponseTooLarge, message: "the response is larger than the connection's limit for " + expectedContent}
}

func interruptedResponse() *fetchFailure {
	return &fetchFailure{class: fetchRetry, kind: sdkgo.FailureTransport, message: "the response was interrupted"}
}

func defectFailure(err error) *fetchFailure {
	return &fetchFailure{class: fetchDefect, kind: sdkgo.FailureValidation, message: err.Error()}
}
