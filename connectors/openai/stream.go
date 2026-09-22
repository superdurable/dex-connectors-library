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

	connector "github.com/superdurable/dex-connectors-library/sdk/go"
)

type streamEvent struct {
	Type           string       `json:"type"`
	SequenceNumber *int64       `json:"sequence_number,omitempty"`
	Delta          string       `json:"delta,omitempty"`
	Response       wireResponse `json:"response"`
}

type sseEventHandler func(streamEvent) (bool, connector.MutationAttempt[Response])

func (operation CreateResponseOperation) readStream(call connector.Call, body io.Reader, header http.Header, requestID string) connector.MutationAttempt[Response] {
	responseID := ""
	receipt := func() connector.Receipt { return responseReceipt(call, requestID, header, responseID) }
	handler := func(event streamEvent) (bool, connector.MutationAttempt[Response]) {
		if event.Response.ID != "" {
			responseID = event.Response.ID
		}
		switch event.Type {
		case "response.created", "response.queued", "response.in_progress":
			if err := call.ReportProgress(connector.Progress{
				Phase: event.Type, Message: event.Response.Status, ProviderSequence: event.SequenceNumber,
			}); err != nil {
				return true, connector.NewMutationUnknown(Response{}, openAIFailure(connector.FailureTransport, "createResponse", "progress delivery failed after dispatch"), receipt())
			}
		case "response.output_text.delta":
			if err := call.WriteText(event.Delta); err != nil {
				return true, connector.NewMutationUnknown(Response{}, openAIFailure(connector.FailureTransport, "createResponse", "text progress delivery failed after dispatch"), receipt())
			}
		case "response.completed":
			result := convertResponse(event.Response)
			return true, connector.NewMutationSuccess(result, responseReceipt(call, requestID, header, result.ID))
		case "response.failed", "response.incomplete":
			result := convertResponse(event.Response)
			return true, connector.NewMutationFailure(result, openAIFailure(connector.FailureProviderRejection, "createResponse", "provider returned "+event.Type), responseReceipt(call, requestID, header, result.ID))
		case "error":
			return true, connector.NewMutationFailure(Response{ID: responseID}, openAIFailure(connector.FailureProviderRejection, "createResponse", "provider returned a streaming error"), receipt())
		}
		return false, connector.MutationAttempt[Response]{}
	}
	attempt, err := readSSE(body, operation.client.maxResponseBytes, operation.client.maxSSEEventBytes, handler)
	if err != nil {
		return connector.NewMutationUnknown(Response{ID: responseID}, openAIFailure(connector.FailureProtocol, "createResponse", err.Error()), receipt())
	}
	return attempt
}

func readSSE(body io.Reader, maxTotalBytes int64, maxEventBytes int, handler sseEventHandler) (connector.MutationAttempt[Response], error) {
	reader := bufio.NewReader(body)
	var data strings.Builder
	var totalBytes int64
	for {
		line, err := reader.ReadString('\n')
		totalBytes += int64(len(line))
		if totalBytes > maxTotalBytes {
			return connector.MutationAttempt[Response]{}, fmt.Errorf("stream exceeds the configured size limit")
		}
		if len(line)+data.Len() > maxEventBytes {
			return connector.MutationAttempt[Response]{}, fmt.Errorf("stream event exceeds the configured size limit")
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
				return connector.MutationAttempt[Response]{}, fmt.Errorf("stream event is invalid")
			}
			data.Reset()
			if terminal, attempt := handler(event); terminal {
				return attempt, nil
			}
		}
		if err != nil {
			if err == io.EOF {
				return connector.MutationAttempt[Response]{}, fmt.Errorf("stream ended before a terminal event")
			}
			return connector.MutationAttempt[Response]{}, fmt.Errorf("stream could not be read")
		}
	}
}
