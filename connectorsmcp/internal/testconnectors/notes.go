// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package testconnectors

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/connectorsmcp/internal/registry"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
	"github.com/superdurable/dex/sdk-go/dex"
)

// Identity of the notes connector, a fake built the way generated code is.
const (
	NotesConnectorID   = "example-notes"
	NotesDisplayName   = "Example Notes"
	NotesModulePath    = "example.com/connectors/example-notes/go"
	NotesModuleVersion = "v0.3.0"
	notesProvider      = "example-notes"
	// postNoteExecuteTimeout leaves the Fence a request window of seconds.
	postNoteExecuteTimeout = 30 * time.Second
)

// NotesConfig is the connection configuration in connections.json.
type NotesConfig struct {
	// BaseURL is the fake provider's URL.
	BaseURL string `json:"baseUrl"`
}

// NotesCredentials is the connection credential in connections.json.
type NotesCredentials struct {
	// Token is the provider bearer token.
	Token string `json:"token"`
}

// GetNoteInput is the getNote input.
type GetNoteInput struct {
	// NoteID names the note.
	NoteID string `json:"noteId"`
}

// Note is the getNote result.
type Note struct {
	// ID names the note.
	ID string `json:"id"`
	// Text is the note text.
	Text string `json:"text"`
}

// PostNoteInput is the postNote input.
type PostNoteInput struct {
	// Text is the note text.
	Text string `json:"text"`
}

// PostedNote is the postNote result.
type PostedNote struct {
	// NoteID names the created note.
	NoteID string `json:"noteId"`
}

// SaveRecordInput is the saveRecord input.
type SaveRecordInput struct {
	// Name names the record.
	Name string `json:"name"`
	// Value is stored under the name.
	Value string `json:"value"`
}

// SavedRecord is the saveRecord result.
type SavedRecord struct {
	// Value is the stored value.
	Value string `json:"value"`
}

var getNoteDefinition = sdkgo.QueryDefinition{
	Operation: sdkgo.OperationRef{ConnectorID: NotesConnectorID, OperationID: "getNote"},
	Branches: []sdkgo.BranchDefinition{
		{ID: "found", Description: "The note was found."},
		{ID: "notFound", Description: "No note has this ID.", Optional: true},
		{ID: "unavailable", Description: "The note store refused the read.", Optional: true},
		{ID: sdkgo.DefectBranchID, Description: "The call or the response was invalid.", Optional: true},
	},
	StepDefaults: sdkgo.StepDefaults{ExecuteMethodTimeout: 10 * time.Second},
}

var postNoteDefinition = sdkgo.MutationDefinition{
	Operation: sdkgo.OperationRef{ConnectorID: NotesConnectorID, OperationID: "postNote"},
	Branches: []sdkgo.BranchDefinition{
		{ID: "posted", Description: "The note was created."},
		{ID: "invalidText", Description: "The text is empty; nothing was sent.", Optional: true},
		{ID: sdkgo.UncertainBranchID, Description: "The note may have been created.", Optional: true},
		{ID: sdkgo.DefectBranchID, Description: "The call or the response was invalid.", Optional: true},
	},
	StepDefaults: sdkgo.StepDefaults{ExecuteMethodTimeout: postNoteExecuteTimeout, ExecuteDurability: dex.StepDurabilitySync},
}

var saveRecordDefinition = sdkgo.MutationDefinition{
	Operation: sdkgo.OperationRef{ConnectorID: NotesConnectorID, OperationID: "saveRecord"},
	Branches: []sdkgo.BranchDefinition{
		{ID: "saved", Description: "The record holds the value."},
		{ID: "providerRejected", Description: "The provider refused the record.", Optional: true},
		{ID: sdkgo.DefectBranchID, Description: "The call or the response was invalid.", Optional: true},
	},
	StepDefaults: sdkgo.StepDefaults{ExecuteMethodTimeout: 10 * time.Second},
}

var deleteNoteDefinition = sdkgo.MutationDefinition{
	Operation: sdkgo.OperationRef{ConnectorID: NotesConnectorID, OperationID: "deleteNote"},
	Branches: []sdkgo.BranchDefinition{
		{ID: "deleted", Description: "The note was deleted."},
		{ID: sdkgo.DefectBranchID, Description: "The call was invalid.", Optional: true},
	},
}

// NotesExposedAgentTools is what generated ExposedAgentTools would return:
// getNote, postNote, and saveRecord. deleteNote is not exposed.
func NotesExposedAgentTools() []sdkgo.AgentToolMetadata {
	return []sdkgo.AgentToolMetadata{
		{
			Operation: getNoteDefinition.Operation, Kind: sdkgo.AgentOperationKindQuery,
			ToolName: NotesConnectorID + "_get_note", Description: "Read one note by its ID.",
			BranchOutcomes: map[sdkgo.BranchID]sdkgo.AgentBranchOutcome{"notFound": sdkgo.AgentBranchOutcomeRejected},
			InputType:      "GetNoteInput",
			InputSchema:    json.RawMessage(`{"type":"object","additionalProperties":false,"required":["noteId"],"properties":{"noteId":{"type":"string","minLength":1}}}`),
			Branches:       getNoteDefinition.Branches, StepDefaults: getNoteDefinition.StepDefaults,
		},
		{
			Operation: postNoteDefinition.Operation, Kind: sdkgo.AgentOperationKindMutation,
			ToolName: NotesConnectorID + "_post_note", Description: "Create a note. Repeating the call creates another note.",
			IsApprovalRecommended: true,
			BranchOutcomes:        map[sdkgo.BranchID]sdkgo.AgentBranchOutcome{"invalidText": sdkgo.AgentBranchOutcomeRejected},
			InputType:             "PostNoteInput",
			InputSchema:           json.RawMessage(`{"type":"object","additionalProperties":false,"required":["text"],"properties":{"text":{"type":"string"}}}`),
			Branches:              postNoteDefinition.Branches, StepDefaults: postNoteDefinition.StepDefaults,
		},
		{
			Operation: saveRecordDefinition.Operation, Kind: sdkgo.AgentOperationKindMutation,
			ToolName: NotesConnectorID + "_save_record", Description: "Store a value under a record name.",
			IsRetrySafe:    true,
			BranchOutcomes: map[sdkgo.BranchID]sdkgo.AgentBranchOutcome{"providerRejected": sdkgo.AgentBranchOutcomeRejected},
			InputType:      "SaveRecordInput",
			InputSchema:    json.RawMessage(`{"type":"object","additionalProperties":false,"required":["name","value"],"properties":{"name":{"type":"string"},"value":{"type":"string"}}}`),
			Branches:       saveRecordDefinition.Branches, StepDefaults: saveRecordDefinition.StepDefaults,
		},
	}
}

// NotesConnector returns the notes connector as the release registry would
// link it. Its connections read baseUrl from connections.json and resolve the
// token through the project's credential store on every call.
func NotesConnector() registry.LinkedConnector {
	return registry.LinkedConnector{
		ConnectorID:       NotesConnectorID,
		DisplayName:       NotesDisplayName,
		ModulePath:        NotesModulePath,
		ModuleVersion:     NotesModuleVersion,
		ExposedAgentTools: NotesExposedAgentTools(),
		OpenAgentTools:    openNotesAgentTools,
	}
}

func openNotesAgentTools(project *projectconfig.LoadedProject, connectionName string) ([]sdkgo.AgentTool, error) {
	key := projectconfig.ConnectionKey{ConnectorID: NotesConnectorID, ConnectionName: connectionName}
	var config NotesConfig
	if err := project.Configuration.DecodeConnectionConfiguration(key, &config); err != nil {
		return nil, fmt.Errorf("example-notes connection %q settings: %w", connectionName, err)
	}
	credentials, err := provider.NewCredentialProvider(project.Connections, key, decodeNotesCredentials)
	if err != nil {
		return nil, err
	}
	client := &notesClient{baseURL: strings.TrimRight(config.BaseURL, "/"), credentials: credentials, http: &http.Client{}}
	reference := sdkgo.ConnectionRef{Provider: notesProvider, Name: connectionName}
	metadata := NotesExposedAgentTools()
	getNote, err := sdkgo.NewQueryAgentTool[GetNoteInput, Note](getNoteQuery{client: client}, reference, metadata[0])
	if err != nil {
		return nil, err
	}
	postNote, err := sdkgo.NewMutationAgentTool[PostNoteInput, PostedNote](postNoteMutation{client: client}, reference, metadata[1])
	if err != nil {
		return nil, err
	}
	saveRecord, err := sdkgo.NewMutationAgentTool[SaveRecordInput, SavedRecord](saveRecordMutation{client: client}, reference, metadata[2])
	if err != nil {
		return nil, err
	}
	// A connector that returned an unexposed tool must still never be offered it.
	deleteNote, err := sdkgo.NewMutationAgentTool[GetNoteInput, Note](deleteNoteMutation{}, reference, sdkgo.AgentToolMetadata{
		Operation: deleteNoteDefinition.Operation, Kind: sdkgo.AgentOperationKindMutation,
		ToolName: NotesConnectorID + "_delete_note", Description: "Delete a note.",
		InputSchema: json.RawMessage(`{"type":"object"}`), Branches: deleteNoteDefinition.Branches,
	})
	if err != nil {
		return nil, err
	}
	return []sdkgo.AgentTool{getNote, postNote, saveRecord, deleteNote}, nil
}

func decodeNotesCredentials(contents json.RawMessage) (NotesCredentials, error) {
	var credentials NotesCredentials
	if err := projectconfig.DecodeCredentials(contents, &credentials); err != nil {
		return NotesCredentials{}, err
	}
	if credentials.Token == "" {
		return NotesCredentials{}, errors.New("example-notes token is required")
	}
	return credentials, nil
}

type notesClient struct {
	baseURL     string
	credentials sdkgo.CredentialProvider[NotesCredentials]
	http        *http.Client
}

// providerAnswer is one HTTP answer of the fake provider.
type providerAnswer struct {
	status     int
	body       []byte
	retryAfter time.Duration
}

func (client *notesClient) send(ctx context.Context, call sdkgo.Call, method string, path string, body any, idempotencyKey string) (providerAnswer, error) {
	token, err := client.token(call)
	if err != nil {
		return providerAnswer{}, err
	}
	return client.do(ctx, method, path, body, token, idempotencyKey)
}

func (client *notesClient) token(call sdkgo.Call) (string, error) {
	credentials, err := client.credentials.Resolve(call)
	if err != nil {
		return "", err
	}
	return credentials.Token, nil
}

func (client *notesClient) do(ctx context.Context, method string, path string, body any, token string, idempotencyKey string) (providerAnswer, error) {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return providerAnswer{}, err
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, client.baseURL+path, reader)
	if err != nil {
		return providerAnswer{}, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	if idempotencyKey != "" {
		request.Header.Set("Idempotency-Key", idempotencyKey)
	}
	response, err := client.http.Do(request)
	if err != nil {
		return providerAnswer{}, err
	}
	defer response.Body.Close()
	contents, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return providerAnswer{}, err
	}
	answer := providerAnswer{status: response.StatusCode, body: contents}
	if seconds, err := strconv.Atoi(response.Header.Get("Retry-After")); err == nil {
		answer.retryAfter = time.Duration(seconds) * time.Second
	}
	return answer, nil
}

func notesFailure(kind sdkgo.FailureKind, operationID string, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: notesProvider, Operation: operationID, Message: message}
}

type getNoteQuery struct{ client *notesClient }

func (getNoteQuery) Definition() sdkgo.QueryDefinition { return getNoteDefinition }

func (query getNoteQuery) Invoke(call sdkgo.Call, input GetNoteInput) sdkgo.QueryAttempt[Note] {
	answer, err := query.client.send(call.Context, call, http.MethodGet, "/notes/"+input.NoteID, nil, "")
	if err != nil {
		return sdkgo.NewQueryRetry[Note](notesFailure(sdkgo.FailureTransport, "getNote", "the note store is unreachable"), 0)
	}
	switch {
	case answer.status == http.StatusOK:
		var note Note
		if json.Unmarshal(answer.body, &note) != nil {
			failure := notesFailure(sdkgo.FailureProtocol, "getNote", "the note store answered with an invalid note")
			return sdkgo.NewQueryBranch(sdkgo.DefectBranchID, Note{}, &failure, sdkgo.Receipt{})
		}
		return sdkgo.NewQueryBranch("found", note, nil, sdkgo.Receipt{})
	case answer.status == http.StatusNotFound:
		failure := notesFailure(sdkgo.FailureNotFound, "getNote", "no note has this ID")
		return sdkgo.NewQueryBranch("notFound", Note{}, &failure, sdkgo.Receipt{})
	case answer.status == http.StatusConflict:
		failure := notesFailure(sdkgo.FailureConflict, "getNote", "the note store refused the read")
		return sdkgo.NewQueryBranch("unavailable", Note{}, &failure, sdkgo.Receipt{})
	case answer.status == http.StatusTooManyRequests:
		return sdkgo.NewQueryRetry[Note](notesFailure(sdkgo.FailureRateLimit, "getNote", "the note store is rate limiting"), answer.retryAfter)
	case answer.status >= 500:
		return sdkgo.NewQueryRetry[Note](notesFailure(sdkgo.FailureAvailability, "getNote", "the note store is unavailable"), answer.retryAfter)
	default:
		failure := notesFailure(sdkgo.FailureProtocol, "getNote", "the note store answered with an unexpected status")
		return sdkgo.NewQueryBranch(sdkgo.DefectBranchID, Note{}, &failure, sdkgo.Receipt{})
	}
}

type postNoteMutation struct{ client *notesClient }

func (postNoteMutation) Definition() sdkgo.MutationDefinition { return postNoteDefinition }

func (postNoteMutation) IdempotencyKey(id sdkgo.CallID, _ PostNoteInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// Invoke sends at most one request per Call ID through the single-dispatch Fence.
func (mutation postNoteMutation) Invoke(call sdkgo.Call, input PostNoteInput) sdkgo.MutationAttempt[PostedNote] {
	if strings.TrimSpace(input.Text) == "" {
		failure := notesFailure(sdkgo.FailureValidation, "postNote", "the note text is empty")
		return sdkgo.NewMutationBranch("invalidText", PostedNote{}, &failure, sdkgo.Receipt{})
	}
	fence, err := singledispatch.NewFence(call, postNoteExecuteTimeout)
	if err != nil {
		failure := notesFailure(sdkgo.FailureLocalDefect, "postNote", "the single-dispatch fence is unavailable")
		return sdkgo.NewMutationBranch(sdkgo.DefectBranchID, PostedNote{}, &failure, sdkgo.Receipt{})
	}
	if decision := fence.Decide(); decision.Kind != singledispatch.SendAllowed {
		return sdkgo.NewMutationUncertain(PostedNote{}, notesFailure(sdkgo.FailureTransport, "postNote", "an earlier attempt may have created the note"), sdkgo.Receipt{})
	}
	token, err := mutation.client.token(call)
	if err != nil {
		if recordErr := fence.RecordNotSent(); recordErr != nil {
			return sdkgo.NewMutationUncertain(PostedNote{}, notesFailure(sdkgo.FailureAuthentication, "postNote", "the credential is unavailable"), sdkgo.Receipt{})
		}
		return sdkgo.NewMutationRetry[PostedNote](notesFailure(sdkgo.FailureAuthentication, "postNote", "the credential is unavailable"), 0)
	}
	var answer providerAnswer
	err = fence.Dispatch(func(requestContext context.Context) error {
		var sendErr error
		answer, sendErr = mutation.client.do(requestContext, http.MethodPost, "/notes", map[string]string{"text": input.Text}, token, "")
		return sendErr
	})
	switch {
	case errors.Is(err, singledispatch.ErrCheckpoint), errors.Is(err, singledispatch.ErrInsufficientTime), errors.Is(err, singledispatch.ErrDispatchNotAllowed):
		return sdkgo.NewMutationRetry[PostedNote](notesFailure(sdkgo.FailureAvailability, "postNote", "the note was not sent"), 0)
	case err != nil:
		return sdkgo.NewMutationUncertain(PostedNote{}, notesFailure(sdkgo.FailureTransport, "postNote", "the note store did not answer"), sdkgo.Receipt{})
	}
	switch {
	case answer.status == http.StatusOK:
		var created struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(answer.body, &created) != nil {
			return sdkgo.NewMutationUncertain(PostedNote{}, notesFailure(sdkgo.FailureProtocol, "postNote", "the note store answered with an invalid note"), sdkgo.Receipt{})
		}
		return sdkgo.NewMutationBranch("posted", PostedNote{NoteID: created.ID}, nil, sdkgo.Receipt{ProviderObjectID: created.ID})
	case answer.status == http.StatusTooManyRequests || answer.status == http.StatusServiceUnavailable:
		if recordErr := fence.RecordCanResend(); recordErr != nil {
			return sdkgo.NewMutationUncertain(PostedNote{}, notesFailure(sdkgo.FailureAvailability, "postNote", "the note store asked to retry"), sdkgo.Receipt{})
		}
		return sdkgo.NewMutationRetry[PostedNote](notesFailure(sdkgo.FailureRateLimit, "postNote", "the note store asked to retry"), answer.retryAfter)
	default:
		return sdkgo.NewMutationUncertain(PostedNote{}, notesFailure(sdkgo.FailureProviderRejection, "postNote", "the note store answered with an unexpected status"), sdkgo.Receipt{})
	}
}

type saveRecordMutation struct{ client *notesClient }

func (saveRecordMutation) Definition() sdkgo.MutationDefinition { return saveRecordDefinition }

func (saveRecordMutation) IdempotencyKey(id sdkgo.CallID, _ SaveRecordInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// Invoke reads back a record an earlier attempt may have saved, then saves with
// the provider idempotency key, so repeating it never applies a second write.
func (mutation saveRecordMutation) Invoke(call sdkgo.Call, input SaveRecordInput) sdkgo.MutationAttempt[SavedRecord] {
	retryFailure := notesFailure(sdkgo.FailureAvailability, "saveRecord", "the record store is unavailable")
	if call.Context.Attempt() > 1 {
		answer, err := mutation.client.send(call.Context, call, http.MethodGet, "/records/"+string(call.IdempotencyKey), nil, "")
		if err != nil {
			return sdkgo.NewMutationRetry[SavedRecord](retryFailure, 0)
		}
		if answer.status == http.StatusOK {
			var saved SavedRecord
			if json.Unmarshal(answer.body, &saved) == nil {
				return sdkgo.NewMutationBranch("saved", saved, nil, sdkgo.Receipt{})
			}
		}
	}
	answer, err := mutation.client.send(call.Context, call, http.MethodPut, "/records", input, string(call.IdempotencyKey))
	if err != nil {
		return sdkgo.NewMutationRetry[SavedRecord](retryFailure, 0)
	}
	switch {
	case answer.status == http.StatusOK:
		var saved SavedRecord
		if json.Unmarshal(answer.body, &saved) != nil {
			return sdkgo.NewMutationRetry[SavedRecord](retryFailure, 0)
		}
		return sdkgo.NewMutationBranch("saved", saved, nil, sdkgo.Receipt{})
	case answer.status == http.StatusBadRequest:
		failure := notesFailure(sdkgo.FailureProviderRejection, "saveRecord", "the record store refused the record")
		return sdkgo.NewMutationBranch("providerRejected", SavedRecord{}, &failure, sdkgo.Receipt{})
	default:
		return sdkgo.NewMutationRetry[SavedRecord](retryFailure, answer.retryAfter)
	}
}

type deleteNoteMutation struct{}

func (deleteNoteMutation) Definition() sdkgo.MutationDefinition { return deleteNoteDefinition }

func (deleteNoteMutation) IdempotencyKey(id sdkgo.CallID, _ GetNoteInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

func (deleteNoteMutation) Invoke(sdkgo.Call, GetNoteInput) sdkgo.MutationAttempt[Note] {
	panic("deleteNote is not exposed and must never run")
}
