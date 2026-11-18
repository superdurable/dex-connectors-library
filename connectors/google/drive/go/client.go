// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package drive implements bounded Google Drive operations: name-based file
// search, file metadata reads, bounded text reads, and duplicate-safe uploads,
// plus fileCreated, fileUpdated, and fileTrashed Triggers that share one durable
// changes.list poller per connection.
//
// Applications use the generated operation-specific Step factories, such as
// NewSearchFilesStep and NewUploadFileStep, with a Connection built by
// NewProjectConnection or NewConnection. The runnable example in
// examples/text-copy shows every operation in one Flow, and
// examples/file-intake starts one Flow per created file with
// NewProjectFileTriggerRunner.
package drive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

const (
	driveProviderName     = "google-drive"
	driveAPIPath          = "/drive/v3"
	driveUploadAPIPath    = "/upload/drive/v3"
	driveRootFolderAlias  = "root"
	defaultRequestTimeout = 25 * time.Second
	readDeadlineMargin    = time.Second
	// maxSearchPageSize bounds every searchFiles page, so one Result stays small.
	maxSearchPageSize = 100
	// maxPollPageSize is the poll Trigger kit's per-page bound, below Drive's documented 1000.
	maxPollPageSize = pollingtrigger.MaxChangesPerPage
	// googleMultipartUploadLimitBytes is Google's documented 5 MB multipart upload limit.
	googleMultipartUploadLimitBytes = 5 << 20
	// fullFileFields selects every metadata field that File exposes.
	fullFileFields = "id,name,mimeType,description,parents,driveId,createdTime,modifiedTime,size,md5Checksum," +
		"version,webViewLink,trashed,owners(displayName,emailAddress),lastModifyingUser(displayName,emailAddress)," +
		"shortcutDetails(targetId,targetMimeType)"
)

// driveIDPattern accepts Drive file, folder, and shared-drive IDs and the root alias.
var driveIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,256}$`)

// googleErrorTokenPointers locate Google's machine-readable error reason and status.
var googleErrorTokenPointers = []string{"/error/errors/0/reason", "/error/status"}

// googleRateLimitReasons are 403 reasons that Google documents as retryable rate limits.
var googleRateLimitReasons = map[string]bool{"rateLimitExceeded": true, "userRateLimitExceeded": true}

// Option configures Client construction.
type Option func(*clientOptions)

type clientOptions struct {
	httpClient *http.Client
	now        func() time.Time
	logger     *slog.Logger
}

// WithHTTPClient overrides the default 25-second HTTP client used for Drive and
// token requests. The connector uses a copy that never follows redirects; the
// caller retains ownership of the original client and its transport.
func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

// WithLogger sends the file Trigger runner's records to logger: failed and rate-limited polls, cursor
// seeds and resets, delivery retries, and the durable inboxes that NewProjectFileTriggerRunner creates.
// Without this option, or with a nil logger, records go to slog.Default() as of each record. Records
// carry connector, connection, feed, binding, and event IDs and error messages, never file names, page
// tokens, or credentials.
func WithLogger(logger *slog.Logger) Option {
	return func(options *clientOptions) { options.logger = logger }
}

// Client executes authenticated Google Drive requests for connector operations.
// A Client is immutable after New and safe for concurrent use by Dex Workers.
type Client struct {
	apiBaseURL       string
	uploadBaseURL    string
	httpClient       *http.Client
	credentials      CredentialSource
	refreshDriver    sdkgo.CredentialRefreshDriver[Credentials]
	maxResponseBytes int64
	maxTextBytes     int64
	maxUploadBytes   int64
	searchPageSize   int
	pollInterval     time.Duration
	pollPageSize     int
	logger           *slog.Logger
	now              func() time.Time
}

// FileSummary is the partial file record that SearchFiles returns for
// name-to-ID resolution.
type FileSummary struct {
	// ID is the stable Drive file ID. A Google Sheets file ID is also its spreadsheet ID.
	ID string `json:"id"`
	// Name is the file title shown in Google Drive.
	Name string `json:"name"`
	// MimeType is the Drive MIME type, such as application/vnd.google-apps.spreadsheet.
	MimeType string `json:"mimeType"`
	// Parents lists the parent folder ID; Drive allows at most one parent.
	Parents []string `json:"parents,omitempty"`
	// ModifiedTime is the last modification time reported by Drive.
	ModifiedTime time.Time `json:"modifiedTime,omitzero"`
	// WebViewLink opens the file in a browser for a user who can access it.
	WebViewLink string `json:"webViewLink,omitempty"`
}

// File is the full metadata record that GetFile and UploadFile return.
type File struct {
	// ID is the stable Drive file ID.
	ID string `json:"id"`
	// Name is the file title shown in Google Drive.
	Name string `json:"name"`
	// MimeType is the Drive MIME type.
	MimeType string `json:"mimeType"`
	// Description is the optional file description.
	Description string `json:"description,omitempty"`
	// Parents lists the parent folder ID; Drive allows at most one parent.
	Parents []string `json:"parents,omitempty"`
	// DriveID identifies the shared drive that owns the file; empty means My Drive.
	DriveID string `json:"driveId,omitempty"`
	// CreatedTime is the creation time reported by Drive.
	CreatedTime time.Time `json:"createdTime,omitzero"`
	// ModifiedTime is the last modification time reported by Drive.
	ModifiedTime time.Time `json:"modifiedTime,omitzero"`
	// SizeBytes is the stored content size; Google Workspace files and folders report zero.
	SizeBytes int64 `json:"sizeBytes,omitempty"`
	// MD5Checksum is the content checksum Drive reports for uploaded binary content.
	MD5Checksum string `json:"md5Checksum,omitempty"`
	// Version is Drive's monotonically increasing file version.
	Version int64 `json:"version,omitempty"`
	// WebViewLink opens the file in a browser for a user who can access it.
	WebViewLink string `json:"webViewLink,omitempty"`
	// IsTrashed reports whether the file is in the trash.
	IsTrashed bool `json:"isTrashed"`
	// Owners lists the owners Drive reports; shared-drive files have none.
	Owners []FileUser `json:"owners,omitempty"`
	// LastModifyingUser is the user who last modified the file, when Drive reports one.
	LastModifyingUser *FileUser `json:"lastModifyingUser,omitempty"`
	// Shortcut identifies the target of a Drive shortcut file; nil for other files.
	Shortcut *ShortcutTarget `json:"shortcut,omitempty"`
}

// FileUser is a Drive user identity attached to file metadata.
type FileUser struct {
	// DisplayName is the user's display name.
	DisplayName string `json:"displayName,omitempty"`
	// EmailAddress is the user's email address when Drive discloses it.
	EmailAddress string `json:"emailAddress,omitempty"`
}

// ShortcutTarget identifies the file a Drive shortcut points to.
type ShortcutTarget struct {
	// TargetFileID is the Drive ID of the shortcut's target file.
	TargetFileID string `json:"targetFileId"`
	// TargetMimeType is the Drive MIME type of the target file.
	TargetMimeType string `json:"targetMimeType,omitempty"`
}

type driveFileResource struct {
	ID                string             `json:"id"`
	AppProperties     map[string]string  `json:"appProperties"`
	Name              string             `json:"name"`
	MimeType          string             `json:"mimeType"`
	Description       string             `json:"description"`
	Parents           []string           `json:"parents"`
	DriveID           string             `json:"driveId"`
	CreatedTime       time.Time          `json:"createdTime"`
	ModifiedTime      time.Time          `json:"modifiedTime"`
	Size              string             `json:"size"`
	MD5Checksum       string             `json:"md5Checksum"`
	Version           string             `json:"version"`
	WebViewLink       string             `json:"webViewLink"`
	Trashed           bool               `json:"trashed"`
	Owners            []driveUser        `json:"owners"`
	LastModifyingUser *driveUser         `json:"lastModifyingUser"`
	ShortcutDetails   *driveShortcutInfo `json:"shortcutDetails"`
}

type driveUser struct {
	DisplayName  string `json:"displayName"`
	EmailAddress string `json:"emailAddress"`
}

type driveShortcutInfo struct {
	TargetID       string `json:"targetId"`
	TargetMimeType string `json:"targetMimeType"`
}

// driveRequest is one Drive HTTP request; body is resent once after a 401 forces a refresh.
type driveRequest struct {
	method        string
	target        string
	body          []byte
	contentType   string
	responseLimit int64
}

type driveResponse struct {
	status    int
	header    http.Header
	body      []byte
	requestID string
}

type driveRequestError struct {
	kind    sdkgo.FailureKind
	message string
}

// Error returns the safe human-readable failure message.
func (failure *driveRequestError) Error() string { return failure.message }

// readBranches names the branches one operation selects for a failed Drive read.
type readBranches struct {
	operationID      string
	notFound         sdkgo.BranchID
	tooLarge         sdkgo.BranchID
	providerRejected sdkgo.BranchID
	invalidResponse  sdkgo.BranchID
	defect           sdkgo.BranchID
}

// readOutcome classifies a failed Drive read; an empty branch means Retry.
type readOutcome struct {
	branch     sdkgo.BranchID
	failure    sdkgo.Failure
	retryAfter time.Duration
	receipt    sdkgo.Receipt
}

// New validates configuration and constructs an authenticated Google Drive client.
// It fails when the endpoint is not HTTPS (loopback HTTP is accepted for tests),
// a limit is outside its documented range, or credentials is nil.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	endpoint, err := providerhttp.ValidateBaseURL(config.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("Google Drive endpoint: %w", err)
	}
	if credentials == nil {
		return nil, errors.New("credential provider is required")
	}
	dependencies := clientOptions{now: time.Now}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Google Drive connector option is nil")
		}
		option(&dependencies)
	}
	if dependencies.httpClient == nil {
		dependencies.httpClient = &http.Client{Timeout: defaultRequestTimeout}
	}
	if config.MaxResponseBytes < 1 || config.MaxTextBytes < 1 {
		return nil, errors.New("Google Drive response and text limits must be positive")
	}
	if config.MaxUploadBytes < 1 || config.MaxUploadBytes > googleMultipartUploadLimitBytes {
		return nil, fmt.Errorf("Google Drive upload limit must be from 1 to %d bytes", googleMultipartUploadLimitBytes)
	}
	if config.SearchPageSize < 1 || config.SearchPageSize > maxSearchPageSize {
		return nil, fmt.Errorf("Google Drive search page size must be from 1 to %d", maxSearchPageSize)
	}
	if config.PollInterval < time.Second {
		return nil, errors.New("Google Drive poll interval must be at least one second")
	}
	if config.PollPageSize < 1 || config.PollPageSize > maxPollPageSize {
		return nil, fmt.Errorf("Google Drive poll page size must be from 1 to %d", maxPollPageSize)
	}
	return &Client{
		apiBaseURL: endpoint + driveAPIPath, uploadBaseURL: endpoint + driveUploadAPIPath,
		httpClient:       providerhttp.NewProviderHTTPClient(dependencies.httpClient, defaultRequestTimeout),
		credentials:      credentials,
		refreshDriver:    NewCredentialRefreshDriver(dependencies.httpClient),
		maxResponseBytes: config.MaxResponseBytes, maxTextBytes: config.MaxTextBytes,
		maxUploadBytes: config.MaxUploadBytes, searchPageSize: int(config.SearchPageSize),
		pollInterval: config.PollInterval, pollPageSize: int(config.PollPageSize), logger: dependencies.logger,
		now: dependencies.now,
	}, nil
}

// SearchFiles returns the SearchFiles operation bound to this client.
func (client *Client) SearchFiles() SearchFilesOperation { return SearchFilesOperation{client: client} }

// GetFile returns the GetFile operation bound to this client.
func (client *Client) GetFile() GetFileOperation { return GetFileOperation{client: client} }

// ReadFileText returns the ReadFileText operation bound to this client.
func (client *Client) ReadFileText() ReadFileTextOperation {
	return ReadFileTextOperation{client: client}
}

// UploadFile returns the UploadFile operation bound to this client.
func (client *Client) UploadFile() UploadFileOperation { return UploadFileOperation{client: client} }

// resolveCredential resolves the call's credential. isRetryable reports a credential outage that
// needs no reauthorization.
func (client *Client) resolveCredential(call sdkgo.Call, operationID string) (credential Credentials, failure *sdkgo.Failure, isRetryable bool) {
	credential, err := sdkgo.ResolveCredential(call.Context, client.credentials, call, client.refreshDriver)
	if err != nil && !isTerminalCredentialError(err) {
		return Credentials{}, driveFailurePointer(sdkgo.FailureAuthentication, operationID, "connection credentials are temporarily unavailable"), true
	}
	if err != nil || validateResolvedCredentials(credential) != nil {
		return Credentials{}, driveFailurePointer(sdkgo.FailureAuthentication, operationID, "connection credentials are unavailable"), false
	}
	return credential, nil, false
}

// resolveUploadCredential is resolveCredential for uploadFile.
func (client *Client) resolveUploadCredential(call sdkgo.Call) (credential Credentials, failure *sdkgo.Failure, isRetryable bool) {
	return client.resolveCredential(call, uploadFileOperationID)
}

// refreshRejectedUploadCredential reports whether the refresh succeeded, and whether a failure is transient.
func (client *Client) refreshRejectedUploadCredential(call sdkgo.Call) (isRefreshed bool, isTransient bool) {
	if _, ok := client.credentials.(sdkgo.RejectedCredentialRefreshingProvider[Credentials]); !ok {
		return false, false
	}
	refreshed, err := sdkgo.ResolveCredentialAfterRejection(call.Context, client.credentials, call, client.refreshDriver)
	if err != nil {
		return false, !isTerminalCredentialError(err)
	}
	return validateResolvedCredentials(refreshed) == nil, false
}

// isTerminalCredentialError reports a resolve failure that only reconnection repairs.
func isTerminalCredentialError(err error) bool {
	return errors.Is(err, sdkgo.ErrReauthorizationRequired) || errors.Is(err, projectconfig.ErrReauthorizationRequired) ||
		errors.Is(err, projectconfig.ErrObjectNotFound) || errors.Is(err, projectconfig.ErrConnectionNotSelected) ||
		errors.Is(err, projectconfig.ErrCredentialBrokerRejected)
}

func (client *Client) readBackUploadByID(call sdkgo.Call, credential *Credentials, fileID, idempotencyKey string) (*File, driveResponse, *readOutcome) {
	response, outcome := client.sendRead(call, credential, uploadFileLookupBranches, driveRequest{
		method: http.MethodGet, target: client.fileMetadataURL(fileID, fullFileFields+",appProperties"), responseLimit: client.maxResponseBytes,
	})
	if outcome != nil {
		return nil, response, outcome
	}
	var resource driveFileResource
	if err := json.Unmarshal(response.body, &resource); err != nil {
		return nil, response, &readOutcome{branch: UploadFileBranchInvalidResponse, failure: driveFailure(sdkgo.FailureProtocol, uploadFileOperationID, "provider returned an invalid upload ID read-back response")}
	}
	if resource.ID != fileID || resource.AppProperties[idempotencyAppPropertyKey] != idempotencyKey {
		return nil, response, nil
	}
	file, err := convertFileResource(resource)
	if err != nil {
		return nil, response, &readOutcome{branch: UploadFileBranchInvalidResponse, failure: driveFailure(sdkgo.FailureProtocol, uploadFileOperationID, "provider returned an invalid upload ID read-back response")}
	}
	return &file, response, nil
}

func (client *Client) lookupEarlierUpload(call sdkgo.Call, credential *Credentials, idempotencyKey string) (*File, driveResponse, *readOutcome, bool) {
	lookupQuery := url.Values{
		"q": {earlierUploadQuery(idempotencyKey)}, "fields": {"incompleteSearch,files(" + fullFileFields + ")"},
		"pageSize": {strconv.Itoa(idempotencyLookupPageSize)}, "spaces": {"drive"},
	}
	lookupResponse, outcome := client.sendRead(call, credential, uploadFileLookupBranches, driveRequest{
		method: http.MethodGet, target: client.filesListURL(lookupQuery), responseLimit: client.maxResponseBytes,
	})
	if outcome != nil {
		return nil, lookupResponse, outcome, false
	}
	earlierFile, isIncompleteLookup, err := decodeEarlierUpload(lookupResponse.body)
	if err != nil {
		return nil, lookupResponse, &readOutcome{branch: UploadFileBranchInvalidResponse, failure: driveFailure(sdkgo.FailureProtocol, uploadFileOperationID, "provider returned an invalid duplicate-lookup response"), receipt: client.receipt(call, lookupResponse.requestID, "")}, false
	}
	return earlierFile, lookupResponse, nil, isIncompleteLookup
}

// reconcileUpload reads back an upload with an unknown outcome, then resends while the cap allows.
func (client *Client) reconcileUpload(call sdkgo.Call, fence *singledispatch.Fence, credential *Credentials, idempotencyKey, requestID string, canResendUnknownOutcome bool) sdkgo.MutationAttempt[UploadFileOutput] {
	file, response, outcome, isIncomplete := client.lookupEarlierUpload(call, credential, idempotencyKey)
	if outcome != nil {
		if outcome.branch == "" {
			return sdkgo.NewMutationRetry[UploadFileOutput](outcome.failure, outcome.retryAfter)
		}
		return resendUploadOrSelectUncertain(call, client, canResendUnknownOutcome, "upload read-back could not confirm the outcome", requestID)
	}
	if file != nil {
		// A later attempt reads this file back by ID even when the property lookup lags.
		_ = fence.RecordProviderID(file.ID)
		return sdkgo.NewMutationBranch(UploadFileBranchUploaded, UploadFileOutput{File: *file}, nil, client.receipt(call, response.requestID, file.ID))
	}
	if isIncomplete {
		return sdkgo.NewMutationRetry[UploadFileOutput](driveFailure(sdkgo.FailureAvailability, uploadFileOperationID, "provider did not complete upload read-back"), 0)
	}
	return resendUploadOrSelectUncertain(call, client, canResendUnknownOutcome, "upload was sent but read-back found no matching file", requestID)
}

// reconcileAcceptedUpload confirms an upload Drive may have stored, from a 2xx without a usable file or a
// response carrying a file ID. It never resends: a saved file ID is read back on later attempts, and
// an upload this attempt cannot confirm without one selects uncertain.
func (client *Client) reconcileAcceptedUpload(call sdkgo.Call, fence *singledispatch.Fence, credential *Credentials, idempotencyKey, requestID, acceptedFileID string) sdkgo.MutationAttempt[UploadFileOutput] {
	if isDriveID(acceptedFileID) {
		isFileIDSaved := fence.RecordProviderID(acceptedFileID) == nil
		reconciled, isRetry := client.readBackSavedUpload(call, credential, acceptedFileID, idempotencyKey, false)
		if isRetry && !isFileIDSaved {
			// Without the saved ID the next attempt could resend, so the outcome stays uncertain.
			return sdkgo.NewMutationUncertain(UploadFileOutput{}, driveFailure(sdkgo.FailureAvailability, uploadFileOperationID,
				"provider accepted the upload, but its file could not be read back and its ID checkpoint failed"), client.receipt(call, requestID, acceptedFileID))
		}
		return reconciled
	}
	isAcceptanceSaved := fence.RecordProviderID(acceptedUploadWithoutFileID) == nil
	file, response, outcome, _ := client.lookupEarlierUpload(call, credential, idempotencyKey)
	if outcome == nil && file != nil {
		// A later attempt reads this file back by ID even when the property lookup lags.
		_ = fence.RecordProviderID(file.ID)
		return sdkgo.NewMutationBranch(UploadFileBranchUploaded, UploadFileOutput{File: *file}, nil, client.receipt(call, response.requestID, file.ID))
	}
	failure := driveFailure(sdkgo.FailureAvailability, uploadFileOperationID, "provider accepted the upload without a usable file, and read-back has not found it yet")
	if isAcceptanceSaved {
		// The saved acceptance makes later attempts look the file up without sending again.
		return sdkgo.NewMutationRetry[UploadFileOutput](failure, 0)
	}
	failure.Message += "; its acceptance checkpoint failed, so it is not sent again"
	return sdkgo.NewMutationUncertain(UploadFileOutput{}, failure, client.receipt(call, requestID, ""))
}

// reconcileSavedAcceptance looks up an upload an earlier attempt saw Drive accept without a usable file.
// It never resends; an upload still missing selects uncertain once the attempts reach the send cap.
func (client *Client) reconcileSavedAcceptance(call sdkgo.Call, fence *singledispatch.Fence, credential *Credentials, idempotencyKey string, canLookUpAgain bool) sdkgo.MutationAttempt[UploadFileOutput] {
	file, response, outcome, isIncomplete := client.lookupEarlierUpload(call, credential, idempotencyKey)
	if outcome == nil && file != nil {
		_ = fence.RecordProviderID(file.ID)
		return sdkgo.NewMutationBranch(UploadFileBranchUploaded, UploadFileOutput{File: *file, IsFromEarlierAttempt: true}, nil, client.receipt(call, response.requestID, file.ID))
	}
	if outcome != nil && outcome.branch == "" {
		return sdkgo.NewMutationRetry[UploadFileOutput](outcome.failure, outcome.retryAfter)
	}
	failure := driveFailure(sdkgo.FailureAvailability, uploadFileOperationID, "an earlier attempt's upload was accepted, and read-back has not found it yet")
	if isIncomplete || canLookUpAgain {
		return sdkgo.NewMutationRetry[UploadFileOutput](failure, 0)
	}
	failure.Message += "; it is not sent again"
	return sdkgo.NewMutationUncertain(UploadFileOutput{}, failure, client.receipt(call, "", ""))
}

// readBackSavedUpload reads an accepted upload by its file ID. isRetry reports a transient read failure.
func (client *Client) readBackSavedUpload(call sdkgo.Call, credential *Credentials, fileID, idempotencyKey string, isFromEarlierAttempt bool) (reconciled sdkgo.MutationAttempt[UploadFileOutput], isRetry bool) {
	file, response, outcome := client.readBackUploadByID(call, credential, fileID, idempotencyKey)
	if outcome != nil {
		if outcome.branch == "" {
			return sdkgo.NewMutationRetry[UploadFileOutput](outcome.failure, outcome.retryAfter), true
		}
		return uploadUncertain(call, client, "upload ID read-back could not confirm the outcome"), false
	}
	if file == nil {
		return uploadUncertain(call, client, "upload ID read-back found no matching file"), false
	}
	return sdkgo.NewMutationBranch(UploadFileBranchUploaded, UploadFileOutput{File: *file, IsFromEarlierAttempt: isFromEarlierAttempt}, nil, client.receipt(call, response.requestID, file.ID)), false
}

// resendUploadOrSelectUncertain retries a possibly accepted upload while the send cap allows another send.
func resendUploadOrSelectUncertain(call sdkgo.Call, client *Client, canResendUnknownOutcome bool, message, requestID string) sdkgo.MutationAttempt[UploadFileOutput] {
	if canResendUnknownOutcome {
		// The claim stays the last marker, so the next attempt reads back again and counts this send.
		return sdkgo.NewMutationRetry[UploadFileOutput](driveFailure(sdkgo.FailureAvailability, uploadFileOperationID, message), 0)
	}
	return sdkgo.NewMutationUncertain(UploadFileOutput{}, driveFailure(sdkgo.FailureAvailability, uploadFileOperationID, message+"; the send cap is reached"), client.receipt(call, requestID, ""))
}

// sendRead performs one idempotent Drive read and classifies every non-2xx or unreadable response.
func (client *Client) sendRead(
	call sdkgo.Call,
	credential *Credentials,
	branches readBranches,
	request driveRequest,
) (driveResponse, *readOutcome) {
	response, err := client.sendRequest(call, credential, request)
	receipt := client.receipt(call, response.requestID, "")
	var requestErr *driveRequestError
	if errors.As(err, &requestErr) {
		switch requestErr.kind {
		case sdkgo.FailureLocalDefect:
			return response, &readOutcome{branch: branches.defect, failure: driveFailure(requestErr.kind, branches.operationID, requestErr.message)}
		case sdkgo.FailureResponseTooLarge:
			return response, &readOutcome{branch: branches.tooLarge, failure: driveFailure(requestErr.kind, branches.operationID, requestErr.message), receipt: receipt}
		}
	}
	if err != nil {
		return response, &readOutcome{failure: driveFailure(sdkgo.FailureTransport, branches.operationID, "provider is unavailable")}
	}
	if response.status >= 200 && response.status < 300 {
		return response, nil
	}
	reasons := providerhttp.ReadErrorTokens(response.body, googleErrorTokenPointers)
	if isRetryableStatus(response.status, reasons) {
		delay := providerhttp.ParseRetryAfter(response.header.Get("Retry-After"), client.now())
		return response, &readOutcome{failure: driveFailure(statusFailureKind(response.status, reasons), branches.operationID, "provider temporarily rejected the request"), retryAfter: delay}
	}
	if response.status == http.StatusNotFound {
		return response, &readOutcome{branch: branches.notFound, failure: driveFailure(sdkgo.FailureNotFound, branches.operationID, "file was not found or is not visible to the connection"), receipt: receipt}
	}
	if hasReason(reasons, "exportSizeLimitExceeded") {
		return response, &readOutcome{branch: branches.tooLarge, failure: driveFailure(sdkgo.FailureResponseTooLarge, branches.operationID, "Google export size limit was exceeded"), receipt: receipt}
	}
	return response, &readOutcome{branch: branches.providerRejected, failure: driveFailure(statusFailureKind(response.status, reasons), branches.operationID, "provider rejected the request"), receipt: receipt}
}

// sendRequest sends request with the resolved credential. After one 401 it
// forces a single coordinated refresh, updates credential, and sends once more.
func (client *Client) sendRequest(call sdkgo.Call, credential *Credentials, request driveRequest) (driveResponse, error) {
	return client.sendRequestWithContext(call.Context, call, credential, request)
}

// sendRequestWithContext is sendRequest for a caller outside a Dex Step, such as a Trigger poller,
// whose Call carries no Step context.
func (client *Client) sendRequestWithContext(ctx context.Context, call sdkgo.Call, credential *Credentials, request driveRequest) (driveResponse, error) {
	requestContext := ctx
	if stepDeadline, hasDeadline := ctx.Deadline(); hasDeadline {
		var cancel context.CancelFunc
		margin := min(readDeadlineMargin, max(0, time.Until(stepDeadline)/6))
		requestContext, cancel = context.WithDeadline(ctx, stepDeadline.Add(-margin))
		defer cancel()
	}
	for attempt := 0; attempt < 2; attempt++ {
		var body io.Reader
		if request.body != nil {
			body = bytes.NewReader(request.body)
		}
		httpRequest, err := http.NewRequestWithContext(requestContext, request.method, request.target, body)
		if err != nil {
			return driveResponse{}, &driveRequestError{kind: sdkgo.FailureLocalDefect, message: "provider request could not be built"}
		}
		httpRequest.Header.Set("Authorization", "Bearer "+credential.AccessToken.Reveal())
		if request.contentType != "" {
			httpRequest.Header.Set("Content-Type", request.contentType)
		}
		httpResponse, err := client.httpClient.Do(httpRequest)
		if err != nil {
			return driveResponse{}, &driveRequestError{kind: sdkgo.FailureTransport, message: "provider request failed"}
		}
		// Error bodies get their own bound, so a small success limit never hides an error status.
		content, readErr := providerhttp.ReadBoundedBody(httpResponse.Body, max(request.responseLimit, providerhttp.MaxErrorBodyBytes))
		closeErr := httpResponse.Body.Close()
		response := driveResponse{status: httpResponse.StatusCode, header: httpResponse.Header, body: content, requestID: googleRequestID(httpResponse.Header)}
		isSuccess := response.status >= 200 && response.status < 300
		isOversized := errors.Is(readErr, providerhttp.ErrBodyTooLarge) || (isSuccess && int64(len(content)) > request.responseLimit)
		if isOversized && isSuccess {
			response.body = nil
			return response, &driveRequestError{kind: sdkgo.FailureResponseTooLarge, message: "provider response exceeds the configured limit"}
		}
		if isOversized {
			// An oversized error body carries no usable reason, so the status alone classifies it.
			response.body = nil
			readErr = nil
		}
		if readErr != nil || closeErr != nil {
			return response, &driveRequestError{kind: sdkgo.FailureTransport, message: "provider response could not be read"}
		}
		if response.status != http.StatusUnauthorized || attempt != 0 {
			return response, nil
		}
		if _, ok := client.credentials.(sdkgo.RejectedCredentialRefreshingProvider[Credentials]); !ok {
			return response, nil
		}
		refreshed, err := sdkgo.ResolveCredentialAfterRejection(requestContext, client.credentials, call, client.refreshDriver)
		if err != nil || validateResolvedCredentials(refreshed) != nil {
			return response, nil
		}
		*credential = refreshed
	}
	return driveResponse{}, &driveRequestError{kind: sdkgo.FailureLocalDefect, message: "authenticated request retry was exhausted"}
}

func (client *Client) receipt(call sdkgo.Call, requestID string, objectID string) sdkgo.Receipt {
	return sdkgo.Receipt{
		CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: driveProviderName,
		ProviderObjectID: objectID, ProviderRequestID: requestID, ObservedAt: client.now().UTC(),
	}
}

// filesListURL searches My Drive, shared-with-me files, and every shared drive the account can see.
func (client *Client) filesListURL(query url.Values) string {
	query.Set("corpora", "allDrives")
	query.Set("supportsAllDrives", "true")
	query.Set("includeItemsFromAllDrives", "true")
	return client.apiBaseURL + "/files?" + query.Encode()
}

func (client *Client) fileMetadataURL(fileID string, fields string) string {
	query := url.Values{"fields": {fields}, "supportsAllDrives": {"true"}}
	return client.apiBaseURL + "/files/" + url.PathEscape(fileID) + "?" + query.Encode()
}

func (client *Client) fileContentURL(fileID string) string {
	query := url.Values{"alt": {"media"}, "supportsAllDrives": {"true"}}
	return client.apiBaseURL + "/files/" + url.PathEscape(fileID) + "?" + query.Encode()
}

func (client *Client) fileExportURL(fileID string, exportMimeType string) string {
	query := url.Values{"mimeType": {exportMimeType}}
	return client.apiBaseURL + "/files/" + url.PathEscape(fileID) + "/export?" + query.Encode()
}

func (client *Client) multipartUploadURL() string {
	query := url.Values{"uploadType": {"multipart"}, "fields": {fullFileFields}, "supportsAllDrives": {"true"}}
	return client.uploadBaseURL + "/files?" + query.Encode()
}

func validateResolvedCredentials(credentials Credentials) error {
	if !providerhttp.IsHeaderSafeCredential(credentials.AccessToken.Reveal()) {
		return errors.New("Google Drive access token is missing or invalid")
	}
	switch credentials.AuthMethodID {
	case "", GoogleOAuthAuthMethodID, WorkspaceDomainDelegationAuthMethodID:
		return nil
	default:
		return errors.New("Google Drive authorization method is invalid")
	}
}

// convertFileResource validates one untrusted Drive file resource.
func convertFileResource(resource driveFileResource) (File, error) {
	if !isDriveID(resource.ID) || resource.Name == "" || resource.MimeType == "" {
		return File{}, errors.New("file resource lacks a valid ID, name, or MIME type")
	}
	file := File{
		ID: resource.ID, Name: resource.Name, MimeType: resource.MimeType, Description: resource.Description,
		Parents: resource.Parents, DriveID: resource.DriveID, CreatedTime: resource.CreatedTime,
		ModifiedTime: resource.ModifiedTime, MD5Checksum: resource.MD5Checksum, WebViewLink: resource.WebViewLink,
		IsTrashed: resource.Trashed,
	}
	var err error
	if file.SizeBytes, err = parseOptionalInt64(resource.Size); err != nil {
		return File{}, errors.New("file size is invalid")
	}
	if file.Version, err = parseOptionalInt64(resource.Version); err != nil {
		return File{}, errors.New("file version is invalid")
	}
	for _, owner := range resource.Owners {
		file.Owners = append(file.Owners, FileUser(owner))
	}
	if resource.LastModifyingUser != nil {
		lastModifyingUser := FileUser(*resource.LastModifyingUser)
		file.LastModifyingUser = &lastModifyingUser
	}
	if resource.ShortcutDetails != nil {
		file.Shortcut = &ShortcutTarget{TargetFileID: resource.ShortcutDetails.TargetID, TargetMimeType: resource.ShortcutDetails.TargetMimeType}
	}
	return file, nil
}

func parseOptionalInt64(value string) (int64, error) {
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 0 {
		return 0, errors.New("value is not a non-negative integer")
	}
	return parsed, nil
}

// queryAttemptFromReadOutcome converts a failed read into a Query Retry or branch.
func queryAttemptFromReadOutcome[T any](outcome *readOutcome) sdkgo.QueryAttempt[T] {
	if outcome.branch == "" {
		return sdkgo.NewQueryRetry[T](outcome.failure, outcome.retryAfter)
	}
	var zero T
	failure := outcome.failure
	return sdkgo.NewQueryBranch(outcome.branch, zero, &failure, outcome.receipt)
}

// mutationAttemptFromReadOutcome converts a failed pre-write read into a Mutation Retry or branch.
func mutationAttemptFromReadOutcome[T any](outcome *readOutcome) sdkgo.MutationAttempt[T] {
	if outcome.branch == "" {
		return sdkgo.NewMutationRetry[T](outcome.failure, outcome.retryAfter)
	}
	var zero T
	failure := outcome.failure
	return sdkgo.NewMutationBranch(outcome.branch, zero, &failure, outcome.receipt)
}

// quoteDriveQueryString quotes value as a Drive query string literal.
func quoteDriveQueryString(value string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(value) + "'"
}

func isDriveID(value string) bool { return driveIDPattern.MatchString(value) }

// isPlainMediaType reports whether value is exactly one lowercase type/subtype without parameters.
func isPlainMediaType(value string) bool {
	mediaType, parameters, err := mime.ParseMediaType(value)
	return err == nil && len(parameters) == 0 && mediaType == value && strings.Count(value, "/") == 1 &&
		!strings.HasPrefix(value, "/") && !strings.HasSuffix(value, "/")
}

func isRetryableStatus(status int, reasons []string) bool {
	return status >= 500 || isRateLimitStatus(status, reasons)
}

// isRateLimitStatus reports a 429 or a 403 whose first Google reason is a rate limit.
func isRateLimitStatus(status int, reasons []string) bool {
	return status == http.StatusTooManyRequests ||
		(status == http.StatusForbidden && len(reasons) > 0 && googleRateLimitReasons[reasons[0]])
}

func hasReason(reasons []string, reason string) bool {
	return len(reasons) > 0 && reasons[0] == reason
}

func statusFailureKind(status int, reasons []string) sdkgo.FailureKind {
	switch {
	case status == http.StatusUnauthorized:
		return sdkgo.FailureAuthentication
	case isRateLimitStatus(status, reasons):
		return sdkgo.FailureRateLimit
	case status == http.StatusForbidden:
		return sdkgo.FailureAuthorization
	case status == http.StatusNotFound:
		return sdkgo.FailureNotFound
	case status == http.StatusConflict:
		return sdkgo.FailureConflict
	case status >= 500:
		return sdkgo.FailureAvailability
	default:
		return sdkgo.FailureProviderRejection
	}
}

func driveFailure(kind sdkgo.FailureKind, operationID, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: driveProviderName, Operation: operationID, Message: message}
}

func driveFailurePointer(kind sdkgo.FailureKind, operationID, message string) *sdkgo.Failure {
	failure := driveFailure(kind, operationID, message)
	return &failure
}

func googleRequestID(header http.Header) string {
	if value := header.Get("X-Goog-Request-Id"); value != "" {
		return value
	}
	return header.Get("X-Request-Id")
}
