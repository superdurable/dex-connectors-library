// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package snowflake

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// maximumResponseBodyBytes bounds one decompressed SQL API body; rows beyond the retention limit are skipped, not kept.
const maximumResponseBodyBytes = 256 << 20

var (
	errResponseBodyTooLarge = errors.New("the Snowflake response exceeds the connector's size limit")
	errResponseShapeInvalid = errors.New("the Snowflake response is not a documented SQL API object")
)

// rowRetention bounds the raw rows one decode keeps; the zero value keeps none.
type rowRetention struct {
	maxRows     int
	maxRawBytes int
}

// statementResponse is the streamed, bounded content of one ResultSet, QueryStatus, or CancelStatus body.
type statementResponse struct {
	status         statementStatus
	metadata       *resultSetMetadata
	rawRows        [][]*string
	hasSkippedRows bool
	stats          *resultSetStats
}

// resultSetMetadata is the documented resultSetMetaData object, present only in partition 0.
type resultSetMetadata struct {
	NumRows       *int64            `json:"numRows"`
	Format        string            `json:"format"`
	RowType       []rowTypeColumn   `json:"rowType"`
	PartitionInfo []json.RawMessage `json:"partitionInfo"`
}

// rowTypeColumn is one documented resultSetMetaData.rowType entry.
type rowTypeColumn struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Length    *int64 `json:"length"`
	Precision *int64 `json:"precision"`
	Scale     *int64 `json:"scale"`
	Nullable  *bool  `json:"nullable"`
}

// resultSetStats is the documented DML stats object.
type resultSetStats struct {
	NumRowsInserted         *int64 `json:"numRowsInserted"`
	NumRowsUpdated          *int64 `json:"numRowsUpdated"`
	NumRowsDeleted          *int64 `json:"numRowsDeleted"`
	NumDuplicateRowsUpdated *int64 `json:"numDuplicateRowsUpdated"`
}

// decodeStatementResponse streams a SQL API object or, for a later partition, a bare row array.
func decodeStatementResponse(response *http.Response, retention rowRetention) (statementResponse, error) {
	body := io.Reader(&boundedBodyReader{reader: response.Body, remaining: maximumResponseBodyBytes})
	if strings.EqualFold(response.Header.Get("Content-Encoding"), "gzip") && !response.Uncompressed {
		decompressed, err := gzip.NewReader(response.Body)
		if err != nil {
			return statementResponse{}, errResponseShapeInvalid
		}
		defer decompressed.Close()
		body = &boundedBodyReader{reader: decompressed, remaining: maximumResponseBodyBytes}
	}
	decoder := json.NewDecoder(body)
	decoded := statementResponse{}
	token, err := decoder.Token()
	if err != nil {
		return statementResponse{}, err
	}
	switch token {
	case json.Delim('['):
		err = decoded.readRows(decoder, retention)
	case json.Delim('{'):
		err = decoded.readObjectFields(decoder, retention)
	default:
		return statementResponse{}, errResponseShapeInvalid
	}
	if err != nil {
		return statementResponse{}, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return statementResponse{}, errResponseShapeInvalid
	}
	return decoded, nil
}

func (decoded *statementResponse) readObjectFields(decoder *json.Decoder, retention rowRetention) error {
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		key, isKey := token.(string)
		if !isKey {
			return errResponseShapeInvalid
		}
		switch key {
		case "code":
			err = decoder.Decode(&decoded.status.Code)
		case "sqlState":
			err = decoder.Decode(&decoded.status.SQLState)
		case "statementHandle":
			err = decoder.Decode(&decoded.status.StatementHandle)
		case "createdOn":
			err = decoder.Decode(&decoded.status.CreatedOn)
		case "resultSetMetaData":
			err = decoder.Decode(&decoded.metadata)
		case "stats":
			err = decoder.Decode(&decoded.stats)
		case "data":
			err = decoded.readDataField(decoder, retention)
		default:
			err = skipJSONValue(decoder)
		}
		if err != nil {
			return err
		}
	}
	if _, err := decoder.Token(); err != nil {
		return err
	}
	// Codes, SQLSTATEs, and handles that do not match Snowflake's formats are dropped, never echoed.
	decoded.status = statementStatus{
		Code: keepIfMatches(decoded.status.Code, isSnowflakeCode), SQLState: keepIfMatches(decoded.status.SQLState, isSQLState),
		StatementHandle: keepIfMatches(decoded.status.StatementHandle, statementHandlePattern.MatchString),
		CreatedOn:       decoded.status.CreatedOn,
	}
	return nil
}

func (decoded *statementResponse) readDataField(decoder *json.Decoder, retention rowRetention) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if token == nil {
		return nil
	}
	if token != json.Delim('[') {
		return errResponseShapeInvalid
	}
	return decoded.readRows(decoder, retention)
}

// readRows keeps rows within retention and skips the rest; the opening bracket is already consumed.
func (decoded *statementResponse) readRows(decoder *json.Decoder, retention rowRetention) error {
	retainedBytes := 0
	for decoder.More() {
		if len(decoded.rawRows) >= retention.maxRows || decoded.hasSkippedRows {
			decoded.hasSkippedRows = true
			if err := skipJSONValue(decoder); err != nil {
				return err
			}
			continue
		}
		var row []*string
		if err := decoder.Decode(&row); err != nil {
			return err
		}
		retainedBytes += rawRowBytes(row)
		if retainedBytes > retention.maxRawBytes {
			decoded.hasSkippedRows = true
			continue
		}
		decoded.rawRows = append(decoded.rawRows, row)
	}
	_, err := decoder.Token()
	return err
}

// skipJSONValue consumes one value token by token, so a skipped row array is never buffered.
func skipJSONValue(decoder *json.Decoder) error {
	depth := 0
	for {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		switch token {
		case json.Delim('['), json.Delim('{'):
			depth++
		case json.Delim(']'), json.Delim('}'):
			depth--
		}
		if depth == 0 {
			return nil
		}
	}
}

func rawRowBytes(row []*string) int {
	total := len("[]")
	for _, value := range row {
		total += len(",")
		if value == nil {
			total += len("null")
			continue
		}
		total += len(*value) + len(`""`)
	}
	return total
}

func keepIfMatches(value string, isValid func(string) bool) string {
	if isValid(value) {
		return value
	}
	return ""
}

// isUndecodableResponse separates a malformed or oversized body from a connection lost mid-read.
func isUndecodableResponse(err error) bool {
	var syntaxError *json.SyntaxError
	var typeError *json.UnmarshalTypeError
	return errors.Is(err, errResponseBodyTooLarge) || errors.Is(err, errResponseShapeInvalid) ||
		errors.Is(err, io.ErrUnexpectedEOF) || errors.As(err, &syntaxError) || errors.As(err, &typeError) ||
		errors.Is(err, gzip.ErrHeader) || errors.Is(err, gzip.ErrChecksum)
}

// boundedBodyReader fails instead of reading past its limit.
type boundedBodyReader struct {
	reader    io.Reader
	remaining int64
}

// Read reads at most the remaining budget and reports errResponseBodyTooLarge once it is spent.
func (bounded *boundedBodyReader) Read(buffer []byte) (int, error) {
	if bounded.remaining <= 0 {
		probe := make([]byte, 1)
		if count, _ := bounded.reader.Read(probe); count > 0 {
			return 0, errResponseBodyTooLarge
		}
		return 0, io.EOF
	}
	if int64(len(buffer)) > bounded.remaining {
		buffer = buffer[:bounded.remaining]
	}
	count, err := bounded.reader.Read(buffer)
	bounded.remaining -= int64(count)
	return count, err
}

// describeDecodeFailure names why a body was undecodable without repeating any of it.
func describeDecodeFailure(err error) string {
	if errors.Is(err, errResponseBodyTooLarge) {
		return fmt.Sprintf("the Snowflake response exceeds %d bytes", maximumResponseBodyBytes)
	}
	return "Snowflake returned a response the connector cannot decode"
}
