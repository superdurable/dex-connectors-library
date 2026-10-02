// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package graphfake is a stateful, credential-checking stand-in for the
// Microsoft Graph drive API subset the OneDrive connector uses. It serves
// item reads by ID and path, folder listings with paging, drive search,
// uploads by path with @microsoft.graph.conflictBehavior, folder creation,
// and content downloads through a redirect to a pre-authenticated URL.
package graphfake

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	apiPrefix       = "/v1.0"
	downloadPrefix  = "/download/"
	rootItemID      = "root"
	defaultPageSize = 200
)

// Config describes the fake tenant.
type Config struct {
	// AccessToken is the only bearer token the API accepts.
	AccessToken string
	// MyDriveID is the drive /me/drive resolves to.
	MyDriveID string
	// DriveIDs lists every drive the fake serves, including MyDriveID.
	DriveIDs []string
}

// Item is one stored file or folder.
type Item struct {
	// ID is the item ID.
	ID string
	// Name is the item name.
	Name string
	// DriveID is the drive that holds the item.
	DriveID string
	// ParentID is the parent folder ID, or root.
	ParentID string
	// IsFolder reports a folder.
	IsFolder bool
	// MimeType is the file media type.
	MimeType string
	// Content is the file content.
	Content []byte
	// ModifiedAt is the last modification time.
	ModifiedAt time.Time
	// IsHashPending omits file.hashes, as for a file Graph has not hashed yet.
	IsHashPending bool
}

// Request is one request the fake received.
type Request struct {
	// Method is the HTTP method.
	Method string
	// Path is the decoded request path.
	Path string
	// Query is the decoded query.
	Query url.Values
	// Header is a copy of the request headers.
	Header http.Header
	// Body is the request body.
	Body []byte
}

// Response overrides the fake's answer to one request.
type Response struct {
	// StatusCode is the HTTP status.
	StatusCode int
	// Code is the Graph error code, sent in an error body when set.
	Code string
	// Header holds extra response headers.
	Header http.Header
	// Body replaces the JSON body when set.
	Body []byte
	// ShouldDropConnection closes the connection without a response.
	ShouldDropConnection bool
}

// Server is the running fake. It is safe for concurrent requests.
type Server struct {
	*httptest.Server
	config        Config
	mutex         sync.Mutex
	items         map[string]*Item
	requests      []Request
	nextItemIndex int
	beforeApply   func(Request) *Response
	afterApply    func(Request) *Response
	responseDelay func(Request) time.Duration
}

// NewServer starts a fake with an empty root folder in every drive. Callers close it.
func NewServer(config Config) *Server {
	server := &Server{config: config, items: map[string]*Item{}}
	server.Server = httptest.NewServer(http.HandlerFunc(server.serveHTTP))
	return server
}

// AddItem stores item; ParentID root is the drive root. It returns the stored item ID.
func (server *Server) AddItem(item Item) string {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	if item.ID == "" {
		item.ID = server.nextIDLocked()
	}
	if item.ModifiedAt.IsZero() {
		item.ModifiedAt = time.Date(2026, time.September, 30, 12, 0, 0, 0, time.UTC)
	}
	stored := item
	server.items[item.ID] = &stored
	return item.ID
}

// Intercept answers matching requests before they change state; nil leaves a request alone.
func (server *Server) Intercept(intercept func(Request) *Response) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	server.beforeApply = intercept
}

// InterceptAfterApply answers matching mutations after they change state, as when a response is lost.
func (server *Server) InterceptAfterApply(intercept func(Request) *Response) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	server.afterApply = intercept
}

// DelayResponses holds each matching response for the returned duration after
// the request has changed state, standing in for a slow upload.
func (server *Server) DelayResponses(delay func(Request) time.Duration) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	server.responseDelay = delay
}

// Requests returns every request received so far, in arrival order.
func (server *Server) Requests() []Request {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	return append([]Request(nil), server.requests...)
}

// CountRequests counts requests with method whose path satisfies matches.
func (server *Server) CountRequests(method string, matches func(path string) bool) int {
	count := 0
	for _, request := range server.Requests() {
		if request.Method == method && matches(request.Path) {
			count++
		}
	}
	return count
}

// ItemsNamed returns the stored items with name in parentID of driveID.
func (server *Server) ItemsNamed(driveID string, parentID string, name string) []Item {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	var found []Item
	for _, item := range server.items {
		if item.DriveID == driveID && item.ParentID == parentID && strings.EqualFold(item.Name, name) {
			found = append(found, *item)
		}
	}
	return found
}

// ItemCount returns the number of stored items.
func (server *Server) ItemCount() int {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	return len(server.items)
}

// QuickXorHash returns Microsoft's QuickXorHash, ported from the C# reference
// structure independently of the connector's implementation.
func QuickXorHash(content []byte) string {
	const widthInBits, shift, bitsInLastCell = 160, 11, 32
	var cells [3]uint64
	vectorArrayIndex, vectorOffset := 0, 0
	iterations := min(len(content), widthInBits)
	for i := 0; i < iterations; i++ {
		isLastCell := vectorArrayIndex == len(cells)-1
		bitsInVectorCell := 64
		if isLastCell {
			bitsInVectorCell = bitsInLastCell
		}
		if vectorOffset <= bitsInVectorCell-8 {
			for j := i; j < len(content); j += widthInBits {
				cells[vectorArrayIndex] ^= uint64(content[j]) << vectorOffset
			}
		} else {
			index1, index2 := vectorArrayIndex, vectorArrayIndex+1
			if isLastCell {
				index2 = 0
			}
			low := bitsInVectorCell - vectorOffset
			var xored byte
			for j := i; j < len(content); j += widthInBits {
				xored ^= content[j]
			}
			cells[index1] ^= uint64(xored) << vectorOffset
			cells[index2] ^= uint64(xored) >> low
		}
		vectorOffset += shift
		for vectorOffset >= bitsInVectorCell {
			if isLastCell {
				vectorArrayIndex = 0
			} else {
				vectorArrayIndex++
			}
			vectorOffset -= bitsInVectorCell
		}
	}
	digest := make([]byte, 24)
	binary.LittleEndian.PutUint64(digest[0:], cells[0])
	binary.LittleEndian.PutUint64(digest[8:], cells[1])
	binary.LittleEndian.PutUint64(digest[16:], cells[2])
	digest = digest[:20]
	var length [8]byte
	binary.LittleEndian.PutUint64(length[:], uint64(len(content)))
	for i := range length {
		digest[20-8+i] ^= length[i]
	}
	return base64.StdEncoding.EncodeToString(digest)
}

func (server *Server) serveHTTP(response http.ResponseWriter, request *http.Request) {
	body, err := io.ReadAll(request.Body)
	if err != nil {
		http.Error(response, "unreadable request", http.StatusBadRequest)
		return
	}
	recorded := Request{Method: request.Method, Path: request.URL.Path, Query: request.URL.Query(), Header: request.Header.Clone(), Body: body}
	server.mutex.Lock()
	server.requests = append(server.requests, recorded)
	beforeApply, afterApply, responseDelay := server.beforeApply, server.afterApply, server.responseDelay
	server.mutex.Unlock()

	if strings.HasPrefix(request.URL.Path, downloadPrefix) {
		server.serveDownload(response, request)
		return
	}
	response.Header().Set("request-id", fmt.Sprintf("fake-request-%d", len(server.Requests())))
	if request.Header.Get("Authorization") != "Bearer "+server.config.AccessToken {
		writeError(response, http.StatusUnauthorized, "InvalidAuthenticationToken")
		return
	}
	if beforeApply != nil {
		if override := beforeApply(recorded); override != nil {
			writeOverride(response, override)
			return
		}
	}
	status, header, payload := server.route(request, body)
	if responseDelay != nil {
		if delay := responseDelay(recorded); delay > 0 {
			time.Sleep(delay)
		}
	}
	if afterApply != nil {
		if override := afterApply(recorded); override != nil {
			writeOverride(response, override)
			return
		}
	}
	for name, values := range header {
		response.Header()[name] = values
	}
	if payload == nil {
		response.WriteHeader(status)
		return
	}
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_, _ = response.Write(payload) // A failed write means the client went away.
}

// route applies one Graph request and returns its status, extra headers, and JSON body.
func (server *Server) route(request *http.Request, body []byte) (int, http.Header, []byte) {
	path := strings.TrimPrefix(request.URL.Path, apiPrefix)
	if path == request.URL.Path {
		return errorPayload(http.StatusNotFound, "itemNotFound")
	}
	driveID, rest, ok := server.splitDrivePath(path)
	if !ok {
		return errorPayload(http.StatusNotFound, "itemNotFound")
	}
	server.mutex.Lock()
	defer server.mutex.Unlock()
	switch {
	case strings.HasPrefix(rest, "/root/search(q='") && strings.HasSuffix(rest, "')") && request.Method == http.MethodGet:
		searchText := strings.ReplaceAll(strings.TrimSuffix(strings.TrimPrefix(rest, "/root/search(q='"), "')"), "''", "'")
		return server.searchLocked(request, driveID, searchText)
	case strings.Contains(rest, ":/") && strings.HasSuffix(rest, ":/content") && request.Method == http.MethodPut:
		parentID, name := splitChildPath(strings.TrimSuffix(rest, ":/content"))
		return server.uploadLocked(request, driveID, parentID, name, body)
	case strings.Contains(rest, ":/") && request.Method == http.MethodGet:
		parentID, name := splitChildPath(rest)
		item := server.childLocked(driveID, parentID, name)
		if item == nil {
			return errorPayload(http.StatusNotFound, "itemNotFound")
		}
		return jsonPayload(http.StatusOK, server.resourceLocked(item))
	case strings.HasSuffix(rest, "/children") && request.Method == http.MethodGet:
		return server.listChildrenLocked(request, driveID, folderIDFromPath(strings.TrimSuffix(rest, "/children")))
	case strings.HasSuffix(rest, "/children") && request.Method == http.MethodPost:
		return server.createFolderLocked(request, driveID, folderIDFromPath(strings.TrimSuffix(rest, "/children")), body)
	case strings.HasSuffix(rest, "/content") && request.Method == http.MethodGet:
		item := server.items[folderIDFromPath(strings.TrimSuffix(rest, "/content"))]
		if item == nil || item.DriveID != driveID || item.IsFolder {
			return errorPayload(http.StatusNotFound, "itemNotFound")
		}
		header := http.Header{"Location": {server.URL + downloadPrefix + url.PathEscape(item.ID) + "?tempauth=fake-preauthenticated"}}
		return http.StatusFound, header, nil
	case request.Method == http.MethodGet && (rest == "/root" || strings.HasPrefix(rest, "/items/")):
		itemID := folderIDFromPath(rest)
		if itemID == rootItemID {
			return jsonPayload(http.StatusOK, server.rootResourceLocked(driveID))
		}
		item := server.items[itemID]
		if item == nil || item.DriveID != driveID {
			return errorPayload(http.StatusNotFound, "itemNotFound")
		}
		return jsonPayload(http.StatusOK, server.resourceLocked(item))
	default:
		return errorPayload(http.StatusBadRequest, "invalidRequest")
	}
}

func (server *Server) splitDrivePath(path string) (string, string, bool) {
	if rest, ok := strings.CutPrefix(path, "/me/drive"); ok {
		return server.config.MyDriveID, rest, true
	}
	rest, ok := strings.CutPrefix(path, "/drives/")
	if !ok {
		return "", "", false
	}
	driveID, rest, _ := strings.Cut(rest, "/")
	for _, known := range server.config.DriveIDs {
		if known == driveID {
			return driveID, "/" + rest, true
		}
	}
	return "", "", false
}

func (server *Server) searchLocked(request *http.Request, driveID string, searchText string) (int, http.Header, []byte) {
	var matches []*Item
	for _, item := range server.items {
		if item.DriveID == driveID && strings.Contains(strings.ToLower(item.Name), strings.ToLower(searchText)) {
			matches = append(matches, item)
		}
	}
	return server.pageLocked(request, matches)
}

func (server *Server) listChildrenLocked(request *http.Request, driveID string, folderID string) (int, http.Header, []byte) {
	if folderID != rootItemID {
		folder := server.items[folderID]
		if folder == nil || folder.DriveID != driveID || !folder.IsFolder {
			return errorPayload(http.StatusNotFound, "itemNotFound")
		}
	}
	var children []*Item
	for _, item := range server.items {
		if item.DriveID == driveID && item.ParentID == folderID {
			children = append(children, item)
		}
	}
	return server.pageLocked(request, children)
}

// pageLocked returns one page of items ordered by name, with an absolute next link.
func (server *Server) pageLocked(request *http.Request, items []*Item) (int, http.Header, []byte) {
	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name || (items[i].Name == items[j].Name && items[i].ID < items[j].ID)
	})
	pageSize := defaultPageSize
	if top, err := strconv.Atoi(request.URL.Query().Get("$top")); err == nil && top > 0 {
		pageSize = top
	}
	offset, _ := strconv.Atoi(request.URL.Query().Get("$skiptoken")) // A missing token starts at zero.
	page := map[string]any{"value": []any{}}
	values := []any{}
	for index := offset; index < len(items) && index < offset+pageSize; index++ {
		values = append(values, server.resourceLocked(items[index]))
	}
	page["value"] = values
	if offset+pageSize < len(items) {
		next := *request.URL
		query := next.Query()
		query.Set("$skiptoken", strconv.Itoa(offset+pageSize))
		next.RawQuery = query.Encode()
		page["@odata.nextLink"] = server.URL + next.RequestURI()
	}
	return jsonPayload(http.StatusOK, page)
}

func (server *Server) uploadLocked(request *http.Request, driveID string, parentID string, name string, content []byte) (int, http.Header, []byte) {
	if !server.isFolderLocked(driveID, parentID) {
		return errorPayload(http.StatusNotFound, "itemNotFound")
	}
	behavior := request.URL.Query().Get("@microsoft.graph.conflictBehavior")
	if behavior == "" {
		behavior = "replace"
	}
	existing := server.childLocked(driveID, parentID, name)
	switch {
	case existing != nil && (behavior == "fail" || existing.IsFolder):
		return errorPayload(http.StatusConflict, "nameAlreadyExists")
	case existing != nil && behavior == "replace":
		existing.Content, existing.MimeType = append([]byte(nil), content...), request.Header.Get("Content-Type")
		existing.ModifiedAt = existing.ModifiedAt.Add(time.Minute)
		return jsonPayload(http.StatusOK, server.resourceLocked(existing))
	case existing != nil:
		name = server.uniqueNameLocked(driveID, parentID, name)
	}
	item := &Item{
		ID: server.nextIDLocked(), Name: name, DriveID: driveID, ParentID: parentID, MimeType: request.Header.Get("Content-Type"),
		Content: append([]byte(nil), content...), ModifiedAt: time.Date(2026, time.September, 30, 12, 0, 0, 0, time.UTC),
	}
	server.items[item.ID] = item
	return jsonPayload(http.StatusCreated, server.resourceLocked(item))
}

func (server *Server) createFolderLocked(request *http.Request, driveID string, parentID string, body []byte) (int, http.Header, []byte) {
	if !server.isFolderLocked(driveID, parentID) {
		return errorPayload(http.StatusNotFound, "itemNotFound")
	}
	var folder struct {
		Name             string          `json:"name"`
		Folder           json.RawMessage `json:"folder"`
		ConflictBehavior string          `json:"@microsoft.graph.conflictBehavior"`
	}
	if err := json.Unmarshal(body, &folder); err != nil || folder.Name == "" || len(folder.Folder) == 0 {
		return errorPayload(http.StatusBadRequest, "invalidRequest")
	}
	behavior := folder.ConflictBehavior
	if queryBehavior := request.URL.Query().Get("@microsoft.graph.conflictBehavior"); queryBehavior != "" {
		behavior = queryBehavior
	}
	name := folder.Name
	if server.childLocked(driveID, parentID, name) != nil {
		if behavior != "rename" {
			return errorPayload(http.StatusConflict, "nameAlreadyExists")
		}
		name = server.uniqueNameLocked(driveID, parentID, name)
	}
	item := &Item{ID: server.nextIDLocked(), Name: name, DriveID: driveID, ParentID: parentID, IsFolder: true,
		ModifiedAt: time.Date(2026, time.September, 30, 12, 0, 0, 0, time.UTC)}
	server.items[item.ID] = item
	return jsonPayload(http.StatusCreated, server.resourceLocked(item))
}

func (server *Server) serveDownload(response http.ResponseWriter, request *http.Request) {
	if request.Header.Get("Authorization") != "" || request.URL.Query().Get("tempauth") != "fake-preauthenticated" {
		http.Error(response, "pre-authenticated downloads take no Authorization header", http.StatusUnauthorized)
		return
	}
	server.mutex.Lock()
	item := server.items[strings.TrimPrefix(request.URL.Path, downloadPrefix)]
	var content []byte
	if item != nil {
		content = append([]byte(nil), item.Content...)
	}
	server.mutex.Unlock()
	if item == nil {
		http.NotFound(response, request)
		return
	}
	response.Header().Set("Content-Type", "application/octet-stream")
	_, _ = response.Write(content) // A failed write means the client went away.
}

func (server *Server) childLocked(driveID string, parentID string, name string) *Item {
	for _, item := range server.items {
		if item.DriveID == driveID && item.ParentID == parentID && strings.EqualFold(item.Name, name) {
			return item
		}
	}
	return nil
}

func (server *Server) isFolderLocked(driveID string, folderID string) bool {
	if folderID == rootItemID {
		return true
	}
	folder := server.items[folderID]
	return folder != nil && folder.DriveID == driveID && folder.IsFolder
}

func (server *Server) uniqueNameLocked(driveID string, parentID string, name string) string {
	for index := 1; ; index++ {
		candidate := fmt.Sprintf("%s %d", name, index)
		if server.childLocked(driveID, parentID, candidate) == nil {
			return candidate
		}
	}
}

func (server *Server) nextIDLocked() string {
	server.nextItemIndex++
	return fmt.Sprintf("01FAKEITEM%04d", server.nextItemIndex)
}

func (server *Server) resourceLocked(item *Item) map[string]any {
	parentID := item.ParentID
	if parentID == rootItemID {
		parentID = "01FAKEROOT" + strings.ToUpper(strings.Trim(item.DriveID, "b!"))
	}
	resource := map[string]any{
		"id": item.ID, "name": item.Name, "webUrl": "https://contoso.sharepoint.com/fake/" + url.PathEscape(item.Name),
		"eTag": fmt.Sprintf("\"{%s},%d\"", item.ID, item.ModifiedAt.Unix()), "createdDateTime": "2026-09-30T12:00:00Z",
		"lastModifiedDateTime": item.ModifiedAt.UTC().Format(time.RFC3339),
		"parentReference":      map[string]any{"driveId": item.DriveID, "driveType": "business", "id": parentID, "path": "/drives/" + item.DriveID + "/root:"},
		"createdBy":            map[string]any{"user": map[string]any{"displayName": "Megan Bowen", "email": "megan@contoso.example"}},
	}
	if item.IsFolder {
		childCount := 0
		for _, candidate := range server.items {
			if candidate.ParentID == item.ID {
				childCount++
			}
		}
		resource["size"] = 0
		resource["folder"] = map[string]any{"childCount": childCount}
		return resource
	}
	file := map[string]any{"mimeType": item.MimeType}
	if !item.IsHashPending {
		file["hashes"] = map[string]any{"quickXorHash": QuickXorHash(item.Content)}
	}
	resource["size"] = len(item.Content)
	resource["file"] = file
	resource["cTag"] = fmt.Sprintf("\"c:{%s},%d\"", item.ID, len(item.Content))
	// Real Graph adds a download URL to default responses; the connector must drop it.
	resource["@microsoft.graph.downloadUrl"] = server.URL + downloadPrefix + url.PathEscape(item.ID) + "?tempauth=fake-preauthenticated"
	return resource
}

func (server *Server) rootResourceLocked(driveID string) map[string]any {
	return map[string]any{
		"id": "01FAKEROOT" + strings.ToUpper(strings.Trim(driveID, "b!")), "name": "root", "root": map[string]any{},
		"folder": map[string]any{"childCount": 0}, "size": 0, "parentReference": map[string]any{"driveId": driveID, "driveType": "business"},
	}
}

// splitChildPath splits "/root:/name" or "/items/ID:/name" into the parent ID and name.
func splitChildPath(path string) (string, string) {
	parentPath, name, _ := strings.Cut(path, ":/")
	return folderIDFromPath(parentPath), strings.TrimSuffix(name, ":")
}

func folderIDFromPath(path string) string {
	if path == "/root" || path == "" {
		return rootItemID
	}
	return strings.TrimPrefix(path, "/items/")
}

func writeOverride(response http.ResponseWriter, override *Response) {
	if override.ShouldDropConnection {
		hijacker, ok := response.(http.Hijacker)
		if !ok {
			return
		}
		connection, _, err := hijacker.Hijack()
		if err == nil {
			_ = connection.Close() // Dropping the connection is the simulated failure.
		}
		return
	}
	for name, values := range override.Header {
		response.Header()[name] = values
	}
	body := override.Body
	if body == nil && override.Code != "" {
		_, _, body = errorPayload(override.StatusCode, override.Code)
	}
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(override.StatusCode)
	_, _ = response.Write(body) // A failed write means the client went away.
}

func writeError(response http.ResponseWriter, status int, code string) {
	_, _, body := errorPayload(status, code)
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_, _ = response.Write(body) // A failed write means the client went away.
}

// errorPayload builds a Graph error body whose message is a sentinel that must never leak.
func errorPayload(status int, code string) (int, http.Header, []byte) {
	body, _ := json.Marshal(map[string]any{"error": map[string]any{
		"code": code, "message": "GRAPH-MESSAGE-SENTINEL", "innerError": map[string]any{"request-id": "fake"},
	}}) // A fixed map always encodes.
	return status, nil, body
}

func jsonPayload(status int, value any) (int, http.Header, []byte) {
	body, _ := json.Marshal(value) // Fake resources are plain maps that always encode.
	return status, nil, body
}
