// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jiraservicemanagement

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"
)

const (
	// maximumPlainTextCharacters is Jira's limit for a description or comment body.
	maximumPlainTextCharacters = 32767
	// maximumReadTextCharacters bounds the plain text the connector returns for one description or comment.
	maximumReadTextCharacters = 32767
	maximumDocumentDepth      = 48
)

// documentNode is the subset of an Atlassian Document Format node the connector reads.
type documentNode struct {
	Type    string         `json:"type"`
	Version int            `json:"version,omitempty"`
	Text    string         `json:"text,omitempty"`
	Attrs   map[string]any `json:"attrs,omitempty"`
	Content []documentNode `json:"content,omitempty"`
}

// documentTextWriter extracts bounded plain text from an Atlassian Document Format tree.
type documentTextWriter struct {
	builder     strings.Builder
	characters  int
	isTruncated bool
}

// validatePlainText checks a description or comment body before it is sent.
func validatePlainText(text string, fieldName string, isRequired bool) error {
	if !utf8.ValidString(text) {
		return errors.New(fieldName + " must be valid UTF-8")
	}
	if isRequired && strings.TrimSpace(text) == "" {
		return errors.New(fieldName + " is required")
	}
	if utf8.RuneCountInString(text) > maximumPlainTextCharacters {
		return errors.New(fieldName + " cannot exceed 32767 characters")
	}
	for _, character := range text {
		if character == 0 || (character < ' ' && character != '\n' && character != '\r' && character != '\t') {
			return errors.New(fieldName + " cannot contain control characters")
		}
	}
	return nil
}

// extractDocumentText reads a document or a stored text string, reporting text beyond the read limit as truncated.
func extractDocumentText(contents json.RawMessage) (string, bool, error) {
	if len(contents) == 0 || string(contents) == "null" {
		return "", false, nil
	}
	var storedText string
	if json.Unmarshal(contents, &storedText) == nil {
		text, isTruncated := truncatePlainText(storedText)
		return text, isTruncated, nil
	}
	var document documentNode
	if err := json.Unmarshal(contents, &document); err != nil {
		return "", false, errors.New("document is not Atlassian Document Format")
	}
	writer := &documentTextWriter{}
	writer.writeBlocks(document.Content, "\n\n", 0)
	return strings.TrimRight(writer.builder.String(), "\n"), writer.isTruncated, nil
}

// truncatePlainText bounds a text string from the provider to the read limit.
func truncatePlainText(text string) (string, bool) {
	writer := &documentTextWriter{}
	writer.writeText(text)
	return writer.builder.String(), writer.isTruncated
}

func (writer *documentTextWriter) writeBlocks(nodes []documentNode, separator string, depth int) {
	for index, node := range nodes {
		if index > 0 {
			writer.writeText(separator)
		}
		writer.writeNode(node, depth+1)
	}
}

func (writer *documentTextWriter) writeNode(node documentNode, depth int) {
	if depth > maximumDocumentDepth || writer.isTruncated {
		return
	}
	switch node.Type {
	case "text":
		writer.writeText(node.Text)
	case "hardBreak":
		writer.writeText("\n")
	case "mention", "emoji", "status":
		writer.writeText(stringAttribute(node.Attrs, "text"))
	case "inlineCard", "blockCard", "embedCard":
		writer.writeText(stringAttribute(node.Attrs, "url"))
	case "rule":
		writer.writeText("---")
	case "paragraph", "heading", "codeBlock":
		for _, child := range node.Content {
			writer.writeNode(child, depth+1)
		}
	case "bulletList", "orderedList":
		for index, item := range node.Content {
			if index > 0 {
				writer.writeText("\n")
			}
			writer.writeText("- ")
			writer.writeBlocks(item.Content, "\n", depth)
		}
	case "tableRow":
		writer.writeBlocks(node.Content, " | ", depth)
	case "media", "mediaSingle", "mediaGroup", "mediaInline":
		// Attachments carry no text.
	default:
		writer.writeBlocks(node.Content, "\n", depth)
	}
}

func (writer *documentTextWriter) writeText(text string) {
	for _, character := range text {
		if writer.characters >= maximumReadTextCharacters {
			writer.isTruncated = true
			return
		}
		writer.builder.WriteRune(character)
		writer.characters++
	}
}

func stringAttribute(attributes map[string]any, name string) string {
	value, _ := attributes[name].(string)
	return value
}
