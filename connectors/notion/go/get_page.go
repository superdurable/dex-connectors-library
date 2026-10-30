// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package notion

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	getPageOperationID       = "getPage"
	getPageFailureSubject    = "page read"
	getContentFailureSubject = "page content read"
	// DefaultMaxContentBlocks is the number of top-level blocks GetPage reads when MaxBlocks is zero.
	DefaultMaxContentBlocks = 100
	// MaximumContentBlocks bounds the top-level blocks one GetPage reads.
	MaximumContentBlocks = 500
	// DefaultMaxContentCharacters is the content text length GetPage keeps when MaxTextCharacters is zero.
	DefaultMaxContentCharacters = 20000
	// MaximumContentCharacters bounds the content text one GetPage returns.
	MaximumContentCharacters = 100000
	blockPageSize            = 100
	maximumCursorBytes       = 1024
)

// GetPageInput selects one page.
type GetPageInput struct {
	// PageID is the page ID, with or without dashes, or the page's Notion URL.
	PageID string `json:"pageId"`
	// ShouldSkipContent reads only the page's properties, without its blocks.
	ShouldSkipContent bool `json:"skipContent,omitempty"`
	// MaxBlocks bounds the top-level blocks read, 1 to 500; zero reads up to 100.
	MaxBlocks int `json:"maxBlocks,omitempty"`
	// MaxTextCharacters bounds Content.Text, 1 to 100,000 characters; zero keeps up to 20,000.
	MaxTextCharacters int `json:"maxTextCharacters,omitempty"`
}

// GetPageOutput is one page with its properties and bounded content.
type GetPageOutput struct {
	// Page holds the page's metadata and property values.
	Page Page `json:"page"`
	// Content renders the page's top-level blocks as plain text; it is empty when ShouldSkipContent is set.
	Content PageContent `json:"content"`
}

// PageContent is a bounded plain-text rendering of a page's top-level blocks.
//
// Each block becomes one line: list items start with "- " or a number, to-do
// items with "[ ] " or "[x] ", table rows join their cells with " | ", and child
// pages and databases show their titles. Blocks inside toggles, columns, and
// other parents are not read; NestedBlockCount counts the blocks that have them.
type PageContent struct {
	// Text is the rendered content, at most MaxTextCharacters characters.
	Text string `json:"text"`
	// BlockCount is the number of top-level blocks read.
	BlockCount int `json:"blockCount"`
	// HasMoreBlocks reports that the page has more top-level blocks than MaxBlocks.
	HasMoreBlocks bool `json:"hasMoreBlocks"`
	// IsTextTruncated reports that Text was cut at MaxTextCharacters.
	IsTextTruncated bool `json:"textTruncated"`
	// NestedBlockCount counts read blocks whose own child blocks were not read.
	NestedBlockCount int `json:"nestedBlockCount"`
	// SkippedBlockCount counts read blocks with no text rendering, such as images and dividers.
	SkippedBlockCount int `json:"skippedBlockCount"`
}

// GetPageOperation implements the getPage Query.
type GetPageOperation struct{ client *Client }

type blockListResource struct {
	Results    []blockResource `json:"results"`
	NextCursor *string         `json:"next_cursor"`
	HasMore    bool            `json:"has_more"`
}

type blockResource struct {
	Object      string `json:"object"`
	ID          string `json:"id"`
	Type        string `json:"type"`
	HasChildren bool   `json:"has_children"`
	raw         json.RawMessage
}

// UnmarshalJSON keeps the raw block so its type-specific object can be read by type name.
func (block *blockResource) UnmarshalJSON(contents []byte) error {
	type plainBlock blockResource
	var decoded plainBlock
	if err := json.Unmarshal(contents, &decoded); err != nil {
		return err
	}
	*block = blockResource(decoded)
	block.raw = append(json.RawMessage(nil), contents...)
	return nil
}

type blockTextResource struct {
	RichText   []richTextResource   `json:"rich_text"`
	Checked    *bool                `json:"checked"`
	Title      string               `json:"title"`
	Expression string               `json:"expression"`
	URL        string               `json:"url"`
	Caption    []richTextResource   `json:"caption"`
	Cells      [][]richTextResource `json:"cells"`
}

// Definition returns the immutable connector operation definition.
func (GetPageOperation) Definition() sdkgo.QueryDefinition { return GetPageDefinition }

// Invoke reads the page and up to MaxBlocks top-level blocks. Transport failures, 408, 409, 429, and 5xx are retried.
func (operation GetPageOperation) Invoke(call sdkgo.Call, input GetPageInput) sdkgo.QueryAttempt[GetPageOutput] {
	client := operation.client
	request, err := validateGetPageInput(input)
	if err != nil {
		return sdkgo.NewQueryBranch(GetPageBranchDefect, GetPageOutput{}, validationFailure(getPageOperationID, err), sdkgo.Receipt{})
	}
	session, cancel, sessionFailure := client.startSession(call, getPageOperationID, readOperationDeadline)
	if sessionFailure != nil {
		return sdkgo.NewQueryBranch(GetPageBranchDefect, GetPageOutput{}, sessionFailure, sdkgo.Receipt{})
	}
	defer cancel()
	result := client.exchange(session, notionRequest{method: http.MethodGet, path: "/pages/" + url.PathEscape(request.pageID)})
	receipt := client.receipt(session, result.response, request.pageID)
	if attempt, isFinal := getPageAttemptForRead(client.classifyRead(getPageOperationID, getPageFailureSubject, result), receipt); isFinal {
		return attempt
	}
	page, err := decodePageBody(result.response.body)
	if err != nil {
		return sdkgo.NewQueryBranch(GetPageBranchInvalidResponse, GetPageOutput{}, failurePointer(getPageOperationID, sdkgo.FailureProtocol, err.Error()), receipt)
	}
	output := GetPageOutput{Page: page}
	if request.shouldSkipContent {
		return sdkgo.NewQueryBranch(GetPageBranchFound, output, nil, receipt)
	}
	renderer := newContentRenderer(request.maxTextCharacters)
	cursor := ""
	for {
		pageSize := min(blockPageSize, request.maxBlocks-renderer.content.BlockCount)
		query := url.Values{"page_size": {strconv.Itoa(pageSize)}}
		if cursor != "" {
			query.Set("start_cursor", cursor)
		}
		blocksResult := client.exchange(session, notionRequest{method: http.MethodGet, path: "/blocks/" + url.PathEscape(page.ID) + "/children", query: query})
		if attempt, isFinal := getPageAttemptForRead(client.classifyRead(getPageOperationID, getContentFailureSubject, blocksResult), receipt); isFinal {
			return attempt
		}
		var blocks blockListResource
		if err := json.Unmarshal(blocksResult.response.body, &blocks); err != nil || len(blocks.Results) > pageSize {
			return sdkgo.NewQueryBranch(GetPageBranchInvalidResponse, GetPageOutput{},
				failurePointer(getPageOperationID, sdkgo.FailureProtocol, "Notion returned an invalid block list"), receipt)
		}
		for _, block := range blocks.Results {
			renderer.render(block)
		}
		nextCursor := dereferenceText(blocks.NextCursor)
		if !blocks.HasMore || nextCursor == "" {
			break
		}
		if renderer.content.BlockCount >= request.maxBlocks {
			renderer.content.HasMoreBlocks = true
			break
		}
		if cursor, err = validateCursor(nextCursor); err != nil {
			return sdkgo.NewQueryBranch(GetPageBranchInvalidResponse, GetPageOutput{},
				failurePointer(getPageOperationID, sdkgo.FailureProtocol, "Notion returned an invalid block cursor"), receipt)
		}
	}
	output.Content = renderer.finish()
	return sdkgo.NewQueryBranch(GetPageBranchFound, output, nil, receipt)
}

// getPageAttemptForRead returns the attempt for any read outcome other than success.
func getPageAttemptForRead(classification readClassification, receipt sdkgo.Receipt) (sdkgo.QueryAttempt[GetPageOutput], bool) {
	switch classification.outcome {
	case readSucceeded:
		return sdkgo.QueryAttempt[GetPageOutput]{}, false
	case readRetry:
		return sdkgo.NewQueryRetry[GetPageOutput](classification.failure, classification.retryAfter), true
	case readNotFound:
		return sdkgo.NewQueryBranch(GetPageBranchNotFound, GetPageOutput{}, &classification.failure, receipt), true
	case readDefect:
		return sdkgo.NewQueryBranch(GetPageBranchDefect, GetPageOutput{}, &classification.failure, receipt), true
	case readInvalid:
		return sdkgo.NewQueryBranch(GetPageBranchInvalidResponse, GetPageOutput{}, &classification.failure, receipt), true
	default:
		return sdkgo.NewQueryBranch(GetPageBranchProviderRejected, GetPageOutput{}, &classification.failure, receipt), true
	}
}

type getPageRequest struct {
	pageID            string
	shouldSkipContent bool
	maxBlocks         int
	maxTextCharacters int
}

func validateGetPageInput(input GetPageInput) (getPageRequest, error) {
	pageID, err := parseFieldID(input.PageID, "pageId")
	if err != nil {
		return getPageRequest{}, err
	}
	request := getPageRequest{pageID: pageID, shouldSkipContent: input.ShouldSkipContent, maxBlocks: input.MaxBlocks, maxTextCharacters: input.MaxTextCharacters}
	switch {
	case request.maxBlocks == 0:
		request.maxBlocks = DefaultMaxContentBlocks
	case request.maxBlocks < 1 || request.maxBlocks > MaximumContentBlocks:
		return getPageRequest{}, fmt.Errorf("maxBlocks must be between 1 and %d", MaximumContentBlocks)
	}
	switch {
	case request.maxTextCharacters == 0:
		request.maxTextCharacters = DefaultMaxContentCharacters
	case request.maxTextCharacters < 1 || request.maxTextCharacters > MaximumContentCharacters:
		return getPageRequest{}, fmt.Errorf("maxTextCharacters must be between 1 and %d", MaximumContentCharacters)
	}
	return request, nil
}

// contentRenderer turns blocks into bounded plain text, numbering consecutive numbered list items.
type contentRenderer struct {
	maximumCharacters int
	characterCount    int
	lines             []string
	listNumber        int
	content           PageContent
}

func newContentRenderer(maximumCharacters int) *contentRenderer {
	return &contentRenderer{maximumCharacters: maximumCharacters}
}

func (renderer *contentRenderer) render(block blockResource) {
	renderer.content.BlockCount++
	if block.HasChildren {
		renderer.content.NestedBlockCount++
	}
	if block.Type != "numbered_list_item" {
		renderer.listNumber = 0
	}
	line, isRendered := renderBlockLine(block, &renderer.listNumber)
	if !isRendered {
		renderer.content.SkippedBlockCount++
		return
	}
	renderer.appendLine(line)
}

func (renderer *contentRenderer) appendLine(line string) {
	if renderer.content.IsTextTruncated {
		return
	}
	separatorLength := 0
	if len(renderer.lines) > 0 {
		separatorLength = 1
	}
	remaining := renderer.maximumCharacters - renderer.characterCount - separatorLength
	lineLength := utf8.RuneCountInString(line)
	if lineLength > remaining {
		renderer.content.IsTextTruncated = true
		if remaining <= 0 {
			return
		}
		line = string([]rune(line)[:remaining])
		lineLength = remaining
	}
	renderer.lines = append(renderer.lines, line)
	renderer.characterCount += separatorLength + lineLength
}

func (renderer *contentRenderer) finish() PageContent {
	renderer.content.Text = strings.Join(renderer.lines, "\n")
	return renderer.content
}

// renderBlockLine returns a block's one-line text rendering, or false for a block without text.
func renderBlockLine(block blockResource, listNumber *int) (string, bool) {
	content, err := blockTypeContent(block)
	if err != nil {
		return "", false
	}
	text := joinPlainText(content.RichText)
	switch block.Type {
	case "paragraph", "heading_1", "heading_2", "heading_3", "heading_4", "quote", "callout", "toggle", "code", "template":
		return text, true
	case "bulleted_list_item":
		return "- " + text, true
	case "numbered_list_item":
		*listNumber++
		return strconv.Itoa(*listNumber) + ". " + text, true
	case "to_do":
		if content.Checked != nil && *content.Checked {
			return "[x] " + text, true
		}
		return "[ ] " + text, true
	case "child_page", "child_database":
		return content.Title, content.Title != ""
	case "equation":
		return content.Expression, content.Expression != ""
	case "bookmark", "embed", "link_preview":
		return content.URL, content.URL != ""
	case "table_row":
		cells := make([]string, 0, len(content.Cells))
		for _, cell := range content.Cells {
			cells = append(cells, joinPlainText(cell))
		}
		return strings.Join(cells, " | "), len(cells) > 0
	case "image", "video", "file", "pdf", "audio":
		caption := joinPlainText(content.Caption)
		return caption, caption != ""
	default:
		return "", false
	}
}

func blockTypeContent(block blockResource) (blockTextResource, error) {
	if block.Type == "" {
		return blockTextResource{}, errors.New("block has no type")
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(block.raw, &envelope); err != nil {
		return blockTextResource{}, err
	}
	raw, isPresent := envelope[block.Type]
	if !isPresent {
		return blockTextResource{}, errors.New("block has no content object")
	}
	var content blockTextResource
	if err := json.Unmarshal(raw, &content); err != nil {
		return blockTextResource{}, err
	}
	return content, nil
}
