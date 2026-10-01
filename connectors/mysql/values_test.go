// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mysql

import (
	"database/sql/driver"
	"encoding/json"
	"io"
	"math"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDecodeColumnValueMapsServerValuesToExactJSONValues(t *testing.T) {
	for name, testCase := range map[string]struct {
		typeName string
		value    driver.Value
		want     any
	}{
		"null":                     {"VARCHAR", nil, nil},
		"tinyint":                  {"TINYINT", int64(-1), int64(-1)},
		"unsigned int":             {"UNSIGNED INT", int64(4294967295), int64(4294967295)},
		"year":                     {"YEAR", int64(2026), int64(2026)},
		"bigint":                   {"BIGINT", int64(-9007199254740993), "-9007199254740993"},
		"unsigned bigint":          {"UNSIGNED BIGINT", int64(9007199254740993), "9007199254740993"},
		"unsigned bigint as text":  {"UNSIGNED BIGINT", []byte("18446744073709551615"), "18446744073709551615"},
		"float":                    {"FLOAT", float32(0.1), 0.1},
		"double":                   {"DOUBLE", 1e300, 1e300},
		"decimal":                  {"DECIMAL", []byte("-0.000000001"), "-0.000000001"},
		"varchar":                  {"VARCHAR", []byte("héllo"), "héllo"},
		"enum":                     {"ENUM", []byte("open"), "open"},
		"blob":                     {"BLOB", []byte{0, 1, 255}, "AAH/"},
		"varbinary":                {"VARBINARY", []byte("raw"), "cmF3"},
		"bit":                      {"BIT", []byte{0x02, 0x01}, "513"},
		"empty bit":                {"BIT", []byte{}, "0"},
		"json":                     {"JSON", []byte(`{"a": 1.10}`), json.RawMessage(`{"a": 1.10}`)},
		"datetime":                 {"DATETIME", []byte("2026-01-01 12:34:56.500000"), "2026-01-01T12:34:56.5"},
		"datetime without seconds": {"DATETIME", []byte("2026-01-01 00:00:00"), "2026-01-01T00:00:00"},
		"zero datetime":            {"DATETIME", []byte("0000-00-00 00:00:00"), "0000-00-00 00:00:00"},
		"partial date":             {"DATE", []byte("2026-00-00"), "2026-00-00"},
		"timestamp":                {"TIMESTAMP", []byte("2026-01-01 00:00:00.123456"), "2026-01-01T00:00:00.123456Z"},
		"date":                     {"DATE", []byte("2026-02-03"), "2026-02-03"},
		"time":                     {"TIME", []byte("-838:59:59.000000"), "-838:59:59.000000"},
		"null type":                {"NULL", []byte("x"), nil},
		"unknown text type":        {"", []byte("text"), "text"},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := decodeColumnValue(testCase.typeName, testCase.value)
			require.NoError(t, err)
			require.Equal(t, testCase.want, got)
		})
	}
}

func TestDecodeColumnValueRejectsValuesThatDoNotMatchTheirType(t *testing.T) {
	for name, testCase := range map[string]struct {
		typeName string
		value    driver.Value
	}{
		"text in an int":          {"INT", []byte("1")},
		"negative unsigned":       {"UNSIGNED BIGINT", int64(-1)},
		"letters in a decimal":    {"DECIMAL", []byte("1e5")},
		"non-finite float":        {"FLOAT", float32(math.Inf(1))},
		"NaN double":              {"DOUBLE", math.NaN()},
		"invalid json":            {"JSON", []byte(`{"a":`)},
		"garbled datetime":        {"DATETIME", []byte("yesterday")},
		"datetime as a date":      {"DATE", []byte("2026-01-01 00:00:00")},
		"garbled time":            {"TIME", []byte("noon")},
		"invalid UTF-8 text":      {"VARCHAR", []byte{0xff}},
		"number for a text value": {"VARCHAR", int64(1)},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := decodeColumnValue(testCase.typeName, testCase.value)
			require.Error(t, err)
		})
	}
}

func TestDecodedValuesDoNotAliasTheDriverBuffer(t *testing.T) {
	buffer := []byte(`{"a":1}`)
	decoded, err := decodeColumnValue("JSON", buffer)
	require.NoError(t, err)
	buffer[2] = 'z'
	require.Equal(t, json.RawMessage(`{"a":1}`), decoded)
}

// scriptedRows is a driver.Rows that returns fixed rows and charges each one to a meter as if read from the network.
type scriptedRows struct {
	meter       *connectionMeter
	rows        [][]driver.Value
	bytesPerRow int
}

func (rows *scriptedRows) Columns() []string { return []string{"id"} }

func (rows *scriptedRows) ColumnTypeDatabaseTypeName(int) string { return "INT" }

func (rows *scriptedRows) Close() error { return nil }

func (rows *scriptedRows) Next(destination []driver.Value) error {
	if len(rows.rows) == 0 {
		return io.EOF
	}
	if !rows.meter.recordRead(rows.bytesPerRow) {
		return errReadBudgetExceeded
	}
	copy(destination, rows.rows[0])
	rows.rows = rows.rows[1:]
	return nil
}

func TestReadBoundedRowsRestoresTheControlBudgetForLaterReplies(t *testing.T) {
	meter := newConnectionMeter(time.Time{})
	rows := &scriptedRows{meter: meter, rows: [][]driver.Value{{int64(1)}, {int64(2)}}, bytesPerRow: 40000}
	read, err := readBoundedRows(rows, meter, 10, 100000)
	require.NoError(t, err)
	require.False(t, read.isTruncated)
	require.Len(t, read.rows, 2)
	require.Equal(t, int64(controlReadBudgetBytes), meter.readBudget, "ROW_COUNT() and COMMIT replies are not charged to the rows")
	require.Zero(t, meter.bytesReadSinceArmed)
	require.True(t, meter.recordRead(protocolOverheadBytes), "a reply after rows that nearly filled their budget still reads")

	tooLarge := &scriptedRows{meter: meter, rows: [][]driver.Value{{int64(1)}, {int64(2)}}, bytesPerRow: 1 << 20}
	read, err = readBoundedRows(tooLarge, meter, 10, 1024)
	require.NoError(t, err)
	require.True(t, read.isTruncated, "a row beyond the wire budget truncates")
	require.True(t, meter.hasExceededReadBudget(), "the connection stays refused after truncation")
}

func TestConnectionMeterRefusesReadsBeyondTheArmedBudget(t *testing.T) {
	serverSide, clientSide := net.Pipe()
	t.Cleanup(func() { require.NoError(t, serverSide.Close()) })
	meter := newConnectionMeter(time.Time{})
	connection := &meteredConnection{Conn: clientSide, meter: meter}
	go func() {
		// The pipe write fails once the client stops reading, which ends this goroutine.
		_, _ = serverSide.Write(make([]byte, 64))
	}()
	buffer := make([]byte, 16)
	count, err := connection.Read(buffer)
	require.NoError(t, err, "a fresh meter allows the control budget")
	unarmedCount := count
	meter.armReadBudget(20)
	total := 0
	for err == nil {
		count, err = connection.Read(buffer)
		total += count
	}
	require.ErrorIs(t, err, errReadBudgetExceeded)
	require.True(t, meter.hasExceededReadBudget())
	require.LessOrEqual(t, total, 20+len(buffer), "the driver never receives more than one read past the budget")
	require.Positive(t, unarmedCount)
	_, err = connection.Read(buffer)
	require.ErrorIs(t, err, errReadBudgetExceeded, "the connection stays refused")
	require.NoError(t, connection.Close())
	require.True(t, meter.isClosed())
}
