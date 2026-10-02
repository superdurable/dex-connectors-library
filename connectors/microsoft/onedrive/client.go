// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package onedrive implements bounded Microsoft Graph file operations for
// OneDrive and SharePoint document libraries: name-based search, item
// metadata reads, bounded text reads, and uploads and folder creation that
// converge on one item when a Step is dispatched twice.
//
// Applications use the generated operation-specific Step factories, such as
// NewSearchFilesStep and NewUploadFileStep, with a Connection built by
// NewLocalConnection or NewConnection. The runnable example in
// examples/text-copy shows every operation in one Flow.
package onedrive

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	graphProviderName     = "microsoft-graph"
	graphAPIVersionPath   = "/v1.0"
	defaultRequestTimeout = 25 * time.Second
	// maxSearchPageSize bounds every searchFiles page, so one Result stays small.
	maxSearchPageSize = 200
	// maxUploadLimitBytes bounds content that travels through Flow state.
	maxUploadLimitBytes = 5 << 20
	// maxItemNameBytes bounds a name before Graph applies its own path limits.
	maxItemNameBytes = 1024
	rootFolderAlias  = "root"
	// conflictBehaviorParameter is Graph's instance annotation for create conflicts.
	conflictBehaviorParameter = "@microsoft.graph.conflictBehavior"
	// itemFields selects every DriveItem field and never @microsoft.graph.downloadUrl.
	itemFields = "id,name,size,webUrl,eTag,cTag,createdDateTime,lastModifiedDateTime,parentReference,file,folder,package,createdBy,lastModifiedBy"
)

// graphIDPattern accepts OneDrive and SharePoint drive and item IDs, such as b!Abc_1-2 or 01BYE5RZ.
var graphIDPattern = regexp.MustCompile(`^[A-Za-z0-9!_.-]{1,256}$`)

// graphErrorTokenPointers locate Graph's machine-readable error codes, outermost first.
var graphErrorTokenPointers = []string{"/error/code", "/error/innerError/code", "/error/innererror/code"}

// graphRetryableErrorCodes are Graph codes that document a throttled or temporarily unavailable service.
var graphRetryableErrorCodes = map[string]bool{
	"activityLimitReached": true, "throttledRequest": true, "TooManyRequests": true, "serviceNotAvailable": true,
}

// reservedItemNameCharacters are the characters Microsoft documents as invalid in OneDrive
// for work or school and SharePoint names, plus the double quote Windows rejects.
const reservedItemNameCharacters = `/\*<>?:|#%"`

// Option configures Client construction.
type Option func(*clientOptions)

type clientOptions struct {
	httpClient *http.Client
}

// WithHTTPClient overrides the default 25-second HTTP client used for Graph,
// content download, and token requests. The connector uses a copy that never
// follows redirects; the caller retains ownership of the original client and
// its transport.
func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

// Client executes authenticated Microsoft Graph requests for connector
// operations. A Client is immutable after New and safe for concurrent use by
// Dex Workers.
type Client struct {
	apiBaseURL       string
	endpointOrigin   *url.URL
	httpClient       *http.Client
	credentials      sdkgo.CredentialProvider[Credentials]
	refreshDriver    sdkgo.CredentialRefreshDriver[Credentials]
	maxResponseBytes int64
	maxTextBytes     int64
	maxUploadBytes   int64
	searchPageSize   int
	now              func() time.Time
}

// DriveItem is the metadata of one OneDrive or SharePoint file or folder. It
// never contains a pre-authenticated download URL.
type DriveItem struct {
	// ID is the stable drive item ID; renaming or moving the item keeps it.
	ID string `json:"id"`
	// Name is the file or folder name, including any extension.
	Name string `json:"name"`
	// DriveID is the ID of the drive that holds the item.
	DriveID string `json:"driveId,omitempty"`
	// DriveType is personal, business, or documentLibrary.
	DriveType string `json:"driveType,omitempty"`
	// SiteID is the SharePoint site ID for an item in a site's document library.
	SiteID string `json:"siteId,omitempty"`
	// ParentFolderID is the item ID of the parent folder; empty for a drive root.
	ParentFolderID string `json:"parentFolderId,omitempty"`
	// ParentPath is Graph's percent-encoded path of the parent folder, such as /drives/b!Abc/root:/Reports.
	ParentPath string `json:"parentPath,omitempty"`
	// IsFolder reports a folder, which can hold children.
	IsFolder bool `json:"isFolder"`
	// IsPackage reports a package, such as a OneNote notebook, which is neither a plain file nor a folder.
	IsPackage bool `json:"isPackage,omitempty"`
	// ChildCount is the number of direct children of a folder.
	ChildCount int64 `json:"childCount,omitempty"`
	// MimeType is the file's media type as Graph reports it; empty for folders.
	MimeType string `json:"mimeType,omitempty"`
	// SizeBytes is the file size, or the total size of a folder's contents.
	SizeBytes int64 `json:"sizeBytes"`
	// QuickXorHash is Microsoft's base64 content hash of a file, when Graph reports one.
	QuickXorHash string `json:"quickXorHash,omitempty"`
	// ETag changes whenever the item's metadata or content changes.
	ETag string `json:"eTag,omitempty"`
	// CTag changes whenever the file's content changes; Graph omits it for SharePoint folders.
	CTag string `json:"cTag,omitempty"`
	// CreatedDateTime is when the item was created.
	CreatedDateTime time.Time `json:"createdDateTime,omitzero"`
	// LastModifiedDateTime is when the item was last modified.
	LastModifiedDateTime time.Time `json:"lastModifiedDateTime,omitzero"`
	// WebURL opens the item in a browser for a user who can access it; it is not pre-authenticated.
	WebURL string `json:"webUrl,omitempty"`
	// CreatedBy identifies the user or application that created the item.
	CreatedBy *ItemIdentity `json:"createdBy,omitempty"`
	// LastModifiedBy identifies the user or application that last modified the item.
	LastModifiedBy *ItemIdentity `json:"lastModifiedBy,omitempty"`
}

// ItemIdentity is the user or application Graph reports for a create or modify action.
type ItemIdentity struct {
	// DisplayName is the user's or application's display name.
	DisplayName string `json:"displayName,omitempty"`
	// Email is the user's email address when Graph discloses it.
	Email string `json:"email,omitempty"`
	// IsApplication reports that an application, not a user, performed the action.
	IsApplication bool `json:"isApplication,omitempty"`
}

// graphItemResource decodes only modeled fields, so a returned download URL is never kept.
type graphItemResource struct {
	ID                   string              `json:"id"`
	Name                 string              `json:"name"`
	Size                 *int64              `json:"size"`
	WebURL               string              `json:"webUrl"`
	ETag                 string              `json:"eTag"`
	CTag                 string              `json:"cTag"`
	CreatedDateTime      time.Time           `json:"createdDateTime"`
	LastModifiedDateTime time.Time           `json:"lastModifiedDateTime"`
	ParentReference      *graphItemReference `json:"parentReference"`
	File                 *graphFileFacet     `json:"file"`
	Folder               *graphFolderFacet   `json:"folder"`
	Package              *struct{}           `json:"package"`
	Root                 *struct{}           `json:"root"`
	CreatedBy            *graphIdentitySet   `json:"createdBy"`
	LastModifiedBy       *graphIdentitySet   `json:"lastModifiedBy"`
}

type graphItemReference struct {
	DriveID   string `json:"driveId"`
	DriveType string `json:"driveType"`
	ID        string `json:"id"`
	Path      string `json:"path"`
	SiteID    string `json:"siteId"`
}

type graphFileFacet struct {
	MimeType string `json:"mimeType"`
	Hashes   *struct {
		QuickXorHash string `json:"quickXorHash"`
	} `json:"hashes"`
}

type graphFolderFacet struct {
	ChildCount int64 `json:"childCount"`
}

type graphIdentitySet struct {
	User        *graphIdentity `json:"user"`
	Application *graphIdentity `json:"application"`
}

type graphIdentity struct {
	DisplayName string `json:"displayName"`
	Email       string `json:"email"`
}

// graphCollectionResponse is one page of a Graph item collection.
type graphCollectionResponse struct {
	Value    []graphItemResource `json:"value"`
	NextLink string              `json:"@odata.nextLink"`
}

// graphRequest is one Graph HTTP request; body is resent once after a 401 forces a refresh.
type graphRequest struct {
	method        string
	target        string
	body          []byte
	contentType   string
	responseLimit int64
	// isRawContent omits Accept: application/json from a file content request.
	isRawContent bool
}

type graphResponse struct {
	status    int
	header    http.Header
	body      []byte
	requestID string
}

type graphRequestError struct {
	kind    sdkgo.FailureKind
	message string
}

// Error returns the safe human-readable failure message.
func (failure *graphRequestError) Error() string { return failure.message }

// readBranches names the branches one operation selects for a failed Graph read.
type readBranches struct {
	operationID      string
	notFound         sdkgo.BranchID
	providerRejected sdkgo.BranchID
	invalidResponse  sdkgo.BranchID
	defect           sdkgo.BranchID
}

// readOutcome classifies a failed Graph read; an empty branch means Retry.
type readOutcome struct {
	branch     sdkgo.BranchID
	failure    sdkgo.Failure
	retryAfter time.Duration
	receipt    sdkgo.Receipt
}

// New validates configuration and constructs an authenticated Microsoft Graph
// client. It fails when the endpoint is not HTTPS (loopback HTTP is accepted
// for tests), a limit is outside its documented range, or credentials is nil.
func New(config Config, credentials sdkgo.CredentialProvider[Credentials], options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	endpoint, err := providerhttp.ValidateBaseURL(config.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("Microsoft Graph endpoint: %w", err)
	}
	endpointOrigin, err := url.Parse(endpoint)
	if err != nil {
		return nil, errors.New("Microsoft Graph endpoint is invalid")
	}
	if credentials == nil {
		return nil, errors.New("credential provider is required")
	}
	dependencies := clientOptions{}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Microsoft OneDrive connector option is nil")
		}
		option(&dependencies)
	}
	if dependencies.httpClient == nil {
		dependencies.httpClient = &http.Client{Timeout: defaultRequestTimeout}
	}
	if config.MaxResponseBytes < 1 || config.MaxTextBytes < 1 {
		return nil, errors.New("Microsoft OneDrive response and text limits must be positive")
	}
	if config.MaxUploadBytes < 1 || config.MaxUploadBytes > maxUploadLimitBytes {
		return nil, fmt.Errorf("Microsoft OneDrive upload limit must be from 1 to %d bytes", maxUploadLimitBytes)
	}
	if config.SearchPageSize < 1 || config.SearchPageSize > maxSearchPageSize {
		return nil, fmt.Errorf("Microsoft OneDrive search page size must be from 1 to %d", maxSearchPageSize)
	}
	return &Client{
		apiBaseURL: endpoint + graphAPIVersionPath, endpointOrigin: endpointOrigin,
		httpClient:       providerhttp.NewProviderHTTPClient(dependencies.httpClient, defaultRequestTimeout),
		credentials:      credentials,
		refreshDriver:    NewCredentialRefreshDriver(dependencies.httpClient),
		maxResponseBytes: config.MaxResponseBytes, maxTextBytes: config.MaxTextBytes,
		maxUploadBytes: config.MaxUploadBytes, searchPageSize: int(config.SearchPageSize),
		now: time.Now,
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

// CreateFolder returns the CreateFolder operation bound to this client.
func (client *Client) CreateFolder() CreateFolderOperation {
	return CreateFolderOperation{client: client}
}

// resolveCredential refreshes the credential and rejects a blank drive ID for app-only connections.
func (client *Client) resolveCredential(call sdkgo.Call, operationID string, driveID string) (Credentials, *sdkgo.Failure) {
	credential, err := sdkgo.ResolveCredential(call.Context, client.credentials, call, client.refreshDriver)
	if err != nil || validateResolvedCredentials(credential) != nil {
		return Credentials{}, graphFailurePointer(sdkgo.FailureAuthentication, operationID, "connection credentials are unavailable")
	}
	if driveID == "" && credential.AuthMethodID == MicrosoftAppOnlyAuthMethodID {
		return Credentials{}, graphFailurePointer(sdkgo.FailureValidation, operationID, "driveId is required with app-only authorization, which has no signed-in user")
	}
	return credential, nil
}

// sendRead performs one idempotent Graph read and classifies every non-2xx or unreadable response.
func (client *Client) sendRead(
	call sdkgo.Call,
	credential *Credentials,
	branches readBranches,
	request graphRequest,
) (graphResponse, *readOutcome) {
	response, err := client.sendRequest(call, credential, request)
	receipt := client.receipt(call, response.requestID, "")
	var requestErr *graphRequestError
	if errors.As(err, &requestErr) {
		switch requestErr.kind {
		case sdkgo.FailureLocalDefect:
			return response, &readOutcome{branch: branches.defect, failure: graphFailure(requestErr.kind, branches.operationID, requestErr.message)}
		case sdkgo.FailureResponseTooLarge:
			return response, &readOutcome{branch: branches.invalidResponse, failure: graphFailure(requestErr.kind, branches.operationID, requestErr.message), receipt: receipt}
		}
	}
	if err != nil {
		return response, &readOutcome{failure: graphFailure(sdkgo.FailureTransport, branches.operationID, "provider is unavailable")}
	}
	if response.status >= 200 && response.status < 300 {
		return response, nil
	}
	return response, client.classifyFailedRead(response, branches, receipt)
}

func (client *Client) classifyFailedRead(response graphResponse, branches readBranches, receipt sdkgo.Receipt) *readOutcome {
	codes := providerhttp.ReadErrorTokens(response.body, graphErrorTokenPointers)
	if isRetryableStatus(response.status, codes) {
		delay := providerhttp.ParseRetryAfter(response.header.Get("Retry-After"), client.now())
		return &readOutcome{failure: graphFailure(statusFailureKind(response.status, codes), branches.operationID, "provider temporarily rejected the request"), retryAfter: delay}
	}
	if response.status == http.StatusNotFound {
		return &readOutcome{branch: branches.notFound, failure: graphFailure(sdkgo.FailureNotFound, branches.operationID, "item was not found or is not visible to the connection"), receipt: receipt}
	}
	return &readOutcome{branch: branches.providerRejected, failure: graphFailure(statusFailureKind(response.status, codes), branches.operationID, "provider rejected the request"), receipt: receipt}
}

// sendRequest sends request with the resolved credential. After one 401 it
// forces a single coordinated refresh, updates credential, and sends once more.
func (client *Client) sendRequest(call sdkgo.Call, credential *Credentials, request graphRequest) (graphResponse, error) {
	for attempt := 0; attempt < 2; attempt++ {
		var body io.Reader
		if request.body != nil {
			body = bytes.NewReader(request.body)
		}
		httpRequest, err := http.NewRequestWithContext(call.Context, request.method, request.target, body)
		if err != nil {
			return graphResponse{}, &graphRequestError{kind: sdkgo.FailureLocalDefect, message: "provider request could not be built"}
		}
		httpRequest.Header.Set("Authorization", "Bearer "+credential.AccessToken.Reveal())
		if !request.isRawContent {
			httpRequest.Header.Set("Accept", "application/json")
		}
		if request.contentType != "" {
			httpRequest.Header.Set("Content-Type", request.contentType)
		}
		httpResponse, err := client.httpClient.Do(httpRequest)
		if err != nil {
			return graphResponse{}, &graphRequestError{kind: sdkgo.FailureTransport, message: "provider request failed"}
		}
		response, err := client.readResponse(httpResponse, request.responseLimit)
		if err != nil {
			return response, err
		}
		if response.status != http.StatusUnauthorized || attempt != 0 {
			return response, nil
		}
		if _, ok := client.credentials.(sdkgo.RejectedCredentialRefreshingProvider[Credentials]); !ok {
			return response, nil
		}
		refreshed, err := sdkgo.ResolveCredentialAfterRejection(call.Context, client.credentials, call, client.refreshDriver)
		if err != nil || validateResolvedCredentials(refreshed) != nil {
			return response, nil
		}
		*credential = refreshed
	}
	return graphResponse{}, &graphRequestError{kind: sdkgo.FailureLocalDefect, message: "authenticated request retry was exhausted"}
}

// readResponse reads one bounded response. Error bodies get their own bound,
// so a small success limit never hides an error status.
func (client *Client) readResponse(httpResponse *http.Response, responseLimit int64) (graphResponse, error) {
	content, readErr := providerhttp.ReadBoundedBody(httpResponse.Body, max(responseLimit, providerhttp.MaxErrorBodyBytes))
	closeErr := httpResponse.Body.Close()
	response := graphResponse{status: httpResponse.StatusCode, header: httpResponse.Header, body: content, requestID: httpResponse.Header.Get("request-id")}
	isSuccess := response.status >= 200 && response.status < 300
	isOversized := errors.Is(readErr, providerhttp.ErrBodyTooLarge) || (isSuccess && int64(len(content)) > responseLimit)
	if isOversized && isSuccess {
		response.body = nil
		return response, &graphRequestError{kind: sdkgo.FailureResponseTooLarge, message: "provider response exceeds the configured limit"}
	}
	if isOversized {
		// An oversized error body carries no usable code, so the status alone classifies it.
		response.body = nil
		readErr = nil
	}
	if readErr != nil || closeErr != nil {
		return response, &graphRequestError{kind: sdkgo.FailureTransport, message: "provider response could not be read"}
	}
	return response, nil
}

func (client *Client) receipt(call sdkgo.Call, requestID string, objectID string) sdkgo.Receipt {
	return sdkgo.Receipt{
		CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: graphProviderName,
		ProviderObjectID: objectID, ProviderRequestID: requestID, ObservedAt: client.now().UTC(),
	}
}

// driveURL addresses the drive by ID, or the signed-in user's OneDrive when driveID is blank.
func (client *Client) driveURL(driveID string) string {
	if driveID == "" {
		return client.apiBaseURL + "/me/drive"
	}
	return client.apiBaseURL + "/drives/" + url.PathEscape(driveID)
}

// itemURL addresses one item by ID, or the drive root for the root alias.
func (client *Client) itemURL(driveID string, itemID string) string {
	if itemID == "" || itemID == rootFolderAlias {
		return client.driveURL(driveID) + "/root"
	}
	return client.driveURL(driveID) + "/items/" + url.PathEscape(itemID)
}

// childPathURL addresses the child named name of a parent folder by path, without a trailing colon.
func (client *Client) childPathURL(driveID string, parentFolderID string, name string) string {
	return client.itemURL(driveID, parentFolderID) + ":/" + url.PathEscape(name)
}

// isGraphNextLink accepts an @odata.nextLink only on the configured Graph origin and API version.
func (client *Client) isGraphNextLink(link string) bool {
	parsed, err := url.Parse(link)
	return err == nil && parsed.Scheme == client.endpointOrigin.Scheme && parsed.Host == client.endpointOrigin.Host &&
		parsed.User == nil && parsed.Fragment == "" && strings.HasPrefix(parsed.Path, client.endpointOrigin.Path+graphAPIVersionPath+"/")
}

// buildODataQuery encodes fixed OData parameter names literally, so $select stays readable to Graph.
func buildODataQuery(parameters ...[2]string) string {
	var query strings.Builder
	for index, parameter := range parameters {
		if index > 0 {
			query.WriteByte('&')
		}
		query.WriteString(parameter[0])
		query.WriteByte('=')
		query.WriteString(url.QueryEscape(parameter[1]))
	}
	return query.String()
}

func validateResolvedCredentials(credentials Credentials) error {
	if !providerhttp.IsHeaderSafeCredential(credentials.AccessToken.Reveal()) {
		return errors.New("Microsoft Graph access token is missing or invalid")
	}
	switch credentials.AuthMethodID {
	case "", MicrosoftOAuthAuthMethodID, MicrosoftAppOnlyAuthMethodID:
		return nil
	default:
		return errors.New("Microsoft OneDrive authorization method is invalid")
	}
}

// convertItemResource validates one untrusted Graph driveItem resource.
func convertItemResource(resource graphItemResource) (DriveItem, error) {
	if !isGraphID(resource.ID) || resource.Name == "" || !utf8.ValidString(resource.Name) {
		return DriveItem{}, errors.New("item resource lacks a valid ID or name")
	}
	if resource.Size != nil && *resource.Size < 0 {
		return DriveItem{}, errors.New("item size is invalid")
	}
	item := DriveItem{
		ID: resource.ID, Name: resource.Name, WebURL: resource.WebURL, ETag: resource.ETag, CTag: resource.CTag,
		CreatedDateTime: resource.CreatedDateTime, LastModifiedDateTime: resource.LastModifiedDateTime,
		IsFolder: resource.Folder != nil || resource.Root != nil, IsPackage: resource.Package != nil,
	}
	if resource.Size != nil {
		item.SizeBytes = *resource.Size
	}
	if reference := resource.ParentReference; reference != nil {
		item.DriveID, item.DriveType, item.SiteID, item.ParentPath = reference.DriveID, reference.DriveType, reference.SiteID, reference.Path
		item.ParentFolderID = reference.ID
	}
	if resource.Folder != nil {
		item.ChildCount = resource.Folder.ChildCount
	}
	if resource.File != nil {
		item.MimeType = resource.File.MimeType
		if resource.File.Hashes != nil {
			item.QuickXorHash = resource.File.Hashes.QuickXorHash
		}
	}
	item.CreatedBy = convertIdentitySet(resource.CreatedBy)
	item.LastModifiedBy = convertIdentitySet(resource.LastModifiedBy)
	return item, nil
}

func convertIdentitySet(identities *graphIdentitySet) *ItemIdentity {
	switch {
	case identities == nil:
		return nil
	case identities.User != nil:
		return &ItemIdentity{DisplayName: identities.User.DisplayName, Email: identities.User.Email}
	case identities.Application != nil:
		return &ItemIdentity{DisplayName: identities.Application.DisplayName, IsApplication: true}
	default:
		return nil
	}
}

// decodeItemResponse decodes and validates one driveItem response body.
func decodeItemResponse(content []byte) (DriveItem, error) {
	var resource graphItemResource
	if err := json.Unmarshal(content, &resource); err != nil {
		return DriveItem{}, err
	}
	return convertItemResource(resource)
}

// validateItemName rejects names Microsoft documents as invalid in OneDrive and SharePoint.
func validateItemName(name string, isFolder bool) error {
	switch {
	case strings.TrimSpace(name) == "":
		return errors.New("name is required")
	case len(name) > maxItemNameBytes || !utf8.ValidString(name):
		return errors.New("name is too long or not valid UTF-8")
	case strings.TrimSpace(name) != name:
		return errors.New("name cannot start or end with whitespace")
	case strings.ContainsAny(name, reservedItemNameCharacters):
		return errors.New(`name cannot contain / \ * < > ? : | # % or "`)
	case strings.HasPrefix(name, "~"):
		return errors.New("name cannot start with ~")
	case isFolder && strings.HasSuffix(name, "."):
		return errors.New("folder name cannot end with a period")
	}
	for _, character := range name {
		if character < 0x20 || character == 0x7f {
			return errors.New("name cannot contain control characters")
		}
	}
	return nil
}

func validateDriveAndFolderIDs(driveID string, folderID string) error {
	if driveID != "" && !isGraphID(driveID) {
		return errors.New("driveId must be a Microsoft Graph drive ID or blank")
	}
	if folderID != "" && !isGraphID(folderID) {
		return errors.New("folder ID must be a Microsoft Graph item ID, root, or blank")
	}
	return nil
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

func isGraphID(value string) bool { return graphIDPattern.MatchString(value) }

// isPlainMediaType reports whether value is exactly one lowercase type/subtype without parameters.
func isPlainMediaType(value string) bool {
	mediaType, parameters, err := mime.ParseMediaType(value)
	return err == nil && len(parameters) == 0 && mediaType == value && strings.Count(value, "/") == 1 &&
		!strings.HasPrefix(value, "/") && !strings.HasSuffix(value, "/")
}

// isRetryableStatus reports throttling and server failures except 501 and a full quota (507).
func isRetryableStatus(status int, codes []string) bool {
	if status == http.StatusNotImplemented || status == http.StatusInsufficientStorage {
		return false
	}
	return status == http.StatusRequestTimeout || status == http.StatusTooManyRequests || status >= 500 || hasAnyCode(codes, graphRetryableErrorCodes)
}

func hasAnyCode(codes []string, wanted map[string]bool) bool {
	for _, code := range codes {
		if wanted[code] {
			return true
		}
	}
	return false
}

func hasCode(codes []string, wanted string) bool {
	return hasAnyCode(codes, map[string]bool{wanted: true})
}

func statusFailureKind(status int, codes []string) sdkgo.FailureKind {
	switch {
	case status == http.StatusUnauthorized:
		return sdkgo.FailureAuthentication
	case status == http.StatusTooManyRequests || hasAnyCode(codes, graphRetryableErrorCodes):
		return sdkgo.FailureRateLimit
	case status == http.StatusForbidden:
		return sdkgo.FailureAuthorization
	case status == http.StatusNotFound:
		return sdkgo.FailureNotFound
	case status == http.StatusConflict || status == http.StatusLocked || status == http.StatusPreconditionFailed:
		return sdkgo.FailureConflict
	case status == http.StatusInsufficientStorage:
		return sdkgo.FailureQuotaExhausted
	case status >= 500:
		return sdkgo.FailureAvailability
	default:
		return sdkgo.FailureProviderRejection
	}
}

func graphFailure(kind sdkgo.FailureKind, operationID, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: graphProviderName, Operation: operationID, Message: message}
}

func graphFailurePointer(kind sdkgo.FailureKind, operationID, message string) *sdkgo.Failure {
	failure := graphFailure(kind, operationID, message)
	return &failure
}

// formatByteCount renders a byte limit for a validation message.
func formatByteCount(byteCount int64) string { return strconv.FormatInt(byteCount, 10) + " bytes" }
