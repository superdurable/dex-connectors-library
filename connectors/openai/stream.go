// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package openai

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

type streamEvent struct {
	Type           string       `json:"type"`
	SequenceNumber *int64       `json:"sequence_number,omitempty"`
	Delta          string       `json:"delta,omitempty"`
	Response       wireResponse `json:"response"`
}

type sseEventHandler func(streamEvent) (bool, sdkgo.MutationAttempt[Response])

// streamLimitError reports a stream or event over its configured size limit.
type streamLimitError struct{ message string }

// Error returns the safe limit message.
func (err streamLimitError) Error() string { return err.message }

type responseStreamHandler struct {
	operation  CreateResponseOperation
	call       sdkgo.Call
	heartbeat  *createResponseHeartbeat
	header     http.Header
	requestID  string
	responseID string
	// isDeliveryStopped ends Stream writes after one fails; reading continues, because closing the connection cancels the response.
	isDeliveryStopped bool
	// hasStreamError records an error event, which does not prove that no response exists.
	hasStreamError bool
}

func (operation CreateResponseOperation) readStream(call sdkgo.Call, heartbeat *createResponseHeartbeat, body io.Reader, header http.Header, requestID string) sdkgo.MutationAttempt[Response] {
	handler := &responseStreamHandler{operation: operation, call: call, heartbeat: heartbeat, header: header, requestID: requestID}
	attempt, err := readSSE(body, operation.client.maxResponseBytes, operation.client.maxSSEEventBytes, handler.handle)
	if err == nil {
		return attempt
	}
	if handler.responseID == "" {
		message := err.Error()
		if handler.hasStreamError {
			message = "provider streamed an error before naming a response, which may exist"
		}
		return sdkgo.NewMutationUncertain(Response{}, openAIFailure(sdkgo.FailureProtocol, "createResponse", message), handler.receipt())
	}
	if errors.As(err, &streamLimitError{}) {
		// Closing the connection now would cancel the response, so let it finish before reading it back.
		_, _ = io.Copy(io.Discard, body) // A failed drain leaves the outcome to the read-back.
	}
	return operation.readBackResponse(call, heartbeat, handler.responseID, header, requestID)
}

func (handler *responseStreamHandler) handle(event streamEvent) (bool, sdkgo.MutationAttempt[Response]) {
	if event.Response.ID != "" && handler.responseID == "" {
		if !isSafeResponseID(event.Response.ID) {
			return true, sdkgo.NewMutationUncertain(Response{}, openAIFailure(sdkgo.FailureProtocol, "createResponse", "provider response ID is invalid"), handler.receipt())
		}
		handler.responseID = event.Response.ID
		// A lost checkpoint makes a later attempt select uncertain; the ticker records it again meanwhile.
		_ = handler.heartbeat.recordResponseID(handler.responseID)
	}
	switch event.Type {
	case "response.created", "response.queued", "response.in_progress":
		handler.deliver(func() error {
			return handler.call.ReportProgress(sdkgo.Progress{Phase: event.Type, Message: event.Response.Status, ProviderSequence: event.SequenceNumber})
		})
	case "response.output_text.delta":
		handler.deliver(func() error { return handler.call.WriteText(event.Delta) })
	case "response.completed", "response.failed", "response.incomplete":
		return handler.resolveTerminalEvent(event)
	case "error":
		// Reading on keeps the connection open and may still reveal the response ID.
		handler.hasStreamError = true
	}
	return false, sdkgo.MutationAttempt[Response]{}
}

// deliver writes best-effort Stream data until one write fails.
func (handler *responseStreamHandler) deliver(write func() error) {
	if handler.isDeliveryStopped {
		return
	}
	if err := write(); err != nil {
		handler.isDeliveryStopped = true
	}
}

// resolveTerminalEvent ends the stream only at a terminal event naming the response this stream created.
func (handler *responseStreamHandler) resolveTerminalEvent(event streamEvent) (bool, sdkgo.MutationAttempt[Response]) {
	result := convertResponse(event.Response)
	if result.ID == "" || result.ID != handler.responseID {
		// Closing the connection now could cancel this stream's response, so read on.
		return false, sdkgo.MutationAttempt[Response]{}
	}
	receipt := responseReceipt(handler.call, handler.requestID, handler.header, result.ID)
	if event.Type == "response.completed" {
		return true, sdkgo.NewMutationBranch(CreateResponseBranchCompleted, result, nil, receipt)
	}
	failure := openAIFailure(sdkgo.FailureProviderRejection, "createResponse", "provider returned "+event.Type)
	return true, sdkgo.NewMutationBranch(CreateResponseBranchFailed, result, &failure, receipt)
}

func (handler *responseStreamHandler) receipt() sdkgo.Receipt {
	return responseReceipt(handler.call, handler.requestID, handler.header, handler.responseID)
}

func readSSE(body io.Reader, maxTotalBytes int64, maxEventBytes int, handler sseEventHandler) (sdkgo.MutationAttempt[Response], error) {
	reader := bufio.NewReader(body)
	var data strings.Builder
	var totalBytes int64
	for {
		line, err := reader.ReadString('\n')
		totalBytes += int64(len(line))
		if totalBytes > maxTotalBytes {
			return sdkgo.MutationAttempt[Response]{}, streamLimitError{message: "stream exceeds the configured size limit"}
		}
		if len(line)+data.Len() > maxEventBytes {
			return sdkgo.MutationAttempt[Response]{}, streamLimitError{message: "stream event exceeds the configured size limit"}
		}
		trimmed := strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if strings.HasPrefix(trimmed, "data:") {
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimSpace(strings.TrimPrefix(trimmed, "data:")))
		}
		if trimmed == "" && data.Len() > 0 {
			var event streamEvent
			// A malformed event is skipped, so a later terminal event can still decide the outcome.
			isDecoded := json.Unmarshal([]byte(data.String()), &event) == nil
			data.Reset()
			if isDecoded {
				if terminal, attempt := handler(event); terminal {
					return attempt, nil
				}
			}
		}
		if err != nil {
			if err == io.EOF {
				return sdkgo.MutationAttempt[Response]{}, fmt.Errorf("stream ended before a terminal event")
			}
			return sdkgo.MutationAttempt[Response]{}, fmt.Errorf("stream could not be read")
		}
	}
}
