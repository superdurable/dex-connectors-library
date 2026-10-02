// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package excel

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	tableColumnsQuery   = "?$select=id,index,name"
	rangeValuesQuery    = "?$select=address,rowCount,columnCount,values"
	rangeValuesAndTexts = "?$select=address,rowCount,columnCount,values,text"
)

// tableColumn is one column of an Excel table, in table order.
type tableColumn struct {
	id    string
	index int
	name  string
}

type tableColumnsResponse struct {
	Value *[]struct {
		ID    *string `json:"id"`
		Index *int    `json:"index"`
		Name  *string `json:"name"`
	} `json:"value"`
}

// rangeResource is the validated part of one Graph workbookRange response.
type rangeResource struct {
	address     string
	rowCount    int64
	columnCount int64
	values      [][]CellValue
	texts       [][]string
}

type rangeResponse struct {
	Address     string         `json:"address"`
	RowCount    *int64         `json:"rowCount"`
	ColumnCount *int64         `json:"columnCount"`
	Values      *[][]CellValue `json:"values"`
	Text        *[][]string    `json:"text"`
}

// cellCount returns the number of cells the range response covers.
func (resource rangeResource) cellCount() int64 { return resource.rowCount * resource.columnCount }

// errRangeValuesOmitted reports a range response without values, as Excel answers for ranges above its cell limit.
var errRangeValuesOmitted = errors.New("Excel returned no cell values for the range")

// readTableColumns reads the table's column IDs, positions, and names.
func (client *Client) readTableColumns(
	call sdkgo.Call,
	credential *Credentials,
	branches readBranches,
	driveID, workbookID, table string,
) ([]tableColumn, graphResponse, *readOutcome) {
	response, outcome := client.sendRead(call, credential, branches, graphRequest{
		method: http.MethodGet, target: tableURL(driveID, workbookID, table) + "/columns" + tableColumnsQuery,
		responseLimit: client.maxResponseBytes,
	})
	if outcome != nil {
		return nil, response, outcome
	}
	columns, err := decodeTableColumns(response.body)
	if err != nil {
		return nil, response, &readOutcome{
			branch: branches.invalidResponse, receipt: client.receipt(call, response.requestID),
			failure: excelFailure(sdkgo.FailureProtocol, branches.operationID, "provider returned an invalid table columns response"),
		}
	}
	return columns, response, nil
}

// decodeTableColumns validates that the columns have IDs, unique non-empty names, and contiguous positions.
func decodeTableColumns(body []byte) ([]tableColumn, error) {
	var response tableColumnsResponse
	if err := json.Unmarshal(body, &response); err != nil || response.Value == nil || len(*response.Value) == 0 {
		return nil, errors.New("table columns response is invalid")
	}
	columns := make([]tableColumn, 0, len(*response.Value))
	seenNames := map[string]bool{}
	for _, column := range *response.Value {
		if column.ID == nil || *column.ID == "" || column.Index == nil || column.Name == nil || *column.Name == "" || seenNames[*column.Name] {
			return nil, errors.New("table column is invalid")
		}
		seenNames[*column.Name] = true
		columns = append(columns, tableColumn{id: *column.ID, index: *column.Index, name: *column.Name})
	}
	sort.Slice(columns, func(i, j int) bool { return columns[i].index < columns[j].index })
	for position, column := range columns {
		if column.index != position {
			return nil, errors.New("table column positions are not contiguous")
		}
	}
	return columns, nil
}

// decodeRange validates one workbookRange response; isTextRequired also requires the text grid.
func decodeRange(body []byte, isTextRequired bool) (rangeResource, error) {
	var response rangeResponse
	if err := json.Unmarshal(body, &response); err != nil || response.RowCount == nil || response.ColumnCount == nil ||
		*response.RowCount < 0 || *response.ColumnCount < 0 {
		return rangeResource{}, errors.New("range response is invalid")
	}
	resource := rangeResource{address: response.Address, rowCount: *response.RowCount, columnCount: *response.ColumnCount}
	if response.Values == nil {
		return resource, errRangeValuesOmitted
	}
	if !hasGridShape(len(*response.Values), resource, func(row int) int { return len((*response.Values)[row]) }) {
		return rangeResource{}, errors.New("range values do not match the range size")
	}
	resource.values = *response.Values
	if !isTextRequired {
		return resource, nil
	}
	if response.Text == nil || !hasGridShape(len(*response.Text), resource, func(row int) int { return len((*response.Text)[row]) }) {
		return rangeResource{}, errors.New("range text does not match the range size")
	}
	resource.texts = *response.Text
	return resource, nil
}

func hasGridShape(rowCount int, resource rangeResource, columnCountOfRow func(int) int) bool {
	if int64(rowCount) != resource.rowCount {
		return false
	}
	for row := 0; row < rowCount; row++ {
		if int64(columnCountOfRow(row)) != resource.columnCount {
			return false
		}
	}
	return true
}

// columnDataBodyRangeURL reads one column's data cells, without its header and total cells.
func columnDataBodyRangeURL(driveID, workbookID, table, columnID string) string {
	return tableURL(driveID, workbookID, table) + "/columns/" + url.PathEscape(columnID) + "/dataBodyRange" + rangeValuesQuery
}

// tableDataBodyRangeURL reads the table's data cells, without its header and total rows.
func tableDataBodyRangeURL(driveID, workbookID, table string) string {
	return tableURL(driveID, workbookID, table) + "/dataBodyRange" + rangeValuesQuery
}
