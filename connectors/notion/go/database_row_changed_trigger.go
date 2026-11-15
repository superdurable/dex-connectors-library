// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package notion

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// DatabaseRowChangeKind is the derived kind of one databaseRowChanged delivery.
type DatabaseRowChangeKind string

const (
	// DatabaseRowCreated is a row whose last edited time equals its created time: Notion reports no change
	// type, and a row edited within the same minute it was created still reads as created.
	DatabaseRowCreated DatabaseRowChangeKind = "created"
	// DatabaseRowUpdated is any other edit to a row's properties or content.
	DatabaseRowUpdated DatabaseRowChangeKind = "updated"
)

// DatabaseRowChangedTriggerConfiguration is the databaseRowChanged binding configuration that the
// dataSourcePicker unit saves in Dex Web. Changing the data source of a binding that already polled makes
// its next poll record a new baseline for the new data source.
type DatabaseRowChangedTriggerConfiguration struct {
	// DataSourceID is the watched data source, a Notion ID such as
	// 2a4d6f80-1b3c-4d5e-8f70-112233445566; the picker resolves it from a database link. It is required.
	DataSourceID string `json:"dataSourceId,omitempty"`
	// DataSourceName is the picked data source's name, kept for display only.
	DataSourceName string `json:"dataSourceName,omitempty"`
}

// Validate requires a Notion data source ID; a binding without one has nothing to watch.
func (configuration DatabaseRowChangedTriggerConfiguration) Validate() error {
	if strings.TrimSpace(configuration.DataSourceID) == "" {
		return errors.New("dataSourceId is required; pick the watched Notion data source in Dex Web")
	}
	if _, err := ParseID(configuration.DataSourceID); err != nil {
		return fmt.Errorf("dataSourceId: %w", err)
	}
	return nil
}

// watchedDataSourceID returns the dashed data source ID; call it only after Validate.
func (configuration DatabaseRowChangedTriggerConfiguration) watchedDataSourceID() string {
	id, err := ParseID(configuration.DataSourceID)
	if err != nil {
		panic(fmt.Errorf("notion databaseRowChanged configuration was not validated: %w", err))
	}
	return id
}

// DatabaseRowChangedEvent is one row created or edited in the watched data source. Edits of one row that
// Notion reports with the same last edited time arrive once, and a row moved to the trash is not delivered.
type DatabaseRowChangedEvent struct {
	// DataSourceID is the watched data source.
	DataSourceID string `json:"dataSourceId"`
	// ChangeKind is the derived change kind; see DatabaseRowChangeKind.
	ChangeKind DatabaseRowChangeKind `json:"changeKind"`
	// Row is the row with its property values as the data source query returned it.
	Row Page `json:"row"`
}

// newDatabaseRowChangedTriggerEvent converts one queried row. Its ID is a pure function of the row version,
// so a refetch after a crash yields the same ID.
func newDatabaseRowChangedTriggerEvent(dataSourceID string, row Page, lastEditedTime time.Time) sdkgo.TriggerEvent[DatabaseRowChangedEvent] {
	return sdkgo.TriggerEvent[DatabaseRowChangedEvent]{
		ID:         databaseRowChangeID(dataSourceID, row.ID, lastEditedTime),
		OccurredAt: lastEditedTime,
		Payload:    DatabaseRowChangedEvent{DataSourceID: dataSourceID, ChangeKind: databaseRowChangeKind(row), Row: row},
	}
}

// databaseRowChangeID hashes the data source, row, and last edited time into a short stable ID.
func databaseRowChangeID(dataSourceID string, rowID string, lastEditedTime time.Time) string {
	digest := sha256.Sum256([]byte(strings.Join([]string{dataSourceID, rowID, lastEditedTime.UTC().Format(time.RFC3339Nano)}, "\x00")))
	return databaseRowChangeIDPrefix + hex.EncodeToString(digest[:16])
}

const databaseRowChangeIDPrefix = "notion-row-"

func databaseRowChangeKind(row Page) DatabaseRowChangeKind {
	created, createdErr := time.Parse(time.RFC3339Nano, row.CreatedTime)
	edited, editedErr := time.Parse(time.RFC3339Nano, row.LastEditedTime)
	if createdErr == nil && editedErr == nil && created.Equal(edited) {
		return DatabaseRowCreated
	}
	return DatabaseRowUpdated
}

func databaseRowChangedTriggerConfigurationError(err error) error {
	return fmt.Errorf("notion databaseRowChanged configuration: %w", err)
}
