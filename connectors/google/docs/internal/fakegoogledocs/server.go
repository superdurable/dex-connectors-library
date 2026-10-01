// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package fakegoogledocs is a stateful, credential-checking fake of the Google
// Docs and Drive API subset the connector uses. It models a document as
// paragraphs, applies documents.batchUpdate only at the required revision as
// Google documents, and lets tests delay, hold, or answer a write ambiguously
// after applying it.
package fakegoogledocs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf16"
)

const (
	// FirstTabID is the tab ID of every fake document's only tab.
	FirstTabID = "t.0"
	// GoogleDocumentMimeType is the Drive MIME type of a Google Doc.
	GoogleDocumentMimeType = "application/vnd.google-apps.document"
	listID                 = "kix.fake-list"
)

// Counter names reported by Count.
const (
	CountDocumentReads         = "documents.get"
	CountBatchUpdatesReceived  = "documents.batchUpdate.received"
	CountBatchUpdatesApplied   = "documents.batchUpdate.applied"
	CountStaleRevisionRejected = "documents.batchUpdate.staleRevision"
	CountDuplicateLookups      = "drive.files.lookup"
	CountCreatesReceived       = "drive.files.create"
)

var idempotencyLookupPattern = regexp.MustCompile(`^appProperties has \{ key='dexIdempotencyKey' and value='([^'\\]+)' \}$`)

// Paragraph is one fake document paragraph.
type Paragraph struct {
	// Text is the paragraph text without its terminating newline.
	Text string
	// NamedStyleType is a Docs named style such as HEADING_1; blank is NORMAL_TEXT.
	NamedStyleType string
	// IsBullet places the paragraph in the document's unordered list.
	IsBullet bool
}

// Document is one fake Google Doc.
type Document struct {
	// ID is the document ID.
	ID string
	// Title is the document title.
	Title string
	// Paragraphs is the first tab's body.
	Paragraphs []Paragraph
	revision   int
}

// RevisionID is the opaque revision ID Google would report for the document.
func (document Document) RevisionID() string {
	return fmt.Sprintf("rev-%s-%d", document.ID, document.revision)
}

// BodyText is the body text as Google's text runs concatenate it.
func (document Document) BodyText() string {
	var text strings.Builder
	for _, paragraph := range document.Paragraphs {
		text.WriteString(paragraph.Text + "\n")
	}
	return text.String()
}

// CreateResponse selects how the fake answers one Drive create after creating the document.
type CreateResponse int

const (
	// CreateResponseCreated answers 200 with the new file.
	CreateResponseCreated CreateResponse = iota
	// CreateResponseRateLimitedWithoutCreating answers 429 and creates nothing.
	CreateResponseRateLimitedWithoutCreating
	// CreateResponseCreatedThenRateLimited creates the document, then answers 429 as a lost response would surface.
	CreateResponseCreatedThenRateLimited
	// CreateResponseCreatedThenServerError creates the document, then answers 500.
	CreateResponseCreatedThenServerError
)

// WriteBehavior shapes the first batch update of one kind.
type WriteBehavior struct {
	// DelayBeforeApplying sleeps before the revision check, so a concurrent batch can win first.
	DelayBeforeApplying time.Duration
	// DelayAfterApplying sleeps after applying, before answering.
	DelayAfterApplying time.Duration
	// StatusAfterApplying answers this status instead of 200 after applying; zero answers 200.
	StatusAfterApplying int
}

// WriteKind classifies a batch update for WriteBehavior.
type WriteKind string

const (
	// WriteKindReplace is a batch with replaceAllText or deleteContentRange.
	WriteKindReplace WriteKind = "replace"
	// WriteKindAppend is a batch that only inserts and restyles text.
	WriteKindAppend WriteKind = "append"
)

type driveFile struct {
	id            string
	name          string
	parents       []string
	appProperties map[string]string
	createdTime   time.Time
}

// Server is the fake Docs and Drive HTTP server.
type Server struct {
	*httptest.Server
	t           testing.TB
	accessToken string

	mutex                     sync.Mutex
	documents                 map[string]*Document
	files                     []*driveFile
	counts                    map[string]int
	createResponses           []CreateResponse
	firstCreateDelay          time.Duration
	firstCreateHold           chan struct{}
	isCreatedHiddenFromLookup bool
	writeBehaviors            map[WriteKind]WriteBehavior
	handledWriteKinds         map[WriteKind]bool
	nextFileNumber            int
	delayedRequests           sync.WaitGroup
}

// New starts a fake server that accepts only accessToken as a bearer credential.
func New(t testing.TB, accessToken string) *Server {
	t.Helper()
	server := &Server{
		t: t, accessToken: accessToken, documents: map[string]*Document{}, counts: map[string]int{},
		writeBehaviors: map[WriteKind]WriteBehavior{}, handledWriteKinds: map[WriteKind]bool{},
	}
	server.Server = httptest.NewServer(http.HandlerFunc(server.serveHTTP))
	t.Cleanup(server.Close)
	return server
}

// AddDocument stores document at its first revision.
func (server *Server) AddDocument(document Document) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	document.revision = 1
	server.documents[document.ID] = &document
	server.files = append(server.files, &driveFile{id: document.ID, name: document.Title, createdTime: time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)})
}

// EditDocument replaces a stored document's paragraphs as a collaborator would, advancing its revision.
func (server *Server) EditDocument(id string, paragraphs []Paragraph) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	document, isFound := server.documents[id]
	if !isFound {
		server.t.Fatalf("fake document %s does not exist", id)
	}
	document.Paragraphs = paragraphs
	document.revision++
}

// Document returns a copy of the stored document.
func (server *Server) Document(id string) (Document, bool) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	document, isFound := server.documents[id]
	if !isFound {
		return Document{}, false
	}
	copied := *document
	copied.Paragraphs = append([]Paragraph(nil), document.Paragraphs...)
	return copied, true
}

// CreatedDocumentIDs lists the documents created through Drive, in creation order.
func (server *Server) CreatedDocumentIDs() []string {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	var ids []string
	for _, file := range server.files {
		if file.appProperties != nil {
			ids = append(ids, file.id)
		}
	}
	return ids
}

// Count returns how often the named event happened.
func (server *Server) Count(name string) int {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	return server.counts[name]
}

// QueueCreateResponses sets how the next Drive creates are answered, in order.
func (server *Server) QueueCreateResponses(responses ...CreateResponse) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	server.createResponses = append(server.createResponses, responses...)
}

// DelayFirstCreate answers the first Drive create delay after creating the document.
func (server *Server) DelayFirstCreate(delay time.Duration) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	server.firstCreateDelay = delay
}

// HoldFirstCreate answers the first Drive create only after hold is closed.
func (server *Server) HoldFirstCreate(hold chan struct{}) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	server.firstCreateHold = hold
}

// HideCreatedDocumentsFromLookup makes the duplicate lookup miss created documents, as a search index lag would.
func (server *Server) HideCreatedDocumentsFromLookup() {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	server.isCreatedHiddenFromLookup = true
}

// SetWriteBehavior shapes the first batch update of kind.
func (server *Server) SetWriteBehavior(kind WriteKind, behavior WriteBehavior) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	server.writeBehaviors[kind] = behavior
}

// WaitForDelayedRequests waits until every delayed or held request has answered.
func (server *Server) WaitForDelayedRequests(t testing.TB, timeout time.Duration) {
	t.Helper()
	finished := make(chan struct{})
	go func() {
		server.delayedRequests.Wait()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(timeout):
		t.Fatal("a delayed fake Google request did not finish")
	}
}

func (server *Server) serveHTTP(response http.ResponseWriter, request *http.Request) {
	body, err := io.ReadAll(request.Body)
	if err != nil {
		writeJSON(response, http.StatusBadRequest, errorBody(http.StatusBadRequest, "INVALID_ARGUMENT"))
		return
	}
	if request.Header.Get("Authorization") != "Bearer "+server.accessToken {
		writeJSON(response, http.StatusUnauthorized, errorBody(http.StatusUnauthorized, "UNAUTHENTICATED"))
		return
	}
	response.Header().Set("X-Goog-Request-Id", "fake-request")
	path := request.URL.Path
	switch {
	case request.Method == http.MethodGet && strings.HasPrefix(path, "/v1/documents/"):
		server.getDocument(response, request, strings.TrimPrefix(path, "/v1/documents/"))
	case request.Method == http.MethodPost && strings.HasPrefix(path, "/v1/documents/") && strings.HasSuffix(path, ":batchUpdate"):
		server.batchUpdate(response, strings.TrimSuffix(strings.TrimPrefix(path, "/v1/documents/"), ":batchUpdate"), body)
	case request.Method == http.MethodGet && path == "/drive/v3/files":
		server.lookUpEarlierDocument(response, request.URL.Query().Get("q"))
	case request.Method == http.MethodPost && path == "/drive/v3/files":
		server.createDocument(response, body, "", nil)
	case request.Method == http.MethodPost && path == "/upload/drive/v3/files":
		server.createUploadedDocument(response, request, body)
	default:
		writeJSON(response, http.StatusNotFound, errorBody(http.StatusNotFound, "NOT_FOUND"))
	}
}

func (server *Server) getDocument(response http.ResponseWriter, request *http.Request, id string) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	server.counts[CountDocumentReads]++
	query := request.URL.Query()
	if query.Get("includeTabsContent") != "true" || query.Get("fields") == "" || query.Get("suggestionsViewMode") == "" {
		server.t.Errorf("document read must request tabs, a field mask, and a suggestions view: %s", request.URL.RawQuery)
	}
	document, isFound := server.documents[id]
	if !isFound {
		writeJSON(response, http.StatusNotFound, errorBody(http.StatusNotFound, "NOT_FOUND"))
		return
	}
	writeJSON(response, http.StatusOK, document.resource())
}

func (server *Server) batchUpdate(response http.ResponseWriter, id string, body []byte) {
	var update struct {
		Requests     []map[string]json.RawMessage `json:"requests"`
		WriteControl struct {
			RequiredRevisionID string `json:"requiredRevisionId"`
		} `json:"writeControl"`
	}
	if err := json.Unmarshal(body, &update); err != nil || len(update.Requests) == 0 {
		writeJSON(response, http.StatusBadRequest, errorBody(http.StatusBadRequest, "INVALID_ARGUMENT"))
		return
	}
	kind := WriteKindAppend
	for _, request := range update.Requests {
		if request["replaceAllText"] != nil || request["deleteContentRange"] != nil {
			kind = WriteKindReplace
		}
	}
	behavior := server.claimWriteBehavior(kind)
	if behavior.DelayBeforeApplying > 0 {
		server.delayedRequests.Add(1)
		defer server.delayedRequests.Done()
		time.Sleep(behavior.DelayBeforeApplying)
	}
	server.mutex.Lock()
	document, isFound := server.documents[id]
	if !isFound {
		server.mutex.Unlock()
		writeJSON(response, http.StatusNotFound, errorBody(http.StatusNotFound, "NOT_FOUND"))
		return
	}
	if update.WriteControl.RequiredRevisionID != document.RevisionID() {
		server.counts[CountStaleRevisionRejected]++
		server.mutex.Unlock()
		writeJSON(response, http.StatusBadRequest, errorBody(http.StatusBadRequest, "INVALID_ARGUMENT"))
		return
	}
	paragraphs, replies, err := applyRequests(document.Paragraphs, update.Requests)
	if err != nil {
		server.t.Logf("fake Google Docs rejected a batch update: %v", err)
		server.mutex.Unlock()
		writeJSON(response, http.StatusBadRequest, errorBody(http.StatusBadRequest, "INVALID_ARGUMENT"))
		return
	}
	document.Paragraphs = paragraphs
	document.revision++
	server.counts[CountBatchUpdatesApplied]++
	answer := map[string]any{"documentId": id, "replies": replies, "writeControl": map[string]any{"requiredRevisionId": document.RevisionID()}}
	server.mutex.Unlock()
	if behavior.DelayAfterApplying > 0 {
		server.delayedRequests.Add(1)
		defer server.delayedRequests.Done()
		time.Sleep(behavior.DelayAfterApplying)
	}
	if behavior.StatusAfterApplying != 0 {
		writeJSON(response, behavior.StatusAfterApplying, errorBody(behavior.StatusAfterApplying, "UNAVAILABLE"))
		return
	}
	writeJSON(response, http.StatusOK, answer)
}

// claimWriteBehavior counts an arriving batch and hands the first one of kind its behavior.
func (server *Server) claimWriteBehavior(kind WriteKind) WriteBehavior {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	server.counts[CountBatchUpdatesReceived]++
	if server.handledWriteKinds[kind] {
		return WriteBehavior{}
	}
	server.handledWriteKinds[kind] = true
	return server.writeBehaviors[kind]
}

func (server *Server) lookUpEarlierDocument(response http.ResponseWriter, query string) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	match := idempotencyLookupPattern.FindStringSubmatch(query)
	if match == nil {
		server.t.Errorf("duplicate lookup must query only the idempotency app property: %q", query)
		writeJSON(response, http.StatusBadRequest, errorBody(http.StatusBadRequest, "INVALID_ARGUMENT"))
		return
	}
	server.counts[CountDuplicateLookups]++
	files := []map[string]any{}
	for _, file := range server.files {
		if file.appProperties["dexIdempotencyKey"] == match[1] && !server.isCreatedHiddenFromLookup {
			files = append(files, file.resource())
		}
	}
	writeJSON(response, http.StatusOK, map[string]any{"files": files, "incompleteSearch": false})
}

func (server *Server) createUploadedDocument(response http.ResponseWriter, request *http.Request, body []byte) {
	if request.URL.Query().Get("uploadType") != "multipart" {
		writeJSON(response, http.StatusBadRequest, errorBody(http.StatusBadRequest, "INVALID_ARGUMENT"))
		return
	}
	_, parameters, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil {
		writeJSON(response, http.StatusBadRequest, errorBody(http.StatusBadRequest, "INVALID_ARGUMENT"))
		return
	}
	reader := multipart.NewReader(bytes.NewReader(body), parameters["boundary"])
	metadataPart, err := reader.NextPart()
	if err != nil {
		writeJSON(response, http.StatusBadRequest, errorBody(http.StatusBadRequest, "INVALID_ARGUMENT"))
		return
	}
	metadata, err := io.ReadAll(metadataPart)
	if err != nil {
		writeJSON(response, http.StatusBadRequest, errorBody(http.StatusBadRequest, "INVALID_ARGUMENT"))
		return
	}
	mediaPart, err := reader.NextPart()
	if err != nil {
		writeJSON(response, http.StatusBadRequest, errorBody(http.StatusBadRequest, "INVALID_ARGUMENT"))
		return
	}
	media, err := io.ReadAll(mediaPart)
	if err != nil {
		writeJSON(response, http.StatusBadRequest, errorBody(http.StatusBadRequest, "INVALID_ARGUMENT"))
		return
	}
	mediaType, _, err := mime.ParseMediaType(mediaPart.Header.Get("Content-Type"))
	if err != nil {
		writeJSON(response, http.StatusBadRequest, errorBody(http.StatusBadRequest, "INVALID_ARGUMENT"))
		return
	}
	server.createDocument(response, metadata, mediaType, media)
}

// createDocument converts media into paragraphs as Drive's text and Markdown imports would.
func (server *Server) createDocument(response http.ResponseWriter, metadataJSON []byte, mediaType string, media []byte) {
	var metadata struct {
		Name          string            `json:"name"`
		MimeType      string            `json:"mimeType"`
		Parents       []string          `json:"parents"`
		AppProperties map[string]string `json:"appProperties"`
	}
	if err := json.Unmarshal(metadataJSON, &metadata); err != nil || metadata.MimeType != GoogleDocumentMimeType || metadata.Name == "" {
		writeJSON(response, http.StatusBadRequest, errorBody(http.StatusBadRequest, "INVALID_ARGUMENT"))
		return
	}
	var paragraphs []Paragraph
	switch mediaType {
	case "":
	case "text/plain":
		paragraphs = importPlainText(string(media))
	case "text/markdown":
		paragraphs = importMarkdown(string(media))
	default:
		writeJSON(response, http.StatusBadRequest, errorBody(http.StatusBadRequest, "INVALID_ARGUMENT"))
		return
	}
	server.mutex.Lock()
	server.counts[CountCreatesReceived]++
	answer := CreateResponseCreated
	if len(server.createResponses) > 0 {
		answer, server.createResponses = server.createResponses[0], server.createResponses[1:]
	}
	if answer == CreateResponseRateLimitedWithoutCreating {
		server.mutex.Unlock()
		response.Header().Set("Retry-After", "1")
		writeJSON(response, http.StatusTooManyRequests, errorBody(http.StatusTooManyRequests, "RESOURCE_EXHAUSTED"))
		return
	}
	server.nextFileNumber++
	id := fmt.Sprintf("createdDoc%d", server.nextFileNumber)
	if len(paragraphs) == 0 {
		paragraphs = []Paragraph{{}}
	}
	server.documents[id] = &Document{ID: id, Title: metadata.Name, Paragraphs: paragraphs, revision: 1}
	file := &driveFile{id: id, name: metadata.Name, parents: metadata.Parents, appProperties: metadata.AppProperties, createdTime: time.Date(2026, 10, 1, 9, 0, server.nextFileNumber, 0, time.UTC)}
	server.files = append(server.files, file)
	delay, hold := time.Duration(0), (chan struct{})(nil)
	if server.counts[CountCreatesReceived] == 1 {
		delay, hold = server.firstCreateDelay, server.firstCreateHold
	}
	resource := file.resource()
	server.mutex.Unlock()
	if delay > 0 || hold != nil {
		server.delayedRequests.Add(1)
		defer server.delayedRequests.Done()
	}
	if delay > 0 {
		time.Sleep(delay)
	}
	if hold != nil {
		<-hold
	}
	switch answer {
	case CreateResponseCreatedThenRateLimited:
		response.Header().Set("Retry-After", "1")
		writeJSON(response, http.StatusTooManyRequests, errorBody(http.StatusTooManyRequests, "RESOURCE_EXHAUSTED"))
	case CreateResponseCreatedThenServerError:
		writeJSON(response, http.StatusInternalServerError, errorBody(http.StatusInternalServerError, "INTERNAL"))
	default:
		writeJSON(response, http.StatusOK, resource)
	}
}

func (file *driveFile) resource() map[string]any {
	resource := map[string]any{
		"id": file.id, "name": file.name, "mimeType": GoogleDocumentMimeType, "trashed": false,
		"createdTime": file.createdTime.Format(time.RFC3339), "webViewLink": "https://docs.google.com/document/d/" + file.id + "/edit",
	}
	if len(file.parents) > 0 {
		resource["parents"] = file.parents
	}
	return resource
}

// resource renders the document as a Docs API Document with tab content.
func (document *Document) resource() map[string]any {
	content := []any{map[string]any{"endIndex": 1, "sectionBreak": map[string]any{}}}
	index := 1
	for _, paragraph := range document.Paragraphs {
		text := paragraph.Text + "\n"
		end := index + utf16Length(text)
		element := map[string]any{
			"startIndex": index, "endIndex": end,
			"paragraph": map[string]any{
				"elements":       []any{map[string]any{"startIndex": index, "endIndex": end, "textRun": map[string]any{"content": text}}},
				"paragraphStyle": map[string]any{"namedStyleType": defaultString(paragraph.NamedStyleType, "NORMAL_TEXT")},
			},
		}
		if paragraph.IsBullet {
			element["paragraph"].(map[string]any)["bullet"] = map[string]any{"listId": listID}
		}
		content = append(content, element)
		index = end
	}
	return map[string]any{
		"documentId": document.ID, "title": document.Title, "revisionId": document.RevisionID(),
		"tabs": []any{map[string]any{
			"tabProperties": map[string]any{"tabId": FirstTabID},
			"documentTab": map[string]any{
				"body":  map[string]any{"content": content},
				"lists": map[string]any{listID: map[string]any{"listProperties": map[string]any{"nestingLevels": []any{map[string]any{"glyphSymbol": "*"}}}}},
			},
		}},
	}
}

// applyRequests applies a batch to a copy, so a rejected batch changes nothing.
func applyRequests(original []Paragraph, requests []map[string]json.RawMessage) ([]Paragraph, []map[string]any, error) {
	paragraphs := append([]Paragraph(nil), original...)
	replies := make([]map[string]any, 0, len(requests))
	for _, request := range requests {
		reply := map[string]any{}
		var err error
		switch {
		case request["replaceAllText"] != nil:
			var replaced int
			paragraphs, replaced, err = replaceAllText(paragraphs, request["replaceAllText"])
			reply["replaceAllText"] = map[string]any{"occurrencesChanged": replaced}
		case request["insertText"] != nil:
			paragraphs, err = insertText(paragraphs, request["insertText"])
		case request["deleteContentRange"] != nil:
			paragraphs, err = deleteContentRange(paragraphs, request["deleteContentRange"])
		case request["deleteParagraphBullets"] != nil, request["updateParagraphStyle"] != nil:
			paragraphs, err = resetParagraphs(paragraphs, request)
		case request["updateTextStyle"] != nil:
			_, err = decodeRange(paragraphs, request["updateTextStyle"], false)
		default:
			err = fmt.Errorf("unsupported request %v", keys(request))
		}
		if err != nil {
			return nil, nil, err
		}
		replies = append(replies, reply)
	}
	return paragraphs, replies, nil
}

func replaceAllText(paragraphs []Paragraph, raw json.RawMessage) ([]Paragraph, int, error) {
	var request struct {
		ReplaceText  string `json:"replaceText"`
		ContainsText struct {
			Text      string `json:"text"`
			MatchCase bool   `json:"matchCase"`
		} `json:"containsText"`
		TabsCriteria struct {
			TabIDs []string `json:"tabIds"`
		} `json:"tabsCriteria"`
	}
	if err := json.Unmarshal(raw, &request); err != nil {
		return nil, 0, err
	}
	if !request.ContainsText.MatchCase || request.ContainsText.Text == "" || len(request.TabsCriteria.TabIDs) != 1 || request.TabsCriteria.TabIDs[0] != FirstTabID {
		return nil, 0, fmt.Errorf("replaceAllText must match case in the first tab only")
	}
	if strings.Contains(request.ReplaceText, "\n") {
		return nil, 0, fmt.Errorf("the fake does not replace with line breaks")
	}
	replaced := 0
	for index := range paragraphs {
		replaced += strings.Count(paragraphs[index].Text, request.ContainsText.Text)
		paragraphs[index].Text = strings.ReplaceAll(paragraphs[index].Text, request.ContainsText.Text, request.ReplaceText)
	}
	return paragraphs, replaced, nil
}

// insertText inserts into the paragraph that contains the index; new paragraphs copy its style, as Docs does.
func insertText(paragraphs []Paragraph, raw json.RawMessage) ([]Paragraph, error) {
	var request struct {
		Text     string `json:"text"`
		Location struct {
			Index int    `json:"index"`
			TabID string `json:"tabId"`
		} `json:"location"`
	}
	if err := json.Unmarshal(raw, &request); err != nil {
		return nil, err
	}
	if request.Location.TabID != FirstTabID {
		return nil, fmt.Errorf("insertText must name the first tab")
	}
	paragraphIndex, offset, err := locate(paragraphs, request.Location.Index)
	if err != nil {
		return nil, err
	}
	target := paragraphs[paragraphIndex]
	before, after := utf16Slice(target.Text, 0, offset), utf16Slice(target.Text, offset, -1)
	lines := strings.Split(request.Text, "\n")
	inserted := make([]Paragraph, 0, len(lines))
	for lineIndex, line := range lines {
		paragraph := target
		paragraph.Text = line
		if lineIndex == 0 {
			paragraph.Text = before + line
		}
		if lineIndex == len(lines)-1 {
			paragraph.Text += after
		}
		inserted = append(inserted, paragraph)
	}
	result := append(append(append([]Paragraph(nil), paragraphs[:paragraphIndex]...), inserted...), paragraphs[paragraphIndex+1:]...)
	return result, nil
}

// deleteContentRange merges the first and last affected paragraphs; the merge keeps the first one's style.
func deleteContentRange(paragraphs []Paragraph, raw json.RawMessage) ([]Paragraph, error) {
	documentRange, err := decodeRange(paragraphs, raw, true)
	if err != nil {
		return nil, err
	}
	first, firstOffset, err := locate(paragraphs, documentRange[0])
	if err != nil {
		return nil, err
	}
	last, lastOffset, err := locate(paragraphs, documentRange[1])
	if err != nil {
		return nil, err
	}
	merged := paragraphs[first]
	merged.Text = utf16Slice(paragraphs[first].Text, 0, firstOffset) + utf16Slice(paragraphs[last].Text, lastOffset, -1)
	result := append(append(append([]Paragraph(nil), paragraphs[:first]...), merged), paragraphs[last+1:]...)
	return result, nil
}

func resetParagraphs(paragraphs []Paragraph, request map[string]json.RawMessage) ([]Paragraph, error) {
	raw := request["deleteParagraphBullets"]
	if raw == nil {
		raw = request["updateParagraphStyle"]
	}
	documentRange, err := decodeRange(paragraphs, raw, false)
	if err != nil {
		return nil, err
	}
	position := 1
	for index := range paragraphs {
		start, end := position, position+utf16Length(paragraphs[index].Text)+1
		if start < documentRange[1] && end > documentRange[0] {
			if request["deleteParagraphBullets"] != nil {
				paragraphs[index].IsBullet = false
			} else {
				paragraphs[index].NamedStyleType = ""
			}
		}
		position = end
	}
	return paragraphs, nil
}

// decodeRange validates a first-tab range; a deletion may not include the body's final newline.
func decodeRange(paragraphs []Paragraph, raw json.RawMessage, isDeletion bool) ([2]int, error) {
	var request struct {
		Range struct {
			StartIndex int    `json:"startIndex"`
			EndIndex   int    `json:"endIndex"`
			TabID      string `json:"tabId"`
		} `json:"range"`
	}
	if err := json.Unmarshal(raw, &request); err != nil {
		return [2]int{}, err
	}
	bodyEnd := 1 + utf16Length(joinParagraphs(paragraphs))
	limit := bodyEnd
	if isDeletion {
		limit = bodyEnd - 1
	}
	if request.Range.TabID != FirstTabID || request.Range.StartIndex < 1 || request.Range.EndIndex <= request.Range.StartIndex || request.Range.EndIndex > limit {
		return [2]int{}, fmt.Errorf("range %+v is outside the body", request.Range)
	}
	return [2]int{request.Range.StartIndex, request.Range.EndIndex}, nil
}

// locate finds the paragraph and UTF-16 offset of a body index before the final newline.
func locate(paragraphs []Paragraph, index int) (int, int, error) {
	position := 1
	for paragraphIndex, paragraph := range paragraphs {
		length := utf16Length(paragraph.Text)
		if index >= position && index <= position+length {
			return paragraphIndex, index - position, nil
		}
		position += length + 1
	}
	return 0, 0, fmt.Errorf("index %d is outside the body", index)
}

func importPlainText(text string) []Paragraph {
	var paragraphs []Paragraph
	for _, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		paragraphs = append(paragraphs, Paragraph{Text: line})
	}
	return paragraphs
}

// importMarkdown understands the headings, bullets, and backslash escapes the connector writes.
func importMarkdown(text string) []Paragraph {
	var paragraphs []Paragraph
	for _, line := range strings.Split(text, "\n") {
		switch {
		case strings.TrimSpace(line) == "":
		case strings.HasPrefix(line, "- "):
			paragraphs = append(paragraphs, Paragraph{Text: unescapeMarkdown(strings.TrimPrefix(line, "- ")), IsBullet: true})
		case strings.HasPrefix(line, "#"):
			level := len(line) - len(strings.TrimLeft(line, "#"))
			paragraphs = append(paragraphs, Paragraph{Text: unescapeMarkdown(strings.TrimSpace(line[level:])), NamedStyleType: fmt.Sprintf("HEADING_%d", level)})
		default:
			paragraphs = append(paragraphs, Paragraph{Text: unescapeMarkdown(line)})
		}
	}
	return paragraphs
}

func unescapeMarkdown(line string) string {
	var text strings.Builder
	for index := 0; index < len(line); index++ {
		if line[index] == '\\' && index+1 < len(line) && strings.ContainsRune("!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~", rune(line[index+1])) {
			continue
		}
		text.WriteByte(line[index])
	}
	return text.String()
}

func joinParagraphs(paragraphs []Paragraph) string {
	var text strings.Builder
	for _, paragraph := range paragraphs {
		text.WriteString(paragraph.Text + "\n")
	}
	return text.String()
}

func utf16Length(value string) int {
	length := 0
	for _, character := range value {
		length += utf16.RuneLen(character)
	}
	return length
}

// utf16Slice slices value by UTF-16 offsets; a negative end means the end of value.
func utf16Slice(value string, start int, end int) string {
	encoded := utf16.Encode([]rune(value))
	if end < 0 || end > len(encoded) {
		end = len(encoded)
	}
	start = min(max(start, 0), end)
	return string(utf16.Decode(encoded[start:end]))
}

func keys(values map[string]json.RawMessage) []string {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	return names
}

func defaultString(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func errorBody(code int, status string) map[string]any {
	return map[string]any{"error": map[string]any{"code": code, "message": "fake Google error SENTINEL", "status": status}}
}

func writeJSON(response http.ResponseWriter, status int, body any) {
	contents, err := json.Marshal(body)
	if err != nil {
		http.Error(response, "fake response could not be encoded", http.StatusInternalServerError)
		return
	}
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	// A client that gave up on a delayed response cannot receive it.
	_, _ = response.Write(contents)
}
