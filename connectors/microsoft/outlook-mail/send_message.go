// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outlookmail

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	sendMessageOperationID = "sendMessage"
	// MaxRecipients is Exchange Online's limit on To, Cc, and Bcc recipients of one message.
	MaxRecipients = 500
	// MaxSendTextBytes bounds the plain-text body of a new message or a reply.
	MaxSendTextBytes = 512 << 10
)

// SendMessageInput is one new plain-text message.
type SendMessageInput struct {
	// To lists bare recipient addresses, such as jane@acme.example.com; at least one is required.
	To []string `json:"to"`
	// Cc lists bare addresses that receive a copy.
	Cc []string `json:"cc,omitempty"`
	// Bcc lists bare addresses that receive a blind copy and appear in no header.
	Bcc []string `json:"bcc,omitempty"`
	// ReplyTo lists bare addresses that replies should go to; empty means the mailbox itself.
	ReplyTo []string `json:"replyTo,omitempty"`
	// Subject is one line of at most MaxSubjectCharacters.
	Subject string `json:"subject"`
	// Text is the plain-text body, at most MaxSendTextBytes.
	Text string `json:"text"`
}

// SendMessageOperation implements the sendMessage Mutation.
type SendMessageOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (SendMessageOperation) Definition() sdkgo.MutationDefinition { return SendMessageDefinition }

// IdempotencyKey uses the stable connector Call ID, which every attempt of one Step execution shares. Graph
// has no idempotency key, so the key only forms the draft's IdempotencyMarker.
func (SendMessageOperation) IdempotencyKey(callID sdkgo.CallID, _ SendMessageInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a draft in Drafts that carries the Step's marker, then sends it at most once; see
// SentMessage and the package documentation for the retry behavior.
func (operation SendMessageOperation) Invoke(call sdkgo.Call, input SendMessageInput) sdkgo.MutationAttempt[SentMessage] {
	if err := validateSendMessageInput(input); err != nil {
		return sdkgo.NewMutationBranch(SendMessageBranchDefect, SentMessage{}, graphFailurePointer(sdkgo.FailureValidation, sendMessageOperationID, err.Error()), sdkgo.Receipt{})
	}
	return operation.client.sendDraftOnce(call, sendMessageOperationID, newMessageDraftCreator{input: input}, draftSendBranches{
		sent: SendMessageBranchSent, providerRejected: SendMessageBranchProviderRejected, defect: SendMessageBranchDefect,
	})
}

// newMessageDraftCreator creates a new draft with the marker in the same request.
type newMessageDraftCreator struct {
	input SendMessageInput
}

func (creator newMessageDraftCreator) createDraft(session *graphSession, marker string) (graphMessageWire, graphExchange) {
	input := creator.input
	payload := map[string]any{
		"subject":                       input.Subject,
		"body":                          map[string]string{"contentType": "text", "content": input.Text},
		"toRecipients":                  buildGraphRecipients(input.To),
		"singleValueExtendedProperties": []extendedPropertyWire{{ID: IdempotencyMarkerPropertyID, Value: marker}},
	}
	if len(input.Cc) > 0 {
		payload["ccRecipients"] = buildGraphRecipients(input.Cc)
	}
	if len(input.Bcc) > 0 {
		payload["bccRecipients"] = buildGraphRecipients(input.Bcc)
	}
	if len(input.ReplyTo) > 0 {
		payload["replyTo"] = buildGraphRecipients(input.ReplyTo)
	}
	result := session.exchange(graphRequest{method: http.MethodPost, path: "/messages", payload: payload})
	if result.outcome != graphSucceeded {
		return graphMessageWire{}, result
	}
	draft, err := decodeGraphMessage(result.body)
	if err != nil {
		// The marker was created with the draft, so the next attempt's lookup finds it.
		return graphMessageWire{}, invalidDraftAnswer(result, session.operation, "the created draft "+err.Error())
	}
	return draft, result
}

func (newMessageDraftCreator) isMarkedOnCreation() bool { return true }

func validateSendMessageInput(input SendMessageInput) error {
	if len(input.To) == 0 {
		return errors.New("to needs at least one recipient")
	}
	if recipients := len(input.To) + len(input.Cc) + len(input.Bcc); recipients > MaxRecipients {
		return fmt.Errorf("to, cc, and bcc can hold at most %d recipients together", MaxRecipients)
	}
	for _, field := range []struct {
		name      string
		addresses []string
	}{{"to", input.To}, {"cc", input.Cc}, {"bcc", input.Bcc}, {"replyTo", input.ReplyTo}} {
		if err := validateBareAddresses(field.name, field.addresses); err != nil {
			return err
		}
	}
	if strings.TrimSpace(input.Subject) == "" {
		return errors.New("subject is required")
	}
	if err := validateSingleLine("subject", input.Subject, MaxSubjectCharacters); err != nil {
		return err
	}
	return validateSendText(input.Text)
}

func validateSendText(text string) error {
	switch {
	case strings.TrimSpace(text) == "":
		return errors.New("text is required")
	case !utf8.ValidString(text):
		return errors.New("text must be valid UTF-8")
	case len(text) > MaxSendTextBytes:
		return fmt.Errorf("text can be at most %d bytes", MaxSendTextBytes)
	}
	return nil
}
