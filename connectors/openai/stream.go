// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package openai

import (
	"bufio"
	"encoding/json"
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

func (operation CreateResponseOperation) readStream(call sdkgo.Call, body io.Reader, header http.Header, requestID string) sdkgo.MutationAttempt[Response] {
	responseID := ""
	receipt := func() sdkgo.Receipt { return responseReceipt(call, requestID, header, responseID) }
	handler := func(event streamEvent) (bool, sdkgo.MutationAttempt[Response]) {
		if event.Response.ID != "" {
			responseID = event.Response.ID
		}
		switch event.Type {
		case "response.created", "response.queued", "response.in_progress":
			if err := call.ReportProgress(sdkgo.Progress{
				Phase: event.Type, Message: event.Response.Status, ProviderSequence: event.SequenceNumber,
			}); err != nil {
				return true, sdkgo.NewMutationUncertain(Response{}, openAIFailure(sdkgo.FailureTransport, "createResponse", "progress delivery failed after dispatch"), receipt())
			}
		case "response.output_text.delta":
			if err := call.WriteText(event.Delta); err != nil {
				return true, sdkgo.NewMutationUncertain(Response{}, openAIFailure(sdkgo.FailureTransport, "createResponse", "text progress delivery failed after dispatch"), receipt())
			}
		case "response.completed":
			result := convertResponse(event.Response)
			return true, sdkgo.NewMutationBranch(CreateResponseBranchCompleted, result, nil, responseReceipt(call, requestID, header, result.ID))
		case "response.failed", "response.incomplete":
			result := convertResponse(event.Response)
			failure := openAIFailure(sdkgo.FailureProviderRejection, "createResponse", "provider returned "+event.Type)
			return true, sdkgo.NewMutationBranch(CreateResponseBranchFailed, result, &failure, responseReceipt(call, requestID, header, result.ID))
		case "error":
			failure := openAIFailure(sdkgo.FailureProviderRejection, "createResponse", "provider returned a streaming error")
			return true, sdkgo.NewMutationBranch(CreateResponseBranchFailed, Response{ID: responseID}, &failure, receipt())
		}
		return false, sdkgo.MutationAttempt[Response]{}
	}
	attempt, err := readSSE(body, operation.client.maxResponseBytes, operation.client.maxSSEEventBytes, handler)
	if err != nil {
		return sdkgo.NewMutationUncertain(Response{ID: responseID}, openAIFailure(sdkgo.FailureProtocol, "createResponse", err.Error()), receipt())
	}
	return attempt
}

func readSSE(body io.Reader, maxTotalBytes int64, maxEventBytes int, handler sseEventHandler) (sdkgo.MutationAttempt[Response], error) {
	reader := bufio.NewReader(body)
	var data strings.Builder
	var totalBytes int64
	for {
		line, err := reader.ReadString('\n')
		totalBytes += int64(len(line))
		if totalBytes > maxTotalBytes {
			return sdkgo.MutationAttempt[Response]{}, fmt.Errorf("stream exceeds the configured size limit")
		}
		if len(line)+data.Len() > maxEventBytes {
			return sdkgo.MutationAttempt[Response]{}, fmt.Errorf("stream event exceeds the configured size limit")
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
			if decodeErr := json.Unmarshal([]byte(data.String()), &event); decodeErr != nil {
				return sdkgo.MutationAttempt[Response]{}, fmt.Errorf("stream event is invalid")
			}
			data.Reset()
			if terminal, attempt := handler(event); terminal {
				return attempt, nil
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
