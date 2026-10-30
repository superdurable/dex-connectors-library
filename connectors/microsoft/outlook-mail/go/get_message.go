// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outlookmail

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"golang.org/x/net/html"
)

const (
	getMessageOperationID = "getMessage"
	// MaxTextBytes bounds the plain-text body getMessage returns; longer text is cut on a UTF-8 boundary.
	MaxTextBytes = 64 << 10
	// MaxListedAttachments bounds the attachment list getMessage returns.
	MaxListedAttachments = 50

	textSourceText = "text"
	textSourceHTML = "html"
)

// attachmentProperties excludes contentBytes, so listing never downloads an attachment.
var attachmentProperties = []string{"id", "name", "contentType", "size", "isInline"}

// GetMessageInput identifies one message.
type GetMessageInput struct {
	// MessageID is the id of a searchMessages summary or of an earlier getMessage Result.
	MessageID string `json:"messageId"`
}

// GetMessageOperation implements the getMessage Query.
type GetMessageOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (GetMessageOperation) Definition() sdkgo.QueryDefinition { return GetMessageDefinition }

// Invoke reads the message with a plain-text body, then lists its attachments when it has any. Reading
// never marks the message read.
func (operation GetMessageOperation) Invoke(call sdkgo.Call, input GetMessageInput) sdkgo.QueryAttempt[Message] {
	messageID := strings.TrimSpace(input.MessageID)
	if err := validateGraphID("messageId", messageID); err != nil {
		return sdkgo.NewQueryBranch(GetMessageBranchDefect, Message{}, graphFailurePointer(sdkgo.FailureValidation, getMessageOperationID, err.Error()), sdkgo.Receipt{})
	}
	session, failed := operation.client.openSession(call, getMessageOperationID)
	if failed != nil {
		attempt, _ := queryAttemptForExchange[Message](*failed, sdkgo.Receipt{}, getMessageBranches)
		return attempt
	}
	messagePath := "/messages/" + url.PathEscape(messageID)
	properties := append(append([]string(nil), summaryProperties...), "bccRecipients", "body", "webLink")
	result := session.exchange(graphRequest{
		method: http.MethodGet, path: messagePath, prefersTextBody: true,
		query: graphQuery{{name: "$select", value: strings.Join(properties, ",")}},
	})
	receipt := session.receipt(result, messageID)
	if attempt, isTerminal := queryAttemptForExchange[Message](result, receipt, getMessageBranches); isTerminal {
		return attempt
	}
	message, err := decodeFullMessage(result.body)
	if err != nil {
		return sdkgo.NewQueryBranch(GetMessageBranchInvalidResponse, Message{},
			graphFailurePointer(sdkgo.FailureProtocol, getMessageOperationID, "Microsoft Graph returned an invalid message: "+err.Error()), receipt)
	}
	if !message.HasAttachments {
		return sdkgo.NewQueryBranch(GetMessageBranchFound, message, nil, receipt)
	}
	listed := session.exchange(graphRequest{
		method: http.MethodGet, path: messagePath + "/attachments",
		query: graphQuery{{name: "$select", value: strings.Join(attachmentProperties, ",")}},
	})
	receipt = session.receipt(listed, messageID)
	if attempt, isTerminal := queryAttemptForExchange[Message](listed, receipt, getMessageBranches); isTerminal {
		return attempt
	}
	attachments, isTruncated, err := decodeAttachmentList(listed.body)
	if err != nil {
		return sdkgo.NewQueryBranch(GetMessageBranchInvalidResponse, Message{},
			graphFailurePointer(sdkgo.FailureProtocol, getMessageOperationID, "Microsoft Graph returned an invalid attachment list: "+err.Error()), receipt)
	}
	message.Attachments, message.IsAttachmentListTruncated = attachments, isTruncated
	return sdkgo.NewQueryBranch(GetMessageBranchFound, message, nil, receipt)
}

var getMessageBranches = queryBranches{
	notFound: GetMessageBranchNotFound, providerRejected: GetMessageBranchProviderRejected,
	invalidResponse: GetMessageBranchInvalidResponse, defect: GetMessageBranchDefect,
}

// decodeFullMessage converts the message and bounds its body, removing markup if Graph returned HTML.
func decodeFullMessage(body []byte) (Message, error) {
	wire, err := decodeGraphMessage(body)
	if err != nil {
		return Message{}, err
	}
	message := Message{MessageSummary: wire.summarize(), Bcc: convertAddressList(wire.BccRecipients), WebLink: wire.WebLink, TextSource: textSourceText}
	if wire.Body != nil {
		text := wire.Body.Content
		if strings.EqualFold(wire.Body.ContentType, "html") {
			text, message.TextSource = extractHTMLText(text), textSourceHTML
		}
		message.Text, message.IsTextTruncated = truncateUTF8Bytes(text, MaxTextBytes)
	}
	return message, nil
}

// decodeAttachmentList reads the first page of attachments; a second page counts as truncation.
func decodeAttachmentList(body []byte) ([]Attachment, bool, error) {
	var envelope struct {
		Value *[]struct {
			ODataType   string `json:"@odata.type"`
			ID          string `json:"id"`
			Name        string `json:"name"`
			ContentType string `json:"contentType"`
			Size        int64  `json:"size"`
			IsInline    bool   `json:"isInline"`
		} `json:"value"`
		NextLink string `json:"@odata.nextLink"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Value == nil {
		return nil, false, errors.New("response has no value list")
	}
	attachments := []Attachment{}
	for _, item := range *envelope.Value {
		if len(attachments) == MaxListedAttachments {
			return attachments, true, nil
		}
		if item.ID == "" || item.Size < 0 {
			return nil, false, errors.New("an attachment has no id or a negative size")
		}
		attachments = append(attachments, Attachment{
			ID: item.ID, Name: item.Name, ContentType: item.ContentType, SizeBytes: item.Size, IsInline: item.IsInline,
			Kind: attachmentKindForODataType(item.ODataType),
		})
	}
	return attachments, envelope.NextLink != "", nil
}

func attachmentKindForODataType(odataType string) AttachmentKind {
	switch odataType {
	case "#microsoft.graph.itemAttachment":
		return AttachmentKindItem
	case "#microsoft.graph.referenceAttachment":
		return AttachmentKindReference
	default:
		return AttachmentKindFile
	}
}

// extractHTMLText keeps the text of an HTML body without tags, scripts, or styles, one block per line.
func extractHTMLText(document string) string {
	tokenizer := html.NewTokenizer(strings.NewReader(document))
	var text strings.Builder
	skippedDepth := 0
	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			// A strings.Reader fails only at io.EOF, so the error marks the end of the document.
			return strings.TrimSpace(text.String())
		case html.StartTagToken:
			name, _ := tokenizer.TagName()
			switch string(name) {
			case "script", "style", "head":
				skippedDepth++
			case "br", "p", "div", "li", "tr", "h1", "h2", "h3", "h4", "blockquote":
				text.WriteString("\n")
			}
		case html.EndTagToken:
			name, _ := tokenizer.TagName()
			switch string(name) {
			case "script", "style", "head":
				skippedDepth = max(skippedDepth-1, 0)
			}
		case html.TextToken:
			if skippedDepth == 0 {
				text.Write(tokenizer.Text())
			}
		}
	}
}
