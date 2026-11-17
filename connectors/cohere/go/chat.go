// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package cohere

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ChatMessage is one text message in chronological order.
type ChatMessage struct {
	// Role is system, user, or assistant.
	Role string `json:"role"`
	// Content is the nonempty message text.
	Content string `json:"content"`
}

// ChatInput selects a model and a text conversation for one generation.
type ChatInput struct {
	// Model is a Cohere Chat model ID.
	Model string `json:"model"`
	// Messages are chronological text messages.
	Messages []ChatMessage `json:"messages"`
	// MaxTokens limits generated output tokens; zero uses the model default.
	MaxTokens int `json:"max_tokens,omitempty"`
}

// ChatOutput contains the provider reply and completion reason.
type ChatOutput struct {
	// ID is the provider response identifier.
	ID string `json:"id"`
	// Text is the concatenated text content.
	Text string `json:"text"`
	// FinishReason identifies why generation stopped.
	FinishReason string `json:"finishReason"`
}

// ChatOperation performs one Cohere v2 chat request.
type ChatOperation struct{ client *Client }

// Chat binds a text generation Query to this client.
func (client *Client) Chat() ChatOperation { return ChatOperation{client} }

// Definition returns the generated chat contract.
func (ChatOperation) Definition() sdkgo.QueryDefinition { return ChatDefinition }

// Invoke validates text messages and requests a generated reply.
func (operation ChatOperation) Invoke(call sdkgo.Call, input ChatInput) sdkgo.QueryAttempt[ChatOutput] {
	if strings.TrimSpace(input.Model) == "" || len(input.Messages) == 0 || len(input.Messages) > 100 || input.MaxTokens < 0 {
		return failedBranch[ChatOutput]("chat", ChatBranchDefect, sdkgo.FailureValidation, "model, 1 to 100 messages, and nonnegative maxTokens are required")
	}
	for _, message := range input.Messages {
		if strings.TrimSpace(message.Content) == "" || (message.Role != "user" && message.Role != "assistant" && message.Role != "system") {
			return failedBranch[ChatOutput]("chat", ChatBranchDefect, sdkgo.FailureValidation, "messages require text and a supported role")
		}
	}
	return invokeJSON(operation.client, call, "chat", "/v2/chat", ChatBranchAnswered, input, decodeChat)
}

func decodeChat(body []byte) (ChatOutput, error) {
	var wire struct {
		ID           string `json:"id"`
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Role    string `json:"role"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return ChatOutput{}, err
	}
	if wire.ID == "" || wire.FinishReason == "" || wire.Message.Role != "assistant" || len(wire.Message.Content) == 0 {
		return ChatOutput{}, errors.New("incomplete chat reply")
	}
	var text strings.Builder
	for _, part := range wire.Message.Content {
		if part.Type != "text" {
			return ChatOutput{}, errors.New("unsupported chat content")
		}
		text.WriteString(part.Text)
	}
	if text.Len() == 0 {
		return ChatOutput{}, errors.New("empty chat reply")
	}
	return ChatOutput{ID: wire.ID, Text: text.String(), FinishReason: wire.FinishReason}, nil
}
