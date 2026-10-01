// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package notion

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	databaseFailureSubject       = "database lookup"
	maximumListedDataSourceIDs   = 10
	maximumDatabaseDataSourceIDs = 100
)

// dataSourceSelection is a validated choice of exactly one data source ID or database ID.
type dataSourceSelection struct {
	dataSourceID string
	databaseID   string
}

type databaseResource struct {
	Object      string                    `json:"object"`
	ID          string                    `json:"id"`
	DataSources []dataSourceReferenceBody `json:"data_sources"`
}

type dataSourceReferenceBody struct {
	ID string `json:"id"`
}

// selectDataSource validates that exactly one of dataSourceID and databaseID is set.
func selectDataSource(dataSourceID string, databaseID string) (dataSourceSelection, error) {
	hasDataSource, hasDatabase := strings.TrimSpace(dataSourceID) != "", strings.TrimSpace(databaseID) != ""
	if hasDataSource == hasDatabase {
		return dataSourceSelection{}, errors.New("set exactly one of dataSourceId and databaseId")
	}
	if hasDataSource {
		id, err := parseFieldID(dataSourceID, "dataSourceId")
		return dataSourceSelection{dataSourceID: id}, err
	}
	id, err := parseFieldID(databaseID, "databaseId")
	return dataSourceSelection{databaseID: id}, err
}

// resolveDataSourceID returns the selected data source, or the only data source of the selected database.
func (client *Client) resolveDataSourceID(session *operationSession, operationID string, selection dataSourceSelection) (string, readClassification, notionResponse) {
	if selection.dataSourceID != "" {
		return selection.dataSourceID, readClassification{outcome: readSucceeded}, notionResponse{header: http.Header{}}
	}
	result := client.exchange(session, notionRequest{method: http.MethodGet, path: "/databases/" + url.PathEscape(selection.databaseID)})
	classification := client.classifyRead(operationID, databaseFailureSubject, result)
	if classification.outcome != readSucceeded {
		return "", classification, result.response
	}
	var database databaseResource
	if err := json.Unmarshal(result.response.body, &database); err != nil || database.Object != "database" || len(database.DataSources) > maximumDatabaseDataSourceIDs {
		return "", readClassification{outcome: readInvalid, failure: newFailure(operationID, sdkgo.FailureProtocol, "Notion returned an invalid database")}, result.response
	}
	var dataSourceIDs []string
	for _, reference := range database.DataSources {
		id, err := ParseID(reference.ID)
		if err != nil {
			return "", readClassification{outcome: readInvalid, failure: newFailure(operationID, sdkgo.FailureProtocol, "Notion returned a database with an invalid data source ID")}, result.response
		}
		dataSourceIDs = append(dataSourceIDs, id)
	}
	switch len(dataSourceIDs) {
	case 1:
		return dataSourceIDs[0], readClassification{outcome: readSucceeded}, result.response
	case 0:
		return "", readClassification{outcome: readDefect, failure: newFailure(operationID, sdkgo.FailureValidation,
			"the database has no data source the connection can use; set dataSourceId")}, result.response
	default:
		listed := dataSourceIDs[:min(len(dataSourceIDs), maximumListedDataSourceIDs)]
		return "", readClassification{outcome: readDefect, failure: newFailure(operationID, sdkgo.FailureValidation, fmt.Sprintf(
			"the database holds %d data sources; set dataSourceId to one of %s", len(dataSourceIDs), strings.Join(listed, ", ")))}, result.response
	}
}
