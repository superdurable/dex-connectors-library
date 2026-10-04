// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package s3 lists, inspects, reads, and stores objects in Amazon S3 and in
// S3-compatible stores, such as Cloudflare R2 and MinIO, as Dex connector Steps.
//
// Every request is signed with AWS Signature Version 4 by the official signer
// in github.com/aws/aws-sdk-go-v2/aws/signer/v4, from a static access key, an
// optional session token, and the configured Region. The connector sends each
// REST request itself, so it reads every response within its own limits, never
// follows a redirect, and leaves retries to Dex.
//
// PutObject is safe to repeat: Amazon S3 replaces the object at a key as a whole,
// and every attempt of one Step execution sends the same bytes, headers, and
// idempotency marker. In create-only mode a repeated attempt finds its own
// marker and reports the object as stored instead of alreadyExists.
//
// Applications use the generated operation-specific Step factories, such as
// NewHeadObjectStep and NewPutObjectStep, with a Connection built by
// NewProjectConnection or NewConnection. The runnable example in
// examples/report-archive uses every operation in one Flow.
package s3

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	providerName = "amazon-s3"
	// signingServiceName is the SigV4 service name for Amazon S3 and S3-compatible stores.
	signingServiceName = "s3"
	// operationDeadline keeps every request of one Invoke inside the 30-second Execute timeout.
	operationDeadline = 25 * time.Second
	// requestTimeout bounds one exchange, so putObject's create-only check still fits the deadline.
	requestTimeout = 12 * time.Second
	// maxListKeys is the Amazon S3 ListObjectsV2 page limit.
	maxListKeys = 1000
	// maxUploadBytesLimit caps putObject content, which travels through Dex Flow state.
	maxUploadBytesLimit = 5 << 20
	// emptyPayloadSHA256 is the hex SHA-256 of an empty body, which S3 requires in x-amz-content-sha256.
	emptyPayloadSHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	contentSHA256Header  = "X-Amz-Content-Sha256"
	requestIDHeader      = "X-Amz-Request-Id"
	bucketRegionHeader   = "X-Amz-Bucket-Region"
	versionIDHeader      = "X-Amz-Version-Id"
	metadataHeaderPrefix = "X-Amz-Meta-"
)

var (
	// errorCodePattern bounds an S3 error code to a machine token, so provider message text never escapes.
	errorCodePattern = regexp.MustCompile(`^[A-Za-z0-9.]{1,64}$`)
	// regionPattern accepts AWS Region codes and the names S3-compatible stores use, such as auto.
	regionPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?$`)
	// requestIDPattern accepts the request IDs Amazon S3, R2, and MinIO return.
	requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9+/=._-]{1,128}$`)
	// endpointPathPattern keeps a custom endpoint path free of characters that would need escaping.
	endpointPathPattern = regexp.MustCompile(`^(/[A-Za-z0-9._~-]+)*$`)
)

// awsPartitionDNSSuffixes copies the AWS SDK partition table; ISO-partition Regions must set endpoint.
var awsPartitionDNSSuffixes = []struct {
	regionPattern *regexp.Regexp
	dnsSuffix     string
}{
	{regexp.MustCompile(`^(us|eu|ap|sa|ca|me|af|il|mx)-\w+-\d+$`), "amazonaws.com"},
	{regexp.MustCompile(`^us-gov-\w+-\d+$`), "amazonaws.com"},
	{regexp.MustCompile(`^cn-\w+-\d+$`), "amazonaws.com.cn"},
	{regexp.MustCompile(`^eusc-de-\w+-\d+$`), "amazonaws.eu"},
}

// errRequestNotBuilt reports a request that could not be constructed or signed, so nothing was sent.
var errRequestNotBuilt = errors.New("S3 request could not be built")

// Option configures Client construction.
type Option func(*clientOptions)

type clientOptions struct {
	httpClient *http.Client
}

// WithHTTPClient replaces the HTTP client used for S3 requests. The caller keeps ownership of client and its
// transport. Requests use a copy that never follows redirects, and each exchange is bounded by 12 seconds so
// putObject's create-only check fits inside Dex's 30-second Execute timeout.
func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

// Client signs and sends S3 REST requests for one connection. It is immutable after New and safe for
// concurrent use by several Steps.
type Client struct {
	endpoint         endpointAddress
	isAmazonEndpoint bool
	addressingStyle  AddressingStyle
	region           string
	defaultBucket    string
	listPageSize     int
	maxResponseBytes int64
	maxTextBytes     int64
	maxUploadBytes   int64
	httpClient       *http.Client
	credentials      sdkgo.CredentialProvider[Credentials]
	signer           *v4.Signer
	now              func() time.Time
}

// endpointAddress is the validated scheme, host with optional port, and unescaped path prefix of the API.
type endpointAddress struct {
	scheme   string
	host     string
	basePath string
}

// objectLocation names one bucket and, for an object request, one key.
type objectLocation struct {
	bucket string
	key    string
	hasKey bool
}

// s3Request is one signed S3 REST call. Every header in header is signed.
type s3Request struct {
	method   string
	location objectLocation
	query    url.Values
	header   http.Header
	body     []byte
}

// operationSession carries the resolved credential and deadline of one Invoke.
type operationSession struct {
	context     context.Context
	call        sdkgo.Call
	credentials Credentials
}

// providerErrorResponse is the safe part of one non-2xx S3 response.
type providerErrorResponse struct {
	statusCode   int
	code         string
	bucketRegion string
	requestID    string
	retryAfter   time.Duration
}

// requestOutcome is the safe classification of one failed exchange, before an operation picks its branch.
type requestOutcome uint8

const (
	outcomeRetry requestOutcome = iota + 1
	outcomeNotFound
	outcomeRejected
	outcomeDefect
)

// classifiedFailure is one failed exchange mapped onto a retry or a branch category.
type classifiedFailure struct {
	outcome    requestOutcome
	failure    sdkgo.Failure
	retryAfter time.Duration
	requestID  string
}

// headObjectResult is one successful HEAD exchange; isInvalid marks metadata a Result cannot carry.
type headObjectResult struct {
	value     ObjectMetadata
	requestID string
	isInvalid bool
}

// s3ErrorDocument is the XML error body S3 returns for every failed request except HEAD.
type s3ErrorDocument struct {
	XMLName xml.Name `xml:"Error"`
	Code    string   `xml:"Code"`
}

// New validates configuration and constructs a client. It fails when the Region does not fit the endpoint,
// the endpoint is not HTTPS (loopback HTTP is accepted for a local store), a limit is outside its documented
// range, the default bucket is invalid, or credentials is nil.
func New(config Config, credentials sdkgo.CredentialProvider[Credentials], options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if credentials == nil {
		return nil, errors.New("credential provider is required")
	}
	if !regionPattern.MatchString(config.Region) {
		return nil, errors.New("S3 region must be a Region code such as eu-west-1, or auto for Cloudflare R2")
	}
	endpoint, isAmazonEndpoint, err := resolveEndpoint(config.Endpoint, config.Region)
	if err != nil {
		return nil, err
	}
	if config.DefaultBucket != "" {
		if err := validateBucketName(config.DefaultBucket); err != nil {
			return nil, fmt.Errorf("S3 defaultBucket: %w", err)
		}
	}
	if config.ListPageSize < 1 || config.ListPageSize > maxListKeys {
		return nil, fmt.Errorf("S3 listPageSize must be from 1 to %d", maxListKeys)
	}
	if config.MaxResponseBytes < 1 || config.MaxTextBytes < 1 {
		return nil, errors.New("S3 maxResponseBytes and maxTextBytes must be positive")
	}
	if config.MaxUploadBytes < 1 || config.MaxUploadBytes > maxUploadBytesLimit {
		return nil, fmt.Errorf("S3 maxUploadBytes must be from 1 to %d", maxUploadBytesLimit)
	}
	dependencies := clientOptions{}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("S3 connector option is nil")
		}
		option(&dependencies)
	}
	return &Client{
		endpoint: endpoint, isAmazonEndpoint: isAmazonEndpoint, addressingStyle: config.AddressingStyle,
		region: config.Region, defaultBucket: config.DefaultBucket, listPageSize: int(config.ListPageSize),
		maxResponseBytes: config.MaxResponseBytes, maxTextBytes: config.MaxTextBytes, maxUploadBytes: config.MaxUploadBytes,
		httpClient:  providerhttp.NewProviderHTTPClient(dependencies.httpClient, requestTimeout),
		credentials: credentials,
		signer:      v4.NewSigner(func(options *v4.SignerOptions) { options.DisableURIPathEscaping = true }),
		now:         time.Now,
	}, nil
}

// ListObjects returns the listObjects Query bound to this client.
func (client *Client) ListObjects() ListObjectsOperation { return ListObjectsOperation{client: client} }

// HeadObject returns the headObject Query bound to this client.
func (client *Client) HeadObject() HeadObjectOperation { return HeadObjectOperation{client: client} }

// GetObjectText returns the getObjectText Query bound to this client.
func (client *Client) GetObjectText() GetObjectTextOperation {
	return GetObjectTextOperation{client: client}
}

// PutObject returns the putObject Mutation bound to this client.
func (client *Client) PutObject() PutObjectOperation { return PutObjectOperation{client: client} }

// startSession resolves the credential, reread before every call; the caller must call the returned cancel.
func (client *Client) startSession(call sdkgo.Call, operationID string) (*operationSession, context.CancelFunc, *sdkgo.Failure) {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || validateResolvedCredentials(credentials) != nil {
		return nil, nil, failurePointer(operationID, sdkgo.FailureAuthentication,
			"S3 connection credentials are unavailable; save an access key ID and secret access key for the connection")
	}
	operationContext, cancel := context.WithTimeout(call.Context, operationDeadline)
	return &operationSession{context: operationContext, call: call, credentials: credentials}, cancel, nil
}

// resolveBucket returns the input bucket, or the connection's default bucket when the input leaves it blank.
func (client *Client) resolveBucket(bucket string) (string, error) {
	if bucket == "" {
		bucket = client.defaultBucket
	}
	if bucket == "" {
		return "", errors.New("bucket is required because the connection has no defaultBucket")
	}
	if err := validateBucketName(bucket); err != nil {
		return "", err
	}
	if client.addressingStyle == AddressingStyleVirtualHosted && strings.Contains(bucket, ".") && client.endpoint.scheme == "https" {
		return "", errors.New("a bucket name with a dot cannot use virtualHosted addressing over HTTPS; set addressingStyle to auto or path")
	}
	return bucket, nil
}

// validateObjectInput resolves the bucket and validates the key of an object request.
func (client *Client) validateObjectInput(bucket string, key string) (objectLocation, error) {
	resolvedBucket, err := client.resolveBucket(bucket)
	if err != nil {
		return objectLocation{}, err
	}
	if err := validateObjectKey(key); err != nil {
		return objectLocation{}, err
	}
	return objectLocation{bucket: resolvedBucket, key: key, hasKey: true}, nil
}

// headObject sends one HEAD request; a nil classifiedFailure means S3 answered 2xx.
func (client *Client) headObject(session *operationSession, operationID string, location objectLocation) (headObjectResult, *classifiedFailure) {
	response, err := client.exchange(session, s3Request{method: http.MethodHead, location: location})
	if errors.Is(err, errRequestNotBuilt) {
		return headObjectResult{}, &classifiedFailure{outcome: outcomeDefect, failure: newFailure(operationID, sdkgo.FailureLocalDefect, err.Error())}
	}
	if err != nil {
		return headObjectResult{}, &classifiedFailure{outcome: outcomeRetry, failure: transportFailure(operationID, "object metadata read")}
	}
	defer closeResponseBody(response)
	if !isSuccessStatus(response.StatusCode) {
		classified := classifyErrorResponse(operationID, "object", client.readErrorResponse(response, session.credentials), true)
		return headObjectResult{}, &classified
	}
	metadata, err := objectMetadataFromResponse(location, response, session.credentials)
	return headObjectResult{value: metadata, requestID: safeRequestID(response.Header), isInvalid: err != nil}, nil
}

// exchange signs and sends one request. The caller owns and must close the returned response body.
func (client *Client) exchange(session *operationSession, request s3Request) (*http.Response, error) {
	target, err := client.requestURL(request.location, request.query)
	if err != nil {
		return nil, errRequestNotBuilt
	}
	var body io.Reader
	payloadHash := emptyPayloadSHA256
	if request.body != nil {
		body = bytes.NewReader(request.body)
		digest := sha256.Sum256(request.body)
		payloadHash = hex.EncodeToString(digest[:])
	}
	httpRequest, err := http.NewRequestWithContext(session.context, request.method, target.String(), body)
	if err != nil {
		return nil, errRequestNotBuilt
	}
	httpRequest.URL = target
	for name, values := range request.header {
		httpRequest.Header[name] = values
	}
	if err := client.signRequest(httpRequest, session.credentials, payloadHash, client.now()); err != nil {
		return nil, errRequestNotBuilt
	}
	// Set after signing: an identity encoding keeps Go from decompressing a stored gzip object.
	httpRequest.Header.Set("Accept-Encoding", "identity")
	return client.httpClient.Do(httpRequest)
}

// signRequest adds x-amz-content-sha256 and the SigV4 Authorization header; S3 paths are escaped only once.
func (client *Client) signRequest(request *http.Request, credentials Credentials, payloadHash string, signingTime time.Time) error {
	request.Header.Set(contentSHA256Header, payloadHash)
	signingCredentials := aws.Credentials{
		AccessKeyID: credentials.AccessKeyID.Reveal(), SecretAccessKey: credentials.SecretAccessKey.Reveal(),
		SessionToken: credentials.SessionToken.Reveal(),
	}
	return client.signer.SignHTTP(request.Context(), signingCredentials, request, payloadHash, signingServiceName, client.region, signingTime.UTC())
}

// requestURL addresses the bucket in the host or the path and escapes the path the way SigV4 signs it.
func (client *Client) requestURL(location objectLocation, query url.Values) (*url.URL, error) {
	host, path := client.endpoint.host, client.endpoint.basePath
	if client.usesPathStyle(location.bucket) {
		path += "/" + location.bucket
		if location.hasKey {
			path += "/" + location.key
		}
	} else {
		host = location.bucket + "." + host
		path += "/" + location.key
	}
	target := &url.URL{Scheme: client.endpoint.scheme, Host: host, Path: path, RawPath: escapeS3Path(path)}
	if len(query) > 0 {
		target.RawQuery = strings.ReplaceAll(query.Encode(), "+", "%20")
	}
	return target, nil
}

// usesPathStyle applies addressingStyle; auto keeps Amazon S3 on virtual hosts except for dotted buckets.
func (client *Client) usesPathStyle(bucket string) bool {
	switch client.addressingStyle {
	case AddressingStylePath:
		return true
	case AddressingStyleVirtualHosted:
		return false
	default:
		return !client.isAmazonEndpoint || strings.Contains(bucket, ".")
	}
}

// readErrorResponse reads at most 64 KiB of a non-2xx body and keeps only its validated error code.
func (client *Client) readErrorResponse(response *http.Response, credentials Credentials) providerErrorResponse {
	errorResponse := providerErrorResponse{
		statusCode: response.StatusCode, requestID: safeRequestID(response.Header),
		retryAfter: providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), client.now()),
	}
	if region := response.Header.Get(bucketRegionHeader); regionPattern.MatchString(region) {
		errorResponse.bucketRegion = region
	}
	// An oversized or unreadable error body carries no usable code, so the status alone classifies it.
	contents, err := providerhttp.ReadBoundedBody(response.Body, providerhttp.MaxErrorBodyBytes)
	if err != nil || len(contents) == 0 {
		return errorResponse
	}
	var document s3ErrorDocument
	if xml.Unmarshal(contents, &document) == nil && errorCodePattern.MatchString(document.Code) &&
		!containsCredential(document.Code, credentials, true) {
		errorResponse.code = document.Code
	}
	return errorResponse
}

// classifyErrorResponse maps a non-2xx response onto Retry or a branch category, by status and error code.
func classifyErrorResponse(operationID string, subject string, response providerErrorResponse, isObjectRequest bool) classifiedFailure {
	classified := classifiedFailure{requestID: response.requestID, retryAfter: response.retryAfter}
	status, code := response.statusCode, response.code
	switch {
	case status == http.StatusTooManyRequests || code == "SlowDown":
		classified.outcome, classified.failure = outcomeRetry, newFailure(operationID, sdkgo.FailureRateLimit, "S3 asked the connector to slow down")
	case code == "RequestTimeout" || status == http.StatusRequestTimeout:
		classified.outcome, classified.failure = outcomeRetry, newFailure(operationID, sdkgo.FailureTransport, "S3 timed out the request")
	case code == "ConditionalRequestConflict" || code == "OperationAborted":
		classified.outcome, classified.failure = outcomeRetry, newFailure(operationID, sdkgo.FailureConflict, "a conflicting S3 operation on the key is in progress")
	case status == http.StatusTemporaryRedirect:
		classified.outcome, classified.failure = outcomeRetry, newFailure(operationID, sdkgo.FailureAvailability, "S3 redirected the request temporarily")
	case status >= 500 && status != http.StatusNotImplemented:
		classified.outcome, classified.failure = outcomeRetry, newFailure(operationID, sdkgo.FailureAvailability, statusMessage("S3 is temporarily unable to serve the "+subject, response))
	case status == http.StatusNotFound && isObjectRequest && code != "NoSuchBucket":
		classified.outcome, classified.failure = outcomeNotFound, newFailure(operationID, sdkgo.FailureNotFound, "the "+subject+" was not found")
	case status == http.StatusNotFound:
		classified.outcome, classified.failure = outcomeRejected, newFailure(operationID, sdkgo.FailureNotFound, statusMessage("S3 rejected the "+subject, response)+"; check the bucket name")
	case status == http.StatusMovedPermanently || code == "PermanentRedirect" || code == "AuthorizationHeaderMalformed":
		classified.outcome, classified.failure = outcomeRejected, newFailure(operationID, sdkgo.FailureProviderRejection, regionMismatchMessage(subject, response))
	case code == "RequestTimeTooSkewed":
		classified.outcome, classified.failure = outcomeRejected, newFailure(operationID, sdkgo.FailureAuthentication,
			"S3 rejected the "+subject+" because the Worker clock differs from S3 by more than 15 minutes")
	case isCredentialErrorCode(code) || status == http.StatusUnauthorized:
		classified.outcome, classified.failure = outcomeRejected, newFailure(operationID, sdkgo.FailureAuthentication,
			statusMessage("S3 rejected the connection credentials for the "+subject, response)+"; replace the access key or session token")
	case status == http.StatusForbidden:
		classified.outcome, classified.failure = outcomeRejected, newFailure(operationID, sdkgo.FailureAuthorization, statusMessage("S3 denied the "+subject, response))
	case status == http.StatusPreconditionFailed || status == http.StatusConflict:
		classified.outcome, classified.failure = outcomeRejected, newFailure(operationID, sdkgo.FailureConflict, statusMessage("S3 rejected the "+subject, response))
	default:
		classified.outcome, classified.failure = outcomeRejected, newFailure(operationID, sdkgo.FailureProviderRejection, statusMessage("S3 rejected the "+subject, response))
	}
	return classified
}

func (client *Client) receipt(call sdkgo.Call, requestID string, location objectLocation) sdkgo.Receipt {
	objectID := location.bucket
	if location.hasKey {
		objectID += "/" + location.key
	}
	return sdkgo.Receipt{
		CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: providerName,
		ProviderObjectID: objectID, ProviderRequestID: requestID, ObservedAt: client.now().UTC(),
	}
}

// queryAttemptForFailure maps a classified failure to Retry or a branch; blank notFound means rejected.
func queryAttemptForFailure[T any](classified classifiedFailure, receipt sdkgo.Receipt, notFound, rejected, defect sdkgo.BranchID, value T) sdkgo.QueryAttempt[T] {
	failure := classified.failure
	switch {
	case classified.outcome == outcomeRetry:
		return sdkgo.NewQueryRetry[T](failure, classified.retryAfter)
	case classified.outcome == outcomeNotFound && notFound != "":
		return sdkgo.NewQueryBranch(notFound, value, &failure, receipt)
	case classified.outcome == outcomeDefect:
		return sdkgo.NewQueryBranch(defect, value, &failure, receipt)
	default:
		return sdkgo.NewQueryBranch(rejected, value, &failure, receipt)
	}
}

// mutationAttemptForFailure converts a classified failure into a Mutation Retry or branch.
func mutationAttemptForFailure[T any](classified classifiedFailure, receipt sdkgo.Receipt, rejected, defect sdkgo.BranchID, value T) sdkgo.MutationAttempt[T] {
	failure := classified.failure
	switch classified.outcome {
	case outcomeRetry:
		return sdkgo.NewMutationRetry[T](failure, classified.retryAfter)
	case outcomeDefect:
		return sdkgo.NewMutationBranch(defect, value, &failure, receipt)
	default:
		return sdkgo.NewMutationBranch(rejected, value, &failure, receipt)
	}
}

// closeResponseBody releases the connection after the body was read or deliberately abandoned.
func closeResponseBody(response *http.Response) {
	// A close failure cannot change an outcome that was already classified.
	_ = response.Body.Close()
}

// resolveEndpoint validates a custom endpoint or derives the Amazon S3 Regional endpoint from region.
func resolveEndpoint(configured string, region string) (endpointAddress, bool, error) {
	isAmazonEndpoint := configured == ""
	if isAmazonEndpoint {
		dnsSuffix := ""
		for _, partition := range awsPartitionDNSSuffixes {
			if partition.regionPattern.MatchString(region) {
				dnsSuffix = partition.dnsSuffix
				break
			}
		}
		if dnsSuffix == "" {
			return endpointAddress{}, false, errors.New("S3 region is not an Amazon S3 Region; set endpoint for an S3-compatible store")
		}
		configured = "https://s3." + region + "." + dnsSuffix
	}
	validated, err := providerhttp.ValidateBaseURL(configured)
	if err != nil {
		return endpointAddress{}, false, fmt.Errorf("S3 endpoint: %w", err)
	}
	parsed, err := url.Parse(validated)
	if err != nil || !endpointPathPattern.MatchString(parsed.Path) || parsed.RawPath != "" {
		return endpointAddress{}, false, errors.New("S3 endpoint path may contain only letters, digits, and - . _ ~ segments")
	}
	return endpointAddress{scheme: parsed.Scheme, host: parsed.Host, basePath: parsed.Path}, isAmazonEndpoint, nil
}

// escapeS3Path percent-encodes every byte except unreserved characters and the slash, as SigV4 for S3 signs it.
func escapeS3Path(path string) string {
	var escaped strings.Builder
	escaped.Grow(len(path))
	for index := 0; index < len(path); index++ {
		character := path[index]
		if isUnreservedByte(character) || character == '/' {
			escaped.WriteByte(character)
			continue
		}
		fmt.Fprintf(&escaped, "%%%02X", character)
	}
	return escaped.String()
}

func isUnreservedByte(character byte) bool {
	return (character >= 'A' && character <= 'Z') || (character >= 'a' && character <= 'z') ||
		(character >= '0' && character <= '9') || character == '-' || character == '.' || character == '_' || character == '~'
}

func isSuccessStatus(status int) bool { return status >= 200 && status < 300 }

// isCredentialErrorCode reports S3 codes for an unknown key, a bad signature, or an expired or invalid token.
func isCredentialErrorCode(code string) bool {
	switch code {
	case "InvalidAccessKeyId", "SignatureDoesNotMatch", "ExpiredToken", "InvalidToken", "TokenRefreshRequired", "InvalidSecurity":
		return true
	default:
		return false
	}
}

// containsCredential reports whether value repeats a secret; access key IDs count only where S3 never echoes one.
func containsCredential(value string, credentials Credentials, isAccessKeyIDSensitive bool) bool {
	secrets := []string{credentials.SecretAccessKey.Reveal(), credentials.SessionToken.Reveal()}
	if isAccessKeyIDSensitive {
		secrets = append(secrets, credentials.AccessKeyID.Reveal())
	}
	for _, secret := range secrets {
		if secret != "" && strings.Contains(value, secret) {
			return true
		}
	}
	return false
}

// statusMessage names the HTTP status and, when S3 sent one, its validated error code, never its message text.
func statusMessage(prefix string, response providerErrorResponse) string {
	if response.code != "" {
		return fmt.Sprintf("%s with HTTP %d %s", prefix, response.statusCode, response.code)
	}
	return fmt.Sprintf("%s with HTTP %d", prefix, response.statusCode)
}

// regionMismatchMessage names the bucket's Region when S3 reported it in x-amz-bucket-region.
func regionMismatchMessage(subject string, response providerErrorResponse) string {
	message := statusMessage("S3 rejected the "+subject, response) + " because the bucket needs another Region or endpoint"
	if response.bucketRegion != "" {
		message += "; the bucket is in " + response.bucketRegion + ", so set region to " + response.bucketRegion
	}
	return message
}

func safeRequestID(header http.Header) string {
	if requestID := header.Get(requestIDHeader); requestIDPattern.MatchString(requestID) {
		return requestID
	}
	return ""
}

func newFailure(operationID string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: providerName, Operation: operationID, Message: message}
}

func failurePointer(operationID string, kind sdkgo.FailureKind, message string) *sdkgo.Failure {
	failure := newFailure(operationID, kind, message)
	return &failure
}

func transportFailure(operationID string, subject string) sdkgo.Failure {
	return newFailure(operationID, sdkgo.FailureTransport, "S3 could not be reached or did not finish the "+subject)
}

func defectFailure(operationID string, err error) *sdkgo.Failure {
	return failurePointer(operationID, sdkgo.FailureValidation, err.Error())
}
