// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package snowflake

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const getStatementResultOperationID = "getStatementResult"

// GetStatementResultInput identifies a submitted statement and the result partition to read.
type GetStatementResultInput struct {
	// StatementHandle is the handle submitStatement returned.
	StatementHandle string `json:"statementHandle"`
	// Partition is the zero-based result partition to read. Partition 0 also carries the column
	// metadata, the partition count, and the total row count; later partitions carry only rows.
	Partition int `json:"partition,omitempty"`
	// Columns must hold the Columns of the partition-0 Result when Partition is greater than zero,
	// because Snowflake sends no column metadata with later partitions. It is ignored for partition 0.
	Columns []Column `json:"columns,omitempty"`
}

// StatementResult is a statement's status and, once it finished, one bounded result partition.
type StatementResult struct {
	// StatementHandle is the statement that was read.
	StatementHandle string `json:"statementHandle"`
	// Columns lists the result columns in statement order; it is empty while the statement runs.
	Columns []Column `json:"columns"`
	// Rows maps each column name to its JSON value using the connector's documented type mapping.
	// It is empty, never nil, while the statement runs or when the partition holds no rows.
	Rows []map[string]any `json:"rows"`
	// Partition is the partition these rows came from.
	Partition int `json:"partition"`
	// PartitionCount is the number of result partitions; it is reported only by partition 0.
	PartitionCount int `json:"partitionCount,omitempty"`
	// TotalRowCount is the number of rows across every partition; it is reported only by partition 0.
	TotalRowCount *int64 `json:"totalRowCount,omitempty"`
	// Truncated reports that the partition held more rows or bytes than maxRows or maxResponseBytes
	// allow, so Rows holds only the rows that fit.
	Truncated bool `json:"truncated,omitempty"`
	// Stats reports the rows a DML statement inserted, updated, or deleted, when Snowflake sends them.
	Stats *StatementStats `json:"stats,omitempty"`
	// SubmittedAt is when Snowflake created the statement, when it reported that time.
	SubmittedAt *time.Time `json:"submittedAt,omitempty"`
}

// StatementStats counts the rows one DML statement changed.
type StatementStats struct {
	// RowsInserted is Snowflake's numRowsInserted.
	RowsInserted int64 `json:"rowsInserted"`
	// RowsUpdated is Snowflake's numRowsUpdated.
	RowsUpdated int64 `json:"rowsUpdated"`
	// RowsDeleted is Snowflake's numRowsDeleted.
	RowsDeleted int64 `json:"rowsDeleted"`
	// DuplicateRowsUpdated is Snowflake's numDuplicateRowsUpdated.
	DuplicateRowsUpdated int64 `json:"duplicateRowsUpdated"`
}

// GetStatementResultOperation implements the statement status and result partition read.
type GetStatementResultOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (GetStatementResultOperation) Definition() sdkgo.QueryDefinition {
	return GetStatementResultDefinition
}

// Invoke reads the statement once and never waits for it.
//
// A 202 selects running. A 200 streams the partition, keeps at most maxRows rows and
// maxResponseBytes of encoded rows, and selects completed or truncated. A 422 or 408 selects
// providerRejected with Snowflake's code and SQLSTATE in the Receipt, for a failed, canceled, or
// timed-out statement or an unknown handle. A 429, a 5xx, or a lost connection returns Retry,
// because a read has no side effect to repeat.
func (operation GetStatementResultOperation) Invoke(call sdkgo.Call, input GetStatementResultInput) sdkgo.QueryAttempt[StatementResult] {
	client := operation.client
	if !statementHandlePattern.MatchString(input.StatementHandle) {
		return resultDefect(sdkgo.FailureValidation, "statementHandle must be the UUID-shaped handle submitStatement returned")
	}
	if input.Partition < 0 {
		return resultDefect(sdkgo.FailureValidation, "partition cannot be negative")
	}
	if input.Partition > 0 {
		if len(input.Columns) == 0 {
			return resultDefect(sdkgo.FailureValidation, "columns from the partition-0 Result are required to read a later partition")
		}
		if err := validateUniqueColumnNames(input.Columns); err != nil {
			return resultDefect(sdkgo.FailureValidation, err.Error())
		}
	}
	target := statementsPath + "/" + input.StatementHandle
	if input.Partition > 0 {
		target += "?partition=" + strconv.Itoa(input.Partition)
	}
	response, err := client.send(call, sqlAPIRequest{method: http.MethodGet, target: target})
	if err != nil {
		return resultAttemptForFailure(requestFailure(err), sdkgo.Receipt{}, input)
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusAccepted:
		status := decodeStatementStatus(readErrorBody(response))
		running := emptyStatementResult(input)
		running.SubmittedAt = createdOnTime(status.CreatedOn)
		return sdkgo.NewQueryBranch(GetStatementResultBranchRunning, running, nil, client.receipt(call, input.StatementHandle, status))
	case http.StatusOK:
		return client.readCompletedPartition(call, response, input)
	default:
		failure := classifyUnsuccessfulResponse(response, readErrorBody(response), client.now())
		return resultAttemptForFailure(failure, client.receipt(call, input.StatementHandle, failure.status), input)
	}
}

func (client *Client) readCompletedPartition(call sdkgo.Call, response *http.Response, input GetStatementResultInput) sdkgo.QueryAttempt[StatementResult] {
	decoded, err := decodeStatementResponse(response, rowRetention{maxRows: client.maxRows, maxRawBytes: 2 * client.maxResponseBytes})
	if err != nil {
		if isUndecodableResponse(err) {
			failure := invalidResponseFailure(describeDecodeFailure(err))
			if errors.Is(err, errResponseBodyTooLarge) {
				failure.kind = sdkgo.FailureResponseTooLarge
			}
			return resultAttemptForFailure(failure, client.receipt(call, input.StatementHandle, statementStatus{}), input)
		}
		return resultAttemptForFailure(transportFailure(), sdkgo.Receipt{}, input)
	}
	receipt := client.receipt(call, input.StatementHandle, decoded.status)
	result := emptyStatementResult(input)
	result.SubmittedAt = createdOnTime(decoded.status.CreatedOn)
	columns := input.Columns
	if input.Partition == 0 {
		if decoded.metadata == nil || (decoded.metadata.Format != "" && decoded.metadata.Format != "jsonv2") {
			return resultAttemptForFailure(invalidResponseFailure("Snowflake returned partition 0 without jsonv2 column metadata"), receipt, input)
		}
		columns, err = describeColumns(decoded.metadata.RowType)
		if err != nil {
			return resultAttemptForFailure(invalidResponseFailure("Snowflake returned invalid column metadata"), receipt, input)
		}
		if err := validateUniqueColumnNames(columns); err != nil {
			return resultAttemptForFailure(classifiedFailure{disposition: dispositionDefect, kind: sdkgo.FailureValidation, message: err.Error()}, receipt, input)
		}
		result.PartitionCount = len(decoded.metadata.PartitionInfo)
		result.TotalRowCount = decoded.metadata.NumRows
	}
	rows, isByteLimitReached, err := convertRows(columns, decoded.rawRows, client.maxResponseBytes)
	if err != nil {
		var undecodable *undecodableValueError
		if errors.As(err, &undecodable) {
			return resultAttemptForFailure(invalidResponseFailure(undecodable.Error()), receipt, input)
		}
		return resultAttemptForFailure(invalidResponseFailure("Snowflake returned a row whose width differs from its columns"), receipt, input)
	}
	result.Columns, result.Rows = columns, rows
	result.Stats = describeStats(decoded.stats)
	if isByteLimitReached || decoded.hasSkippedRows {
		result.Truncated = true
		failure := classifiedFailure{
			kind:    sdkgo.FailureResponseTooLarge,
			message: "the partition held more rows or bytes than maxRows or maxResponseBytes allow",
		}
		return sdkgo.NewQueryBranch(GetStatementResultBranchTruncated, result, failurePointer(getStatementResultOperationID, failure), receipt)
	}
	return sdkgo.NewQueryBranch(GetStatementResultBranchCompleted, result, nil, receipt)
}

func resultAttemptForFailure(failure classifiedFailure, receipt sdkgo.Receipt, input GetStatementResultInput) sdkgo.QueryAttempt[StatementResult] {
	switch failure.disposition {
	case dispositionRetry:
		return sdkgo.NewQueryRetry[StatementResult](sdkFailure(getStatementResultOperationID, failure), failure.retryAfter)
	case dispositionInvalidResponse:
		return sdkgo.NewQueryBranch(GetStatementResultBranchInvalidResponse, emptyStatementResult(input), failurePointer(getStatementResultOperationID, failure), receipt)
	case dispositionDefect:
		return sdkgo.NewQueryBranch(GetStatementResultBranchDefect, emptyStatementResult(input), failurePointer(getStatementResultOperationID, failure), receipt)
	default:
		return sdkgo.NewQueryBranch(GetStatementResultBranchProviderRejected, emptyStatementResult(input), failurePointer(getStatementResultOperationID, failure), receipt)
	}
}

func resultDefect(kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[StatementResult] {
	failure := classifiedFailure{disposition: dispositionDefect, kind: kind, message: message}
	return resultAttemptForFailure(failure, sdkgo.Receipt{}, GetStatementResultInput{})
}

func emptyStatementResult(input GetStatementResultInput) StatementResult {
	return StatementResult{StatementHandle: input.StatementHandle, Columns: []Column{}, Rows: []map[string]any{}, Partition: input.Partition}
}

func describeStats(stats *resultSetStats) *StatementStats {
	if stats == nil {
		return nil
	}
	described := &StatementStats{}
	for _, field := range []struct {
		source *int64
		target *int64
	}{
		{stats.NumRowsInserted, &described.RowsInserted}, {stats.NumRowsUpdated, &described.RowsUpdated},
		{stats.NumRowsDeleted, &described.RowsDeleted}, {stats.NumDuplicateRowsUpdated, &described.DuplicateRowsUpdated},
	} {
		if field.source != nil {
			*field.target = *field.source
		}
	}
	return described
}
