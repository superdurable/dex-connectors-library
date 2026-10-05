// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package fakesnowflake is a scripted, credential-checking stand-in for the Snowflake SQL API.
//
// It implements POST /api/v2/statements with requestId, retry, and async, GET
// /api/v2/statements/{handle} with partition, and POST /api/v2/statements/{handle}/cancel, using the
// response codes and bodies Snowflake documents. A resubmission with a known requestId and
// retry=true returns the existing statement instead of executing it again. Every error message
// carries the word SENTINEL, so tests can prove that no provider text reaches a Result.
package fakesnowflake

import (
	"bytes"
	"compress/gzip"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Credentials are the credentials the fake accepts.
type Credentials struct {
	// ProgrammaticAccessToken is accepted with the PROGRAMMATIC_ACCESS_TOKEN token type when non-empty.
	ProgrammaticAccessToken string
	// KeyPairPublicKey verifies KEYPAIR_JWT tokens when non-nil.
	KeyPairPublicKey *rsa.PublicKey
	// QualifiedUserName is the ACCOUNT.USER prefix a JWT's iss and sub claims must carry.
	QualifiedUserName string
}

// Column is one scripted resultSetMetaData.rowType entry.
type Column struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Length    int64  `json:"length"`
	Precision int64  `json:"precision"`
	Scale     int64  `json:"scale"`
	Nullable  bool   `json:"nullable"`
}

// StatementFailure is a scripted 422 QueryFailureStatus.
type StatementFailure struct {
	Code     string
	SQLState string
}

// StatementScript decides how one newly submitted statement behaves.
type StatementScript struct {
	// TransientSubmitStatuses answer the first submissions of a requestId, such as 429 or 503, before acceptance.
	TransientSubmitStatuses []int
	// RejectSubmission answers every submission with this 422 failure, such as a compilation error.
	RejectSubmission *StatementFailure
	// FirstSubmitDelay delays the response to the first accepted submission after the statement started.
	FirstSubmitDelay time.Duration
	// RunningReads is the number of status reads answered 202 before the outcome.
	RunningReads int
	// Failure, when set, is the 422 outcome of a finished statement.
	Failure *StatementFailure
	// Columns is the result rowType.
	Columns []Column
	// Partitions holds the rows of each result partition; partition 0 comes first.
	Partitions [][][]*string
}

// SubmittedStatement is one accepted or rejected POST /api/v2/statements request.
type SubmittedStatement struct {
	RequestID  string
	IsRetry    bool
	IsAsync    bool
	TokenType  string
	UserAgent  string
	Statement  string                        `json:"statement"`
	Timeout    int64                         `json:"timeout"`
	Database   string                        `json:"database"`
	Schema     string                        `json:"schema"`
	Warehouse  string                        `json:"warehouse"`
	Role       string                        `json:"role"`
	Bindings   map[string]map[string]*string `json:"bindings"`
	Parameters map[string]string             `json:"parameters"`
}

// Server is a running fake SQL API.
type Server struct {
	*httptest.Server
	t           testing.TB
	credentials Credentials
	script      func(SubmittedStatement) StatementScript
	mutex       sync.Mutex
	submissions []SubmittedStatement
	statements  map[string]*fakeStatement
	handles     map[string]string
	attempts    map[string]int
	executions  int
	cancels     map[string]int
}

type fakeStatement struct {
	script       StatementScript
	runningReads int
	reads        int
	isCanceled   bool
	createdOn    int64
}

// New starts a fake that executes each new statement as script describes.
func New(t testing.TB, credentials Credentials, script func(SubmittedStatement) StatementScript) *Server {
	t.Helper()
	server := &Server{
		t: t, credentials: credentials, script: script,
		statements: map[string]*fakeStatement{}, handles: map[string]string{}, attempts: map[string]int{}, cancels: map[string]int{},
	}
	server.Server = httptest.NewServer(http.HandlerFunc(server.serveHTTP))
	t.Cleanup(server.Close)
	return server
}

// Submissions returns every statement submission received, in order.
func (server *Server) Submissions() []SubmittedStatement {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	return append([]SubmittedStatement(nil), server.submissions...)
}

// ExecutionCount returns how many statements the fake started executing.
func (server *Server) ExecutionCount() int {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	return server.executions
}

// ReadCount returns how many status reads a statement received.
func (server *Server) ReadCount(handle string) int {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	if statement, found := server.statements[handle]; found {
		return statement.reads
	}
	return 0
}

// CancelCount returns how many cancel requests a statement received.
func (server *Server) CancelCount(handle string) int {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	return server.cancels[handle]
}

// HandleForRequestID returns the statement handle a requestId started, or an empty string.
func (server *Server) HandleForRequestID(requestID string) string {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	return server.handles[requestID]
}

func (server *Server) serveHTTP(response http.ResponseWriter, request *http.Request) {
	tokenType, isAuthorized := server.authorize(request)
	if !isAuthorized {
		server.writeJSON(response, http.StatusUnauthorized, map[string]any{"code": "390303", "message": "Invalid token SENTINEL"})
		return
	}
	if request.Header.Get("Accept") != "application/json" || !strings.HasPrefix(request.Header.Get("User-Agent"), "dex-snowflake-connector/") {
		server.writeJSON(response, http.StatusBadRequest, map[string]any{"code": "390142", "message": "Missing headers SENTINEL"})
		return
	}
	path := request.URL.Path
	switch {
	case request.Method == http.MethodPost && path == "/api/v2/statements":
		server.submitStatement(response, request, tokenType)
	case request.Method == http.MethodGet && strings.HasPrefix(path, "/api/v2/statements/") && !strings.Contains(strings.TrimPrefix(path, "/api/v2/statements/"), "/"):
		server.readStatement(response, request, strings.TrimPrefix(path, "/api/v2/statements/"))
	case request.Method == http.MethodPost && strings.HasPrefix(path, "/api/v2/statements/") && strings.HasSuffix(path, "/cancel"):
		server.cancelStatement(response, strings.TrimSuffix(strings.TrimPrefix(path, "/api/v2/statements/"), "/cancel"))
	default:
		server.writeJSON(response, http.StatusNotFound, map[string]any{"code": "390404", "message": "Not found SENTINEL"})
	}
}

func (server *Server) submitStatement(response http.ResponseWriter, request *http.Request, tokenType string) {
	if request.Header.Get("Content-Type") != "application/json" {
		server.writeJSON(response, http.StatusUnsupportedMediaType, map[string]any{"code": "390415", "message": "Bad content type SENTINEL"})
		return
	}
	var submitted SubmittedStatement
	if err := json.NewDecoder(request.Body).Decode(&submitted); err != nil {
		server.writeJSON(response, http.StatusBadRequest, map[string]any{"code": "390142", "message": "Invalid payload SENTINEL"})
		return
	}
	query := request.URL.Query()
	submitted.RequestID, submitted.IsRetry, submitted.IsAsync = query.Get("requestId"), query.Get("retry") == "true", query.Get("async") == "true"
	submitted.TokenType, submitted.UserAgent = tokenType, request.Header.Get("User-Agent")

	server.mutex.Lock()
	server.submissions = append(server.submissions, submitted)
	if handle, isKnown := server.handles[submitted.RequestID]; isKnown && submitted.IsRetry && submitted.RequestID != "" {
		statement := server.statements[handle]
		server.mutex.Unlock()
		server.writeJSON(response, http.StatusAccepted, statusBody("333334", "", handle, statement.createdOn))
		return
	}
	script := server.script(submitted)
	attempt := server.attempts[submitted.RequestID]
	server.attempts[submitted.RequestID] = attempt + 1
	if attempt < len(script.TransientSubmitStatuses) {
		server.mutex.Unlock()
		server.writeJSON(response, script.TransientSubmitStatuses[attempt], map[string]any{"code": "390505", "message": "Too many requests SENTINEL"})
		return
	}
	if script.RejectSubmission != nil {
		server.mutex.Unlock()
		server.writeJSON(response, http.StatusUnprocessableEntity, failureBody(*script.RejectSubmission, newStatementHandle(900000+len(server.submissions))))
		return
	}
	server.executions++
	handle := newStatementHandle(server.executions)
	statement := &fakeStatement{script: script, runningReads: script.RunningReads, createdOn: time.Now().UnixMilli()}
	server.statements[handle] = statement
	if submitted.RequestID != "" {
		server.handles[submitted.RequestID] = handle
	}
	server.mutex.Unlock()
	if script.FirstSubmitDelay > 0 {
		// Snowflake already accepted the statement; only the response is slow.
		time.Sleep(script.FirstSubmitDelay)
	}
	server.writeJSON(response, http.StatusAccepted, statusBody("333334", "", handle, statement.createdOn))
}

func (server *Server) readStatement(response http.ResponseWriter, request *http.Request, handle string) {
	server.mutex.Lock()
	statement, found := server.statements[handle]
	if !found {
		server.mutex.Unlock()
		server.writeJSON(response, http.StatusUnprocessableEntity, failureBody(StatementFailure{Code: "000709", SQLState: "02000"}, handle))
		return
	}
	statement.reads++
	isCanceled, isRunning := statement.isCanceled, statement.runningReads > 0
	if isRunning && !isCanceled {
		statement.runningReads--
	}
	script := statement.script
	server.mutex.Unlock()
	switch {
	case isCanceled:
		server.writeJSON(response, http.StatusUnprocessableEntity, failureBody(StatementFailure{Code: "000604", SQLState: "57014"}, handle))
	case isRunning:
		server.writeJSON(response, http.StatusAccepted, statusBody("333334", "", handle, statement.createdOn))
	case script.Failure != nil:
		server.writeJSON(response, http.StatusUnprocessableEntity, failureBody(*script.Failure, handle))
	default:
		server.writeResultPartition(response, request, handle, statement)
	}
}

func (server *Server) writeResultPartition(response http.ResponseWriter, request *http.Request, handle string, statement *fakeStatement) {
	script := statement.script
	partition := 0
	if value := request.URL.Query().Get("partition"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 0 || parsed >= max(len(script.Partitions), 1) {
			server.writeJSON(response, http.StatusUnprocessableEntity, failureBody(StatementFailure{Code: "000709", SQLState: "02000"}, handle))
			return
		}
		partition = parsed
	}
	rowsOf := func(index int) [][]*string {
		if index < len(script.Partitions) {
			return script.Partitions[index]
		}
		return [][]*string{}
	}
	if partition > 0 {
		// Later partitions carry only data and are gzip-compressed, as Snowflake documents.
		var compressed bytes.Buffer
		writer := gzip.NewWriter(&compressed)
		if err := json.NewEncoder(writer).Encode(map[string]any{"data": rowsOf(partition)}); err != nil || writer.Close() != nil {
			server.t.Errorf("fake Snowflake could not compress partition %d", partition)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		response.Header().Set("Content-Encoding", "gzip")
		response.WriteHeader(http.StatusOK)
		if _, err := response.Write(compressed.Bytes()); err != nil {
			server.t.Logf("fake Snowflake response write failed: %v", err)
		}
		return
	}
	totalRows := 0
	partitionInfo := make([]map[string]any, 0, len(script.Partitions))
	for _, rows := range script.Partitions {
		totalRows += len(rows)
		partitionInfo = append(partitionInfo, map[string]any{"rowCount": len(rows), "uncompressedSize": 1024})
	}
	body := statusBody("090001", "00000", handle, statement.createdOn)
	body["message"] = "successfully executed SENTINEL"
	body["resultSetMetaData"] = map[string]any{
		"numRows": totalRows, "format": "jsonv2", "rowType": script.Columns, "partitionInfo": partitionInfo,
	}
	body["data"] = rowsOf(0)
	server.writeJSON(response, http.StatusOK, body)
}

// cancelStatement answers 422 000709 for a finished statement, an assumption: Snowflake does not document it.
func (server *Server) cancelStatement(response http.ResponseWriter, handle string) {
	server.mutex.Lock()
	server.cancels[handle]++
	statement, found := server.statements[handle]
	isRunning := found && statement.runningReads > 0 && !statement.isCanceled
	if isRunning {
		statement.isCanceled = true
	}
	server.mutex.Unlock()
	if !isRunning && !(found && statement.isCanceled) {
		server.writeJSON(response, http.StatusUnprocessableEntity, failureBody(StatementFailure{Code: "000709", SQLState: "02000"}, handle))
		return
	}
	body := statusBody("000604", "57014", handle, 0)
	body["message"] = "SQL execution canceled SENTINEL"
	server.writeJSON(response, http.StatusOK, body)
}

// authorize returns the token type of a valid credential.
func (server *Server) authorize(request *http.Request) (string, bool) {
	token, hasBearer := strings.CutPrefix(request.Header.Get("Authorization"), "Bearer ")
	tokenType := request.Header.Get("X-Snowflake-Authorization-Token-Type")
	switch {
	case !hasBearer || token == "":
		return "", false
	case tokenType == "PROGRAMMATIC_ACCESS_TOKEN":
		return tokenType, server.credentials.ProgrammaticAccessToken != "" && token == server.credentials.ProgrammaticAccessToken
	case tokenType == "KEYPAIR_JWT":
		return tokenType, server.isValidKeyPairJWT(token)
	default:
		return "", false
	}
}

func (server *Server) isValidKeyPairJWT(token string) bool {
	publicKey := server.credentials.KeyPairPublicKey
	parts := strings.Split(token, ".")
	if publicKey == nil || len(parts) != 3 {
		return false
	}
	header, headerErr := base64.RawURLEncoding.DecodeString(parts[0])
	claimsText, claimsErr := base64.RawURLEncoding.DecodeString(parts[1])
	signature, signatureErr := base64.RawURLEncoding.DecodeString(parts[2])
	if headerErr != nil || claimsErr != nil || signatureErr != nil {
		return false
	}
	var headerFields struct {
		Algorithm string `json:"alg"`
	}
	var claims struct {
		Issuer   string `json:"iss"`
		Subject  string `json:"sub"`
		IssuedAt int64  `json:"iat"`
		Expires  int64  `json:"exp"`
	}
	if json.Unmarshal(header, &headerFields) != nil || headerFields.Algorithm != "RS256" || json.Unmarshal(claimsText, &claims) != nil {
		return false
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, digest[:], signature) != nil {
		return false
	}
	encodedPublicKey, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return false
	}
	fingerprint := sha256.Sum256(encodedPublicKey)
	expectedIssuer := server.credentials.QualifiedUserName + ".SHA256:" + base64.StdEncoding.EncodeToString(fingerprint[:])
	lifetime := claims.Expires - claims.IssuedAt
	return claims.Issuer == expectedIssuer && claims.Subject == server.credentials.QualifiedUserName &&
		lifetime > 0 && lifetime <= int64(time.Hour/time.Second)
}

func (server *Server) writeJSON(response http.ResponseWriter, status int, body any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	if err := json.NewEncoder(response).Encode(body); err != nil {
		server.t.Logf("fake Snowflake response write failed: %v", err)
	}
}

func statusBody(code string, sqlState string, handle string, createdOn int64) map[string]any {
	body := map[string]any{
		"code": code, "message": "Asynchronous execution in progress. SENTINEL",
		"statementHandle": handle, "statementStatusUrl": "/api/v2/statements/" + handle,
	}
	if sqlState != "" {
		body["sqlState"] = sqlState
	}
	if createdOn > 0 {
		body["createdOn"] = createdOn
	}
	return body
}

func failureBody(failure StatementFailure, handle string) map[string]any {
	return map[string]any{
		"code": failure.Code, "sqlState": failure.SQLState, "statementHandle": handle,
		"message": "SQL compilation error: SENTINEL customer-card-4242",
	}
}

func newStatementHandle(sequence int) string {
	return fmt.Sprintf("01b%05x-0000-4000-8000-%012x", sequence, sequence)
}

// Text returns a pointer to value, for scripting a non-null cell.
func Text(value string) *string { return &value }
