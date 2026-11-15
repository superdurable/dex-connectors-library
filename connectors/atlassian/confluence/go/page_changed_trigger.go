// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package confluence

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// PageChangeKind is the derived kind of one pageChanged delivery.
type PageChangeKind string

const (
	// PageChangeCreated is a page whose delivered version is 1, its first published version.
	PageChangeCreated PageChangeKind = "created"
	// PageChangeUpdated is a page whose delivered version is 2 or later, including a page created and
	// edited again before the next poll read it.
	PageChangeUpdated PageChangeKind = "updated"
)

// PageChangedTriggerConfiguration is the pageChanged binding configuration that the space picker saves in
// Dex Web. Changing the space of a binding that already polled makes its next poll record a new baseline.
type PageChangedTriggerConfiguration struct {
	// SpaceID is the picked space's numeric ID, kept for display only.
	SpaceID string `json:"spaceId,omitempty"`
	// SpaceKey limits the Trigger to pages in this space, such as OPS or ~5b10ac8d82e05b22cc7d4ef5; blank
	// watches every space the connection can view.
	SpaceKey string `json:"spaceKey,omitempty"`
	// SpaceName is the picked space's display name, kept for display only.
	SpaceName string `json:"spaceName,omitempty"`
}

// Validate rejects a space key that is not a Confluence space key; blank is valid.
func (configuration PageChangedTriggerConfiguration) Validate() error {
	if configuration.SpaceKey != "" && !spaceKeyPattern.MatchString(configuration.SpaceKey) {
		return errors.New("pageChanged spaceKey must be a Confluence space key such as OPS, without surrounding whitespace")
	}
	return nil
}

// PageChangedEvent is one page created or updated since the previous poll. A page edited several times
// between two polls arrives once, at the version current when the poll read it.
type PageChangedEvent struct {
	// CloudID is the Confluence site the page belongs to.
	CloudID string `json:"cloudId"`
	// ChangeKind is the derived change kind; see PageChangeKind.
	ChangeKind PageChangeKind `json:"changeKind"`
	// Page identifies the page and its delivered version. Read its body with getPage.
	Page PageSummary `json:"page"`
}

// newPageChangedTriggerEvent converts one search result. Its ID is a pure function of the site, page, and
// version, so a refetch after a crash yields the same ID.
func newPageChangedTriggerEvent(cloudID string, page PageSummary) sdkgo.TriggerEvent[PageChangedEvent] {
	kind := PageChangeUpdated
	if page.VersionNumber == 1 {
		kind = PageChangeCreated
	}
	return sdkgo.TriggerEvent[PageChangedEvent]{
		ID:         pageChangeID(cloudID, page.ID, page.VersionNumber),
		OccurredAt: page.LastModifiedAt.UTC(),
		Payload:    PageChangedEvent{CloudID: cloudID, ChangeKind: kind, Page: page},
	}
}

func pageChangeID(cloudID string, pageID string, versionNumber int) string {
	digest := sha256.Sum256([]byte(strings.Join([]string{cloudID, pageID, strconv.Itoa(versionNumber)}, "\x00")))
	return "confluence-page-" + hex.EncodeToString(digest[:16])
}

func pageChangedTriggerConfigurationError(err error) error {
	return fmt.Errorf("confluence pageChanged configuration: %w", err)
}
