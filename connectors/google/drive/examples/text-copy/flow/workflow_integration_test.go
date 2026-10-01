//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package textcopy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/google/drive"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	sourceFolderID      = "fld_sales"
	destinationFolderID = "fld_finance"
	// appliedThenRateLimitedCopyName makes the fake apply an upload and then answer 429, standing
	// in for an attempt whose success response was lost before Dex committed the Step.
	appliedThenRateLimitedCopyName = "applied-then-rate-limited.txt"
	// uncertainCopyName makes the fake apply an upload and then answer 500.
	uncertainCopyName = "uncertain.txt"
)

func TestTextCopyExampleRoutesEveryBusinessOutcomeWithRealDex(t *testing.T) {
	provider := newDriveProvider(t)
	flow, harness := newDriveIntegrationHarness(t, provider.URL)
	harness.startWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	runID := strconv.FormatInt(time.Now().UnixNano(), 10)

	copied := runTextCopy(t, ctx, harness.client, flow, "copied-"+runID, Input{SourceName: "Ops Policy", CopyName: "Ops Policy " + runID + ".txt"})
	require.Equal(t, dex.FlowCompleted, copied.status)
	require.Equal(t, StatusCopied, copied.outcome.Status)
	require.Equal(t, "file_policy", copied.outcome.Source.ID)
	require.Equal(t, "text/plain", copied.outcome.TextMimeType)
	require.Equal(t, int64(len("Ops Policy\nRefunds above 500 need approval.")), copied.outcome.TextByteCount)
	require.False(t, copied.outcome.IsCopyFromEarlierAttempt)
	require.Equal(t, []string{destinationFolderID}, copied.outcome.Copy.Parents)
	createdCopy := provider.fileNamed(t, "Ops Policy "+runID+".txt")
	require.Equal(t, createdCopy.id, copied.outcome.Copy.ID)
	require.Equal(t, "Ops Policy\nRefunds above 500 need approval.", string(createdCopy.content))
	require.Equal(t, "text/plain", createdCopy.mimeType)
	require.NotEmpty(t, createdCopy.idempotencyKey)

	ambiguous := runTextCopy(t, ctx, harness.client, flow, "ambiguous-"+runID, Input{SourceName: "Account Hierarchy", CopyName: "never-" + runID + ".txt"})
	require.Equal(t, dex.FlowCompleted, ambiguous.status)
	require.Equal(t, StatusSourceAmbiguous, ambiguous.outcome.Status)
	require.ElementsMatch(t, []string{"file_ah", "file_ah_copy"}, candidateIDs(ambiguous.outcome.SourceCandidates))

	missing := runTextCopy(t, ctx, harness.client, flow, "missing-"+runID, Input{SourceName: "Ops Polcy", CopyName: "never-" + runID + ".txt"})
	require.Equal(t, dex.FlowCompleted, missing.status)
	require.Equal(t, StatusSourceNotFound, missing.outcome.Status)
	require.Equal(t, 0, provider.uploadsNamed("never-"+runID+".txt"))

	rejected := runTextCopy(t, ctx, harness.client, flow, "rejected-"+runID, Input{SourceName: " ", CopyName: "never-" + runID + ".txt"})
	require.Equal(t, dex.FlowFailed, rejected.status)
}

func TestTextCopyRetryReusesTheFileAnEarlierAttemptCreatedWithRealDex(t *testing.T) {
	provider := newDriveProvider(t)
	flow, harness := newDriveIntegrationHarness(t, provider.URL)
	harness.startWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	flowID := "retried-upload-" + strconv.FormatInt(time.Now().UnixNano(), 10)

	result := runTextCopy(t, ctx, harness.client, flow, flowID, Input{SourceName: "Ops Policy", CopyName: appliedThenRateLimitedCopyName})
	require.Equal(t, dex.FlowCompleted, result.status)
	require.Equal(t, StatusCopied, result.outcome.Status)
	require.True(t, result.outcome.IsCopyFromEarlierAttempt)
	require.Equal(t, 1, provider.uploadsNamed(appliedThenRateLimitedCopyName))
	require.Equal(t, provider.fileNamed(t, appliedThenRateLimitedCopyName).id, result.outcome.Copy.ID)
	require.GreaterOrEqual(t, provider.idempotencyLookups(), 2)
}

func TestTextCopyUncertainUploadFailsTheFlowWithoutResendingWithRealDex(t *testing.T) {
	provider := newDriveProvider(t)
	flow, harness := newDriveIntegrationHarness(t, provider.URL)
	harness.startWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	flowID := "uncertain-upload-" + strconv.FormatInt(time.Now().UnixNano(), 10)

	result := runTextCopy(t, ctx, harness.client, flow, flowID, Input{SourceName: "Ops Policy", CopyName: uncertainCopyName})
	require.Equal(t, dex.FlowFailed, result.status)
	require.Equal(t, 1, provider.uploadsNamed(uncertainCopyName))
}

type textCopyRun struct {
	status  dex.FlowStatus
	outcome Outcome
}

func runTextCopy(t *testing.T, ctx context.Context, client *dex.Client, flow *Flow, flowID string, input Input) textCopyRun {
	t.Helper()
	_, err := client.StartFlow(ctx, flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	run := textCopyRun{status: result.Status}
	if result.Status == dex.FlowCompleted {
		require.NoError(t, result.DecodeSingleOutput(&run.outcome))
	}
	return run
}

func candidateIDs(files []drive.FileSummary) []string {
	ids := make([]string, 0, len(files))
	for _, file := range files {
		ids = append(ids, file.ID)
	}
	return ids
}

type driveFile struct {
	id             string
	name           string
	mimeType       string
	parent         string
	isTrashed      bool
	modifiedTime   string
	content        []byte
	idempotencyKey string
}

// driveProvider is a stateful fake of the Drive files API subset the example uses.
type driveProvider struct {
	*httptest.Server
	t                      *testing.T
	mutex                  sync.Mutex
	files                  []*driveFile
	uploadCountsByName     map[string]int
	idempotencyLookupCount int
}

var (
	appPropertiesQueryPattern = regexp.MustCompile(`^appProperties has \{ key='dexIdempotencyKey' and value='([^'\\]+)' \}$`)
	quotedValuePattern        = `'((?:[^'\\]|\\.)*)'`
	nameTermPattern           = regexp.MustCompile(`^name (=|contains) ` + quotedValuePattern + `$`)
	parentTermPattern         = regexp.MustCompile(`^` + quotedValuePattern + ` in parents$`)
)

func newDriveProvider(t *testing.T) *driveProvider {
	t.Helper()
	provider := &driveProvider{t: t, uploadCountsByName: map[string]int{}, files: []*driveFile{
		{id: "file_policy", name: "Ops Policy", mimeType: "application/vnd.google-apps.document", parent: sourceFolderID, modifiedTime: "2026-01-20T09:00:00Z", content: []byte("\xef\xbb\xbfOps Policy\nRefunds above 500 need approval.")},
		{id: "file_policy_trashed", name: "Ops Policy", mimeType: "application/vnd.google-apps.document", parent: sourceFolderID, isTrashed: true, modifiedTime: "2026-01-25T09:00:00Z", content: []byte("DRAFT")},
		{id: "file_policy_archive", name: "Ops Policy", mimeType: "application/vnd.google-apps.document", parent: "fld_archive", modifiedTime: "2025-12-31T23:00:00Z", content: []byte("2025 policy")},
		{id: "file_ah", name: "Account Hierarchy", mimeType: "application/vnd.google-apps.spreadsheet", parent: sourceFolderID, modifiedTime: "2026-01-20T09:00:00Z", content: []byte("account,parent\n")},
		{id: "file_ah_copy", name: "Account Hierarchy", mimeType: "application/vnd.google-apps.spreadsheet", parent: sourceFolderID, modifiedTime: "2026-01-02T09:00:00Z", content: []byte("account,parent\n")},
		{id: "file_ah_final", name: "Account Hierarchy (2025 FINAL)", mimeType: "application/vnd.google-apps.spreadsheet", parent: sourceFolderID, modifiedTime: "2025-12-31T23:00:00Z", content: []byte("account,parent\n")},
	}}
	provider.Server = httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *driveProvider) serveHTTP(response http.ResponseWriter, request *http.Request) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	if request.Header.Get("Authorization") != "Bearer drive-integration-token" {
		provider.writeJSON(response, http.StatusUnauthorized, map[string]any{"error": map[string]any{"code": 401}})
		return
	}
	switch {
	case request.Method == http.MethodGet && request.URL.Path == "/drive/v3/files":
		provider.listFiles(response, request.URL.Query().Get("q"))
	case request.Method == http.MethodGet && strings.HasSuffix(request.URL.Path, "/export"):
		file := provider.fileByID(strings.TrimSuffix(strings.TrimPrefix(request.URL.Path, "/drive/v3/files/"), "/export"))
		if file == nil {
			provider.writeJSON(response, http.StatusNotFound, map[string]any{"error": map[string]any{"code": 404}})
			return
		}
		_, err := response.Write(file.content)
		require.NoError(provider.t, err)
	case request.Method == http.MethodGet && strings.HasPrefix(request.URL.Path, "/drive/v3/files/"):
		file := provider.fileByID(strings.TrimPrefix(request.URL.Path, "/drive/v3/files/"))
		if file == nil {
			provider.writeJSON(response, http.StatusNotFound, map[string]any{"error": map[string]any{"code": 404}})
			return
		}
		provider.writeJSON(response, http.StatusOK, file.resource())
	case request.Method == http.MethodPost && request.URL.Path == "/upload/drive/v3/files":
		provider.createFile(response, request)
	default:
		http.NotFound(response, request)
	}
}

func (provider *driveProvider) listFiles(response http.ResponseWriter, query string) {
	if match := appPropertiesQueryPattern.FindStringSubmatch(query); match != nil {
		provider.idempotencyLookupCount++
		var matches []map[string]any
		for _, file := range provider.files {
			if file.idempotencyKey == match[1] {
				matches = append(matches, file.resource())
			}
		}
		provider.writeJSON(response, http.StatusOK, map[string]any{"files": matches})
		return
	}
	var matches []map[string]any
	for _, file := range provider.files {
		if provider.matchesSearchQuery(file, query) {
			matches = append(matches, file.resource())
		}
	}
	provider.writeJSON(response, http.StatusOK, map[string]any{"files": matches})
}

// matchesSearchQuery evaluates the and-joined terms that searchFiles emits.
func (provider *driveProvider) matchesSearchQuery(file *driveFile, query string) bool {
	for _, term := range strings.Split(query, " and ") {
		switch {
		case term == "trashed = false":
			if file.isTrashed {
				return false
			}
		case nameTermPattern.MatchString(term):
			match := nameTermPattern.FindStringSubmatch(term)
			name := unescapeDriveQueryValue(match[2])
			if (match[1] == "=" && file.name != name) || (match[1] == "contains" && !strings.HasPrefix(file.name, name)) {
				return false
			}
		case parentTermPattern.MatchString(term):
			if file.parent != unescapeDriveQueryValue(parentTermPattern.FindStringSubmatch(term)[1]) {
				return false
			}
		default:
			provider.t.Errorf("unexpected Drive query term %q", term)
			return false
		}
	}
	return true
}

func (provider *driveProvider) createFile(response http.ResponseWriter, request *http.Request) {
	body, err := io.ReadAll(request.Body)
	require.NoError(provider.t, err)
	_, parameters, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	require.NoError(provider.t, err)
	reader := multipart.NewReader(bytes.NewReader(body), parameters["boundary"])
	metadataPart, err := reader.NextPart()
	require.NoError(provider.t, err)
	var metadata struct {
		Name          string            `json:"name"`
		MimeType      string            `json:"mimeType"`
		Parents       []string          `json:"parents"`
		AppProperties map[string]string `json:"appProperties"`
	}
	require.NoError(provider.t, json.NewDecoder(metadataPart).Decode(&metadata))
	mediaPart, err := reader.NextPart()
	require.NoError(provider.t, err)
	content, err := io.ReadAll(mediaPart)
	require.NoError(provider.t, err)
	provider.uploadCountsByName[metadata.Name]++
	file := &driveFile{
		id: fmt.Sprintf("created_%d", len(provider.files)+1), name: metadata.Name, mimeType: metadata.MimeType,
		modifiedTime: "2026-09-30T12:00:00Z", content: content, idempotencyKey: metadata.AppProperties["dexIdempotencyKey"],
	}
	if len(metadata.Parents) == 1 {
		file.parent = metadata.Parents[0]
	}
	provider.files = append(provider.files, file)
	switch metadata.Name {
	case appliedThenRateLimitedCopyName:
		response.Header().Set("Retry-After", "1")
		provider.writeJSON(response, http.StatusTooManyRequests, map[string]any{"error": map[string]any{"code": 429}})
	case uncertainCopyName:
		provider.writeJSON(response, http.StatusInternalServerError, map[string]any{"error": map[string]any{"code": 500}})
	default:
		provider.writeJSON(response, http.StatusOK, file.resource())
	}
}

func (provider *driveProvider) fileByID(id string) *driveFile {
	for _, file := range provider.files {
		if file.id == id {
			return file
		}
	}
	return nil
}

func (provider *driveProvider) fileNamed(t *testing.T, name string) driveFile {
	t.Helper()
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	var found []driveFile
	for _, file := range provider.files {
		if file.name == name {
			found = append(found, *file)
		}
	}
	require.Len(t, found, 1)
	return found[0]
}

func (provider *driveProvider) uploadsNamed(name string) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.uploadCountsByName[name]
}

func (provider *driveProvider) idempotencyLookups() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.idempotencyLookupCount
}

func (provider *driveProvider) writeJSON(response http.ResponseWriter, status int, body any) {
	contents, err := json.Marshal(body)
	require.NoError(provider.t, err)
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_, err = response.Write(contents)
	require.NoError(provider.t, err)
}

func (file *driveFile) resource() map[string]any {
	resource := map[string]any{
		"id": file.id, "name": file.name, "mimeType": file.mimeType, "trashed": file.isTrashed,
		"modifiedTime": file.modifiedTime, "createdTime": file.modifiedTime,
	}
	if file.parent != "" {
		resource["parents"] = []string{file.parent}
	}
	if !strings.HasPrefix(file.mimeType, "application/vnd.google-apps.") {
		resource["size"] = strconv.Itoa(len(file.content))
	}
	return resource
}

func unescapeDriveQueryValue(value string) string {
	return strings.NewReplacer(`\\`, `\`, `\'`, `'`).Replace(value)
}

type driveIntegrationHarness struct {
	registry      *dex.Registry
	cache         *blobcache.Cache
	serverAddress string
	workerAddress string
	worker        *dex.Worker
	workerResult  chan error
	client        *dex.Client
}

func newDriveIntegrationHarness(t *testing.T, endpoint string) (*Flow, *driveIntegrationHarness) {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "google", Name: ConnectionName}
	providerClient, err := drive.New(drive.Config{Endpoint: endpoint}, sdkgo.StaticCredentialProvider[drive.Credentials]{
		reference: {AccessToken: sdkgo.NewSecretString("drive-integration-token")},
	})
	require.NoError(t, err)
	connection, err := drive.NewConnection(providerClient, reference)
	require.NoError(t, err)
	flow := NewFlow(connection,
		sdkgo.ConnectorLoadedConfiguration[FolderConfiguration]{Reference: SourceFolderConfigurationRef(), Value: FolderConfiguration{FolderID: sourceFolderID, FolderName: "Sales"}},
		sdkgo.ConnectorLoadedConfiguration[FolderConfiguration]{Reference: DestinationFolderConfigurationRef(), Value: FolderConfiguration{FolderID: destinationFolderID, FolderName: "Finance"}},
	)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	workerAddress := net.JoinHostPort("127.0.0.1", availableDriveIntegrationPort(t))
	harness := &driveIntegrationHarness{
		registry: registry, cache: cache, serverAddress: environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801"), workerAddress: workerAddress,
	}
	harness.client, err = dex.NewClient(registry, cache, dex.ClientOptions{
		FlowServiceAddress: harness.serverAddress, WorkerTarget: &dex.WorkerTarget{Address: workerAddress},
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		if harness.worker != nil {
			harness.stopWorker(t)
		}
		require.NoError(t, errors.Join(harness.client.Close(), harness.cache.Close()))
	})
	return flow, harness
}

func (harness *driveIntegrationHarness) startWorker(t *testing.T) {
	t.Helper()
	worker, err := dex.NewWorker(harness.registry, harness.cache, dex.WorkerOptions{
		BindAddress: harness.workerAddress, FlowServiceAddress: harness.serverAddress,
		WorkerTarget: dex.WorkerTarget{Address: harness.workerAddress},
	})
	require.NoError(t, err)
	harness.worker = worker
	harness.workerResult = make(chan error, 1)
	go func() { harness.workerResult <- worker.Start() }()
}

func (harness *driveIntegrationHarness) stopWorker(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, errors.Join(harness.worker.Stop(ctx), <-harness.workerResult))
	harness.worker = nil
	harness.workerResult = nil
}

func availableDriveIntegrationPort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	return strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
}

func environmentOr(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
