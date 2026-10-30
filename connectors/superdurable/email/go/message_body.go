// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package email

import (
	"bytes"
	"errors"
	"io"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/emersion/go-imap/v2"
	gomessage "github.com/emersion/go-message"
	// Importing charset registers decoders for ISO-8859, Windows, and other common mail charsets.
	_ "github.com/emersion/go-message/charset"
	"golang.org/x/net/html"
)

const (
	// MaxTextBytes bounds the decoded text getMessage returns; longer text is cut on a UTF-8 boundary.
	MaxTextBytes = 64 << 10
	// MaxAttachments bounds the attachment list getMessage returns.
	MaxAttachments = 50

	// maximumEncodedBodyFetchBytes bounds the encoded body part read from the server.
	maximumEncodedBodyFetchBytes = 1 << 20
	maximumFileNameBytes         = 255
)

// TextSource names the body part that Message.Text comes from.
type TextSource string

const (
	// TextSourcePlain means Text is the message's text/plain part.
	TextSourcePlain TextSource = "plain"
	// TextSourceHTML means the message has no text/plain part, so Text is the text of its text/html part
	// with tags, scripts, and styles removed.
	TextSourceHTML TextSource = "html"
	// TextSourceNone means the message has no readable text part.
	TextSourceNone TextSource = "none"
)

// Attachment describes one attachment without its content.
type Attachment struct {
	// Part is the IMAP body section number, such as 2 or 1.2.
	Part string `json:"part"`
	// FileName is the attachment's file name, or empty when the part has none.
	FileName string `json:"fileName,omitempty"`
	// MediaType is the lowercase MIME type, such as application/pdf.
	MediaType string `json:"mediaType"`
	// EncodedSizeBytes is the part's size in its transfer encoding; base64 is about four thirds of the file size.
	EncodedSizeBytes int64 `json:"encodedSizeBytes"`
	// IsInline reports an inline disposition, such as an image shown in the message body.
	IsInline bool `json:"isInline,omitempty"`
}

// messageBodyParts is the text part chosen from one body structure, with its IMAP section path.
type messageBodyParts struct {
	textPath   []int
	textPart   *imap.BodyStructureSinglePart
	textSource TextSource
}

// chooseTextPart prefers the first text/plain body part and falls back to the first text/html one.
func chooseTextPart(structure imap.BodyStructure) messageBodyParts {
	for _, mediaType := range []string{"text/plain", "text/html"} {
		var chosenPath []int
		var chosenPart *imap.BodyStructureSinglePart
		structure.Walk(func(path []int, part imap.BodyStructure) bool {
			if chosenPart != nil {
				return false
			}
			singlePart, isSinglePart := part.(*imap.BodyStructureSinglePart)
			if isSinglePart && singlePart.MediaType() == mediaType && !isAttachmentPart(singlePart) {
				chosenPath, chosenPart = append([]int(nil), path...), singlePart
			}
			return true
		})
		if chosenPart != nil {
			source := TextSourcePlain
			if mediaType == "text/html" {
				source = TextSourceHTML
			}
			return messageBodyParts{textPath: chosenPath, textPart: chosenPart, textSource: source}
		}
	}
	return messageBodyParts{textSource: TextSourceNone}
}

// findAttachments lists parts with an attachment disposition, a file name, or an encapsulated message.
// chooseTextPart never chooses such a part, so the text part is never listed.
func findAttachments(structure imap.BodyStructure) []Attachment {
	var attachments []Attachment
	structure.Walk(func(path []int, part imap.BodyStructure) bool {
		singlePart, isSinglePart := part.(*imap.BodyStructureSinglePart)
		if !isSinglePart || !isAttachmentPart(singlePart) {
			return true
		}
		disposition := singlePart.Disposition()
		attachments = append(attachments, Attachment{
			Part: formatSectionPath(path), FileName: truncateUTF8(strings.ToValidUTF8(singlePart.Filename(), ""), maximumFileNameBytes),
			MediaType: singlePart.MediaType(), EncodedSizeBytes: int64(singlePart.Size),
			IsInline: disposition != nil && strings.EqualFold(disposition.Value, "inline"),
		})
		return true
	})
	return attachments
}

// isAttachmentPart treats a named part, an attachment disposition, or an encapsulated message as an attachment.
func isAttachmentPart(part *imap.BodyStructureSinglePart) bool {
	if disposition := part.Disposition(); disposition != nil && strings.EqualFold(disposition.Value, "attachment") {
		return true
	}
	return part.Filename() != "" || part.MediaType() == "message/rfc822"
}

// decodeBodyText decodes the transfer encoding and charset of one fetched part and returns bounded UTF-8 text.
// isPartial reports that the server returned only the first maximumEncodedBodyFetchBytes, so a decoding error
// at the cut is expected.
func decodeBodyText(part *imap.BodyStructureSinglePart, source TextSource, encoded []byte, isPartial bool) (string, bool, error) {
	var header gomessage.Header
	header.SetContentType(part.MediaType(), part.Params)
	if part.Encoding != "" {
		header.Set("Content-Transfer-Encoding", part.Encoding)
	}
	entity, err := gomessage.New(header, bytes.NewReader(encoded))
	if err != nil && !gomessage.IsUnknownCharset(err) && !gomessage.IsUnknownEncoding(err) {
		return "", false, err
	}
	readLimit := int64(MaxTextBytes + 1)
	if source == TextSourceHTML {
		readLimit = maximumEncodedBodyFetchBytes
	}
	decoded, readErr := io.ReadAll(io.LimitReader(entity.Body, readLimit))
	if readErr != nil && !isPartial {
		return "", false, errors.New("the message body could not be decoded")
	}
	isTruncated := isPartial
	text := strings.ToValidUTF8(string(decoded), string(utf8.RuneError))
	if source == TextSourceHTML {
		text = extractTextFromHTML(text)
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if len(text) > MaxTextBytes {
		text, isTruncated = truncateUTF8(text, MaxTextBytes), true
	}
	return text, isTruncated, nil
}

// extractTextFromHTML keeps visible text, turns block boundaries into line breaks, and drops scripts and styles.
func extractTextFromHTML(document string) string {
	tokenizer := html.NewTokenizer(strings.NewReader(document))
	var text strings.Builder
	skippedDepth := 0
	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			return collapseBlankLines(text.String())
		case html.StartTagToken, html.SelfClosingTagToken:
			name, _ := tokenizer.TagName()
			switch string(name) {
			case "script", "style", "head", "title":
				skippedDepth++
			case "br", "p", "div", "li", "tr", "h1", "h2", "h3", "h4", "h5", "h6", "blockquote", "table":
				text.WriteByte('\n')
			}
		case html.EndTagToken:
			name, _ := tokenizer.TagName()
			switch string(name) {
			case "script", "style", "head", "title":
				skippedDepth = max(0, skippedDepth-1)
			case "p", "div", "li", "tr", "h1", "h2", "h3", "h4", "h5", "h6", "blockquote", "table":
				text.WriteByte('\n')
			}
		case html.TextToken:
			if skippedDepth == 0 {
				writeHTMLTextRun(&text, string(tokenizer.Text()))
			}
		}
	}
}

// writeHTMLTextRun collapses a text run's whitespace to single spaces and keeps one space at a run boundary
// where the HTML had whitespace, as a browser renders it.
func writeHTMLTextRun(text *strings.Builder, run string) {
	words := strings.Fields(run)
	if len(words) == 0 {
		if run != "" {
			writeSeparatingSpace(text)
		}
		return
	}
	if first, _ := utf8.DecodeRuneInString(run); unicode.IsSpace(first) {
		writeSeparatingSpace(text)
	}
	text.WriteString(strings.Join(words, " "))
	if last, _ := utf8.DecodeLastRuneInString(run); unicode.IsSpace(last) {
		text.WriteByte(' ')
	}
}

// writeSeparatingSpace adds a space unless the text is empty or already ends in whitespace.
func writeSeparatingSpace(text *strings.Builder) {
	current := text.String()
	if current == "" {
		return
	}
	if last, _ := utf8.DecodeLastRuneInString(current); !unicode.IsSpace(last) {
		text.WriteByte(' ')
	}
}

// collapseBlankLines trims each line and keeps at most one empty line between paragraphs.
func collapseBlankLines(text string) string {
	var lines []string
	isPreviousBlank := true
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			if !isPreviousBlank {
				lines = append(lines, "")
			}
			isPreviousBlank = true
			continue
		}
		lines = append(lines, line)
		isPreviousBlank = false
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func formatSectionPath(path []int) string {
	parts := make([]string, len(path))
	for index, number := range path {
		parts[index] = strconv.Itoa(number)
	}
	return strings.Join(parts, ".")
}
