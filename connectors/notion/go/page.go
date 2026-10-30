// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package notion

import (
	"encoding/json"
	"errors"
	"strings"
)

// ParentType names the kind of object that contains a page or data source.
type ParentType string

const (
	// ParentTypeDataSource is a data source; the page is a database row.
	ParentTypeDataSource ParentType = "data_source_id"
	// ParentTypeDatabase is a database, the usual parent of a data source.
	ParentTypeDatabase ParentType = "database_id"
	// ParentTypePage is another page.
	ParentTypePage ParentType = "page_id"
	// ParentTypeBlock is a block inside a page.
	ParentTypeBlock ParentType = "block_id"
	// ParentTypeWorkspace is the workspace root.
	ParentTypeWorkspace ParentType = "workspace"
)

// Parent identifies the object that contains a page or data source.
type Parent struct {
	// Type is the parent's kind; other kinds Notion adds are kept with an empty ID.
	Type ParentType `json:"type"`
	// ID is the parent's ID; it is empty for the workspace.
	ID string `json:"id,omitempty"`
	// DatabaseID is the database that contains a data source parent.
	DatabaseID string `json:"databaseId,omitempty"`
}

// Page is one Notion page with its property values. A database row is a page
// whose parent is a data source.
type Page struct {
	// ID is the page ID, the page's stable identifier.
	ID string `json:"id"`
	// URL opens the page in Notion; Notion does not keep it stable, so store ID instead.
	URL string `json:"url,omitempty"`
	// Title is the plain text of the page's title property.
	Title string `json:"title"`
	// Parent contains the page.
	Parent Parent `json:"parent"`
	// CreatedTime is the RFC 3339 creation time.
	CreatedTime string `json:"createdTime,omitempty"`
	// LastEditedTime is the RFC 3339 time of the last edit.
	LastEditedTime string `json:"lastEditedTime,omitempty"`
	// IsInTrash reports that the page was moved to the trash.
	IsInTrash bool `json:"inTrash"`
	// IsArchived reports that the page was archived, which is separate from the trash.
	IsArchived bool `json:"archived"`
	// Properties maps each property name to its value. Notion omits the
	// properties of a page the connection can see only partially.
	Properties map[string]PageProperty `json:"properties"`
}

// pageResource is the subset of Notion's page object the connector reads.
type pageResource struct {
	Object         string          `json:"object"`
	ID             string          `json:"id"`
	URL            string          `json:"url"`
	CreatedTime    string          `json:"created_time"`
	LastEditedTime string          `json:"last_edited_time"`
	InTrash        bool            `json:"in_trash"`
	IsArchived     bool            `json:"is_archived"`
	Parent         parentResource  `json:"parent"`
	Properties     json.RawMessage `json:"properties"`
}

type parentResource struct {
	Type         string `json:"type"`
	DataSourceID string `json:"data_source_id"`
	DatabaseID   string `json:"database_id"`
	PageID       string `json:"page_id"`
	BlockID      string `json:"block_id"`
}

var errUnusablePage = errors.New("Notion returned an unusable page")

// decodePage validates a page resource and converts it to the connector's Page.
func decodePage(resource pageResource) (Page, error) {
	id, err := ParseID(resource.ID)
	if err != nil || resource.Object != "page" {
		return Page{}, errUnusablePage
	}
	properties, err := decodeRawProperties(resource.Properties)
	if err != nil {
		return Page{}, errUnusablePage
	}
	return Page{
		ID: id, URL: resource.URL, Title: titleOfProperties(properties), Parent: decodeParent(resource.Parent),
		CreatedTime: resource.CreatedTime, LastEditedTime: resource.LastEditedTime,
		IsInTrash: resource.InTrash, IsArchived: resource.IsArchived, Properties: properties,
	}, nil
}

func decodePageBody(body []byte) (Page, error) {
	var resource pageResource
	if err := json.Unmarshal(body, &resource); err != nil {
		return Page{}, errUnusablePage
	}
	return decodePage(resource)
}

func decodeParent(resource parentResource) Parent {
	parent := Parent{Type: ParentType(resource.Type)}
	switch parent.Type {
	case ParentTypeDataSource:
		parent.ID = canonicalOrRaw(resource.DataSourceID)
		parent.DatabaseID = canonicalOrRaw(resource.DatabaseID)
	case ParentTypeDatabase:
		parent.ID = canonicalOrRaw(resource.DatabaseID)
	case ParentTypePage:
		parent.ID = canonicalOrRaw(resource.PageID)
	case ParentTypeBlock:
		parent.ID = canonicalOrRaw(resource.BlockID)
	}
	return parent
}

// canonicalOrRaw returns the dashed form of a Notion ID, or the value unchanged when it is not one.
func canonicalOrRaw(value string) string {
	if id, err := canonicalIDFromText(strings.TrimSpace(value)); err == nil {
		return id
	}
	return value
}
