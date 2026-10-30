// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package docs

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

const (
	// readDocumentFields selects what getDocumentText renders, plus child tab IDs to report other tabs.
	readDocumentFields = "documentId,title,revisionId,tabs(tabProperties/tabId,childTabs/tabProperties/tabId,documentTab(body,lists))"
	// writeDocumentFields selects the revision and first-tab body that guarded writes inspect.
	writeDocumentFields = "documentId,revisionId,tabs(tabProperties/tabId,documentTab/body)"
	// suggestionsPreviewWithout reads the text in force: every pending suggestion rejected.
	suggestionsPreviewWithout = "PREVIEW_WITHOUT_SUGGESTIONS"
	// suggestionsInline is the view Google requires for computing edit indexes.
	suggestionsInline = "SUGGESTIONS_INLINE"
	// nonTextElementMarker is U+E907, which Google's text runs use for an inline element without text.
	nonTextElementMarker = rune(0xE907)
	// maxStructureDepth bounds tables nested in table cells.
	maxStructureDepth = 16
)

var errDocumentStructureTooDeep = errors.New("document tables are nested too deeply")

// documentResource is the subset of a Google Docs Document the connector reads.
type documentResource struct {
	DocumentID string        `json:"documentId"`
	Title      string        `json:"title"`
	RevisionID string        `json:"revisionId"`
	Tabs       []tabResource `json:"tabs"`
}

type tabResource struct {
	TabProperties tabPropertiesResource `json:"tabProperties"`
	ChildTabs     []tabResource         `json:"childTabs"`
	DocumentTab   *documentTabResource  `json:"documentTab"`
}

type tabPropertiesResource struct {
	TabID string `json:"tabId"`
}

type documentTabResource struct {
	Body  bodyResource            `json:"body"`
	Lists map[string]listResource `json:"lists"`
}

type bodyResource struct {
	Content []structuralElementResource `json:"content"`
}

type structuralElementResource struct {
	StartIndex      int                      `json:"startIndex"`
	EndIndex        int                      `json:"endIndex"`
	Paragraph       *paragraphResource       `json:"paragraph"`
	SectionBreak    json.RawMessage          `json:"sectionBreak"`
	Table           *tableResource           `json:"table"`
	TableOfContents *tableOfContentsResource `json:"tableOfContents"`
}

type paragraphResource struct {
	Elements       []paragraphElementResource `json:"elements"`
	ParagraphStyle paragraphStyleResource     `json:"paragraphStyle"`
	Bullet         *bulletResource            `json:"bullet"`
}

type paragraphStyleResource struct {
	NamedStyleType string `json:"namedStyleType"`
}

type bulletResource struct {
	ListID       string `json:"listId"`
	NestingLevel int    `json:"nestingLevel"`
}

type paragraphElementResource struct {
	TextRun           *textRunResource           `json:"textRun"`
	FootnoteReference *footnoteReferenceResource `json:"footnoteReference"`
	HorizontalRule    json.RawMessage            `json:"horizontalRule"`
	Person            *personResource            `json:"person"`
	RichLink          *richLinkResource          `json:"richLink"`
	DateElement       *dateElementResource       `json:"dateElement"`
}

type textRunResource struct {
	Content string `json:"content"`
}

type footnoteReferenceResource struct {
	FootnoteNumber string `json:"footnoteNumber"`
}

type personResource struct {
	PersonProperties struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	} `json:"personProperties"`
}

type richLinkResource struct {
	RichLinkProperties struct {
		Title string `json:"title"`
		URI   string `json:"uri"`
	} `json:"richLinkProperties"`
}

type dateElementResource struct {
	DateElementProperties struct {
		DisplayText string `json:"displayText"`
	} `json:"dateElementProperties"`
}

type tableResource struct {
	TableRows []tableRowResource `json:"tableRows"`
}

type tableRowResource struct {
	TableCells []tableCellResource `json:"tableCells"`
}

type tableCellResource struct {
	Content []structuralElementResource `json:"content"`
}

type tableOfContentsResource struct {
	Content []structuralElementResource `json:"content"`
}

type listResource struct {
	ListProperties struct {
		NestingLevels []nestingLevelResource `json:"nestingLevels"`
	} `json:"listProperties"`
}

type nestingLevelResource struct {
	GlyphType   string `json:"glyphType"`
	GlyphSymbol string `json:"glyphSymbol"`
}

// decodeDocument validates the untrusted Docs response the connector depends on.
func decodeDocument(content []byte) (documentResource, error) {
	var document documentResource
	if err := json.Unmarshal(content, &document); err != nil {
		return documentResource{}, errors.New("document response is not valid JSON")
	}
	if !isDriveID(document.DocumentID) {
		return documentResource{}, errors.New("document response lacks a valid document ID")
	}
	if len(document.Tabs) == 0 || document.Tabs[0].DocumentTab == nil {
		return documentResource{}, errors.New("document response lacks its first tab")
	}
	if !tabIDPattern.MatchString(document.Tabs[0].TabProperties.TabID) {
		return documentResource{}, errors.New("document response lacks a valid first tab ID")
	}
	bodyContent := document.Tabs[0].DocumentTab.Body.Content
	if len(bodyContent) == 0 || bodyContent[len(bodyContent)-1].Paragraph == nil || bodyContent[len(bodyContent)-1].EndIndex < 2 {
		return documentResource{}, errors.New("document body does not end with a paragraph")
	}
	if !utf8.ValidString(document.RevisionID) || !utf8.ValidString(document.Title) {
		return documentResource{}, errors.New("document title or revision ID is not valid UTF-8")
	}
	return document, nil
}

// firstTab is the tab Google writes to when a request names no tab.
func (document documentResource) firstTab() tabResource { return document.Tabs[0] }

func (document documentResource) hasOtherTabs() bool {
	return len(document.Tabs) > 1 || len(document.Tabs[0].ChildTabs) > 0
}

// bodyEndIndex is the exclusive end of the first tab's body, after its final newline.
func (document documentResource) bodyEndIndex() int {
	content := document.firstTab().DocumentTab.Body.Content
	return content[len(content)-1].EndIndex
}

// bodyText joins the first tab's text runs, table cells included; non-text elements become a marker no placeholder spans.
func (document documentResource) bodyText() (string, error) {
	var text strings.Builder
	err := appendStructuralText(&text, document.firstTab().DocumentTab.Body.Content, 0)
	return text.String(), err
}

func appendStructuralText(text *strings.Builder, content []structuralElementResource, depth int) error {
	if depth > maxStructureDepth {
		return errDocumentStructureTooDeep
	}
	for _, element := range content {
		switch {
		case element.Paragraph != nil:
			for _, paragraphElement := range element.Paragraph.Elements {
				if paragraphElement.TextRun != nil {
					text.WriteString(paragraphElement.TextRun.Content)
				} else {
					text.WriteRune(nonTextElementMarker)
				}
			}
		case element.Table != nil:
			for _, row := range element.Table.TableRows {
				for _, cell := range row.TableCells {
					if err := appendStructuralText(text, cell.Content, depth+1); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

// utf16Length is the length of value in the UTF-16 code units Google uses for indexes.
func utf16Length(value string) int {
	length := 0
	for _, character := range value {
		length += utf16.RuneLen(character)
	}
	return length
}

// validateWriteText rejects characters Google strips on insertion, so written text compares exactly.
func validateWriteText(field string, value string) error {
	if !utf8.ValidString(value) {
		return errors.New(field + " must be valid UTF-8")
	}
	for _, character := range value {
		switch {
		case character == '\r':
			return errors.New(field + " must use \\n line breaks without \\r")
		case character <= 0x08 || (character >= 0x0C && character <= 0x1F):
			return errors.New(field + " cannot contain control characters other than tab and line breaks")
		case character >= 0xE000 && character <= 0xF8FF:
			return errors.New(field + " cannot contain private-use characters, which Google strips")
		}
	}
	return nil
}
