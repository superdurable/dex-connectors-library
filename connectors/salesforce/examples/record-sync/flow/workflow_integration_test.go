//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package recordsync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/salesforce"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	integrationToken       = "salesforce-integration-token"
	externalIDField        = "ERP_Id__c"
	dataPathPrefix         = "/services/data/v62.0"
	lostResponseExternalID = "ERP-LOST-RESPONSE"
	slowExternalID         = "ERP-SLOW"
	// slowUpsertDelay outlasts the seven-second local phase of async Execute durability.
	slowUpsertDelay = 9 * time.Second
)

var matchQueryPattern = regexp.MustCompile(`^SELECT Id, (\w+) FROM (\w+) WHERE (\w+) = '((?:[^'\\]|\\.)*)' ORDER BY CreatedDate ASC LIMIT 5$`)

func TestRecordSyncRoutesEveryBusinessOutcomeWithRealDex(t *testing.T) {
	provider := newSalesforceProvider(t)
	flow, harness := newSalesforceIntegrationHarness(t, provider.URL)
	harness.startWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	runID := strconv.FormatInt(time.Now().UnixNano(), 10)

	linked := runRecordSync(t, ctx, harness.client, flow, "linked-"+runID, Input{
		MatchValue: "priya@meridian.example.com", ExternalID: "ERP-88213", Fields: map[string]string{"Title": "CTO"},
	})
	require.Equal(t, dex.FlowCompleted, linked.status)
	require.Equal(t, StatusLinked, linked.outcome.Status)
	require.Equal(t, provider.recordID("priya"), linked.outcome.RecordID)
	require.Equal(t, "ERP-88213", readBackText(t, linked.outcome, externalIDField))
	require.Equal(t, "CTO", readBackText(t, linked.outcome, "Title"))
	require.Equal(t, "ERP-88213", provider.fieldValue("priya", externalIDField))

	relinked := runRecordSync(t, ctx, harness.client, flow, "relinked-"+runID, Input{
		MatchValue: "jane@acme.example.com", ExternalID: "ERP-1001", Fields: map[string]string{"Title": "VP Finance"},
	})
	require.Equal(t, StatusLinked, relinked.outcome.Status)
	require.Equal(t, "VP Finance", provider.fieldValue("jane", "Title"))

	writesBeforeDecoys := provider.writeCount()
	conflicting := runRecordSync(t, ctx, harness.client, flow, "conflicting-"+runID, Input{
		MatchValue: "jordan@acme.example.com", ExternalID: "ERP-9999", Fields: map[string]string{"Title": "Controller"},
	})
	require.Equal(t, dex.FlowCompleted, conflicting.status)
	require.Equal(t, StatusConflictingExternalID, conflicting.outcome.Status)
	require.Equal(t, []string{provider.recordID("jordan")}, conflicting.outcome.CandidateRecordIDs)
	require.Equal(t, "ERP-2002", provider.fieldValue("jordan", externalIDField))

	ambiguous := runRecordSync(t, ctx, harness.client, flow, "ambiguous-"+runID, Input{
		MatchValue: "sam@globex.example.com", ExternalID: "ERP-4004", Fields: map[string]string{"LastName": "Lee"},
	})
	require.Equal(t, StatusAmbiguousMatch, ambiguous.outcome.Status)
	require.ElementsMatch(t, []string{provider.recordID("sam"), provider.recordID("samuel")}, ambiguous.outcome.CandidateRecordIDs)
	require.Equal(t, writesBeforeDecoys, provider.writeCount(), "decoys must not be written")

	created := runRecordSync(t, ctx, harness.client, flow, "created-"+runID, Input{
		MatchValue: "new.person@example.com", ExternalID: "ERP-3003",
		Fields: map[string]string{"LastName": "Person", "Email": "new.person@example.com"},
	})
	require.Equal(t, dex.FlowCompleted, created.status)
	require.Equal(t, StatusCreated, created.outcome.Status)
	require.Equal(t, []string{created.outcome.RecordID}, provider.contactIDsWithExternalID("ERP-3003"))
	require.Equal(t, "Person", readBackText(t, created.outcome, "LastName"))
	require.Nil(t, provider.fieldValue("lead", externalIDField), "the Lead with the same email is a decoy")

	rejected := runRecordSync(t, ctx, harness.client, flow, "rejected-"+runID, Input{
		MatchValue: "no.last.name@example.com", ExternalID: "ERP-5005", Fields: map[string]string{"FirstName": "Nomen"},
	})
	require.Equal(t, dex.FlowCompleted, rejected.status)
	require.Equal(t, StatusRejected, rejected.outcome.Status)
	require.Equal(t, []salesforce.ProviderError{{ErrorCode: "REQUIRED_FIELD_MISSING", Fields: []string{"LastName"}}}, rejected.outcome.ProviderErrors)
	require.Empty(t, provider.contactIDsWithExternalID("ERP-5005"))

	invalid := runRecordSync(t, ctx, harness.client, flow, "invalid-"+runID, Input{MatchValue: " ", ExternalID: "ERP-6006"})
	require.Equal(t, dex.FlowFailed, invalid.status)
}

func TestRecordSyncRetriedUpsertAfterALostResponseKeepsOneRecordWithRealDex(t *testing.T) {
	provider := newSalesforceProvider(t)
	flow, harness := newSalesforceIntegrationHarness(t, provider.URL)
	harness.startWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	result := runRecordSync(t, ctx, harness.client, flow, "lost-response-"+strconv.FormatInt(time.Now().UnixNano(), 10), Input{
		MatchValue: "lost.response@example.com", ExternalID: lostResponseExternalID, Fields: map[string]string{"LastName": "Retry"},
	})
	require.Equal(t, dex.FlowCompleted, result.status)
	require.Equal(t, StatusUpdatedByExternalID, result.outcome.Status)
	require.Len(t, provider.contactIDsWithExternalID(lostResponseExternalID), 1)
	require.Equal(t, 2, provider.upsertCount(lostResponseExternalID))
}

// Async durability repeats an attempt that outlasts its seven-second local phase.
func TestRecordSyncSlowUpsertSentTwiceKeepsOneRecordWithRealDex(t *testing.T) {
	provider := newSalesforceProvider(t)
	flow, harness := newSalesforceIntegrationHarness(t, provider.URL)
	harness.startWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	result := runRecordSync(t, ctx, harness.client, flow, "slow-upsert-"+strconv.FormatInt(time.Now().UnixNano(), 10), Input{
		MatchValue: "slow.upsert@example.com", ExternalID: slowExternalID, Fields: map[string]string{"LastName": "Slow"},
	})
	require.Equal(t, dex.FlowCompleted, result.status)
	require.Contains(t, []Status{StatusCreated, StatusUpdatedByExternalID}, result.outcome.Status)
	require.Equal(t, []string{result.outcome.RecordID}, provider.contactIDsWithExternalID(slowExternalID))
	require.GreaterOrEqual(t, provider.upsertCount(slowExternalID), 2, "Dex did not repeat the slow attempt, so this test proves nothing")
}

type recordSyncRun struct {
	status  dex.FlowStatus
	outcome Outcome
}

func runRecordSync(t *testing.T, ctx context.Context, client *dex.Client, flow *Flow, flowID string, input Input) recordSyncRun {
	t.Helper()
	_, err := client.StartFlow(ctx, flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	run := recordSyncRun{status: result.Status}
	if result.Status == dex.FlowCompleted {
		require.NoError(t, result.DecodeSingleOutput(&run.outcome))
	}
	return run
}

func readBackText(t *testing.T, outcome Outcome, fieldName string) string {
	t.Helper()
	require.NotNil(t, outcome.Record)
	value, isString := outcome.Record.StringField(fieldName)
	require.True(t, isString, fieldName)
	return value
}

type providerRecord struct {
	key         string
	id          string
	sObjectType string
	fields      map[string]any
}

// salesforceProvider fakes the example's REST subset; its mutex serializes writes like a unique index.
type salesforceProvider struct {
	*httptest.Server
	t                    *testing.T
	mutex                sync.Mutex
	records              []*providerRecord
	upsertCountsByValue  map[string]int
	writes               int
	hasLostFirstResponse bool
}

func newSalesforceProvider(t *testing.T) *salesforceProvider {
	t.Helper()
	provider := &salesforceProvider{t: t, upsertCountsByValue: map[string]int{}}
	for _, seed := range []struct {
		key, sObjectType string
		fields           map[string]any
	}{
		{key: "priya", sObjectType: "Contact", fields: map[string]any{"Email": "priya@meridian.example.com", "LastName": "Raman"}},
		{key: "jane", sObjectType: "Contact", fields: map[string]any{"Email": "jane@acme.example.com", "LastName": "Smith", externalIDField: "ERP-1001"}},
		{key: "jane-near-duplicate", sObjectType: "Contact", fields: map[string]any{"Email": "jane.smith@acme.example.com", "LastName": "Smith-Okafor"}},
		{key: "jordan", sObjectType: "Contact", fields: map[string]any{"Email": "jordan@acme.example.com", "LastName": "Diaz", externalIDField: "ERP-2002"}},
		{key: "sam", sObjectType: "Contact", fields: map[string]any{"Email": "sam@globex.example.com", "LastName": "Lee"}},
		{key: "samuel", sObjectType: "Contact", fields: map[string]any{"Email": "sam@globex.example.com", "LastName": "Lee"}},
		{key: "lead", sObjectType: "Lead", fields: map[string]any{"Email": "new.person@example.com", "LastName": "Person", "Company": "Example"}},
	} {
		provider.addRecord(seed.key, seed.sObjectType, seed.fields)
	}
	provider.Server = httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *salesforceProvider) addRecord(key string, sObjectType string, fields map[string]any) *providerRecord {
	prefix := map[string]string{"Contact": "003", "Lead": "00Q"}[sObjectType]
	record := &providerRecord{key: key, id: fmt.Sprintf("%sRM%010dAAA", prefix, len(provider.records)+1), sObjectType: sObjectType, fields: fields}
	provider.records = append(provider.records, record)
	return record
}

func (provider *salesforceProvider) serveHTTP(response http.ResponseWriter, request *http.Request) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	if request.Header.Get("Authorization") != "Bearer "+integrationToken {
		provider.writeJSON(response, http.StatusUnauthorized, []map[string]any{{"errorCode": "INVALID_SESSION_ID", "message": "Session expired or invalid"}})
		return
	}
	segments := strings.Split(strings.TrimPrefix(request.URL.EscapedPath(), dataPathPrefix+"/sobjects/"), "/")
	switch {
	case request.Method == http.MethodGet && request.URL.Path == dataPathPrefix+"/query":
		provider.query(response, request.URL.Query().Get("q"))
	case request.Method == http.MethodGet && len(segments) == 2:
		provider.getRecord(response, segments[0], segments[1], strings.Split(request.URL.Query().Get("fields"), ","))
	case request.Method == http.MethodPatch && len(segments) == 2:
		provider.updateRecord(response, request, segments[0], segments[1])
	case request.Method == http.MethodPatch && len(segments) == 3:
		value, err := url.PathUnescape(segments[2])
		require.NoError(provider.t, err)
		provider.upsertRecord(response, request, segments[0], segments[1], value)
	default:
		provider.writeJSON(response, http.StatusNotFound, []map[string]any{{"errorCode": "NOT_FOUND", "message": "The requested resource does not exist"}})
	}
}

func (provider *salesforceProvider) query(response http.ResponseWriter, soql string) {
	match := matchQueryPattern.FindStringSubmatch(soql)
	if match == nil {
		provider.t.Errorf("unexpected SOQL %q", soql)
		provider.writeJSON(response, http.StatusBadRequest, []map[string]any{{"errorCode": "MALFORMED_QUERY", "message": "unexpected"}})
		return
	}
	selectedField, sObjectType, matchField := match[1], match[2], match[3]
	matchValue := strings.NewReplacer(`\\`, `\`, `\'`, `'`, `\"`, `"`).Replace(match[4])
	records := []map[string]any{}
	for _, record := range provider.records {
		if record.sObjectType == sObjectType && record.fields[matchField] == matchValue {
			records = append(records, map[string]any{
				"attributes": map[string]any{"type": record.sObjectType}, "Id": record.id, selectedField: record.fields[selectedField],
			})
		}
	}
	provider.writeJSON(response, http.StatusOK, map[string]any{"totalSize": len(records), "done": true, "records": records})
}

func (provider *salesforceProvider) getRecord(response http.ResponseWriter, sObjectType string, id string, fields []string) {
	record := provider.recordByID(sObjectType, id)
	if record == nil {
		provider.writeJSON(response, http.StatusNotFound, []map[string]any{{"errorCode": "NOT_FOUND", "message": "missing"}})
		return
	}
	body := map[string]any{"attributes": map[string]any{"type": record.sObjectType}, "Id": record.id}
	for _, field := range fields {
		body[field] = record.fields[field]
	}
	provider.writeJSON(response, http.StatusOK, body)
}

func (provider *salesforceProvider) updateRecord(response http.ResponseWriter, request *http.Request, sObjectType string, id string) {
	record := provider.recordByID(sObjectType, id)
	if record == nil {
		provider.writeJSON(response, http.StatusNotFound, []map[string]any{{"errorCode": "ENTITY_IS_DELETED", "message": "deleted"}})
		return
	}
	provider.applyFields(record, provider.decodeFields(request))
	response.WriteHeader(http.StatusNoContent)
}

func (provider *salesforceProvider) upsertRecord(response http.ResponseWriter, request *http.Request, sObjectType string, field string, value string) {
	provider.upsertCountsByValue[value]++
	fields := provider.decodeFields(request)
	var matches []*providerRecord
	for _, record := range provider.records {
		if record.sObjectType == sObjectType && record.fields[field] == value {
			matches = append(matches, record)
		}
	}
	switch {
	case len(matches) > 1:
		provider.writeJSON(response, http.StatusMultipleChoices, []string{dataPathPrefix + "/sobjects/" + sObjectType + "/" + matches[0].id})
		return
	case len(matches) == 1:
		provider.applyFields(matches[0], fields)
		provider.writeJSON(response, http.StatusOK, map[string]any{"id": matches[0].id, "success": true, "errors": []any{}, "created": false})
		return
	}
	if lastName, _ := fields["LastName"].(string); lastName == "" {
		provider.writeJSON(response, http.StatusBadRequest, []map[string]any{{"errorCode": "REQUIRED_FIELD_MISSING", "message": "Required fields are missing: [LastName]", "fields": []string{"LastName"}}})
		return
	}
	fields[field] = value
	record := provider.addRecord(value, sObjectType, map[string]any{})
	provider.applyFields(record, fields)
	switch {
	case value == lostResponseExternalID && !provider.hasLostFirstResponse:
		provider.hasLostFirstResponse = true
		provider.writeJSON(response, http.StatusServiceUnavailable, []map[string]any{{"errorCode": "SERVER_UNAVAILABLE", "message": "the response was lost after the write"}})
	case value == slowExternalID:
		time.Sleep(slowUpsertDelay)
		provider.writeJSON(response, http.StatusCreated, map[string]any{"id": record.id, "success": true, "errors": []any{}, "created": true})
	default:
		provider.writeJSON(response, http.StatusCreated, map[string]any{"id": record.id, "success": true, "errors": []any{}, "created": true})
	}
}

func (provider *salesforceProvider) decodeFields(request *http.Request) map[string]any {
	var fields map[string]any
	require.NoError(provider.t, json.NewDecoder(request.Body).Decode(&fields))
	require.NotContains(provider.t, fields, "Id")
	return fields
}

func (provider *salesforceProvider) applyFields(record *providerRecord, fields map[string]any) {
	provider.writes++
	for name, value := range fields {
		record.fields[name] = value
	}
}

func (provider *salesforceProvider) recordByID(sObjectType string, id string) *providerRecord {
	for _, record := range provider.records {
		if record.sObjectType == sObjectType && record.id[:15] == id[:min(len(id), 15)] {
			return record
		}
	}
	return nil
}

func (provider *salesforceProvider) recordByKey(key string) *providerRecord {
	for _, record := range provider.records {
		if record.key == key {
			return record
		}
	}
	provider.t.Fatalf("no seeded record %q", key)
	return nil
}

func (provider *salesforceProvider) recordID(key string) string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.recordByKey(key).id
}

func (provider *salesforceProvider) fieldValue(key string, field string) any {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.recordByKey(key).fields[field]
}

func (provider *salesforceProvider) contactIDsWithExternalID(value string) []string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	var ids []string
	for _, record := range provider.records {
		if record.sObjectType == "Contact" && record.fields[externalIDField] == value {
			ids = append(ids, record.id)
		}
	}
	return ids
}

func (provider *salesforceProvider) upsertCount(value string) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.upsertCountsByValue[value]
}

func (provider *salesforceProvider) writeCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.writes
}

func (provider *salesforceProvider) writeJSON(response http.ResponseWriter, status int, body any) {
	contents, err := json.Marshal(body)
	require.NoError(provider.t, err)
	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("Sforce-Limit-Info", "api-usage=25/15000")
	response.WriteHeader(status)
	_, err = response.Write(contents)
	require.NoError(provider.t, err)
}

type salesforceIntegrationHarness struct {
	registry      *dex.Registry
	cache         *blobcache.Cache
	serverAddress string
	workerAddress string
	worker        *dex.Worker
	workerResult  chan error
	client        *dex.Client
}

func newSalesforceIntegrationHarness(t *testing.T, instanceURL string) (*Flow, *salesforceIntegrationHarness) {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "salesforce", Name: ConnectionName}
	providerClient, err := salesforce.New(salesforce.Config{}, sdkgo.StaticCredentialProvider[salesforce.Credentials]{
		reference: {
			AuthMethodID: salesforce.ProductionOAuthAuthMethodID, AccessToken: sdkgo.NewSecretString(integrationToken), InstanceURL: instanceURL,
		},
	})
	require.NoError(t, err)
	connection, err := salesforce.NewConnection(providerClient, reference)
	require.NoError(t, err)
	flow := NewFlow(connection, sdkgo.ConnectorLoadedConfiguration[SyncConfiguration]{
		Reference: SyncConfigurationRef(), Value: SyncConfiguration{ExternalIDField: externalIDField},
	})
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	workerAddress := net.JoinHostPort("127.0.0.1", availableIntegrationPort(t))
	harness := &salesforceIntegrationHarness{
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

func (harness *salesforceIntegrationHarness) startWorker(t *testing.T) {
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

func (harness *salesforceIntegrationHarness) stopWorker(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, errors.Join(harness.worker.Stop(ctx), <-harness.workerResult))
	harness.worker = nil
	harness.workerResult = nil
}

func availableIntegrationPort(t *testing.T) string {
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
