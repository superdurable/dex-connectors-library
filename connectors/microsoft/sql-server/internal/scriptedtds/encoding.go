// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package scriptedtds

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
)

// TDS packet types, token types, and status flags from Microsoft's MS-TDS specification.
const (
	packetSQLBatch           byte = 0x01
	packetRPC                byte = 0x03
	packetReply              byte = 0x04
	packetAttention          byte = 0x06
	packetTransactionManager byte = 0x0e
	packetLogin7             byte = 0x10
	packetPrelogin           byte = 0x12

	statusEndOfMessage byte = 0x01

	tokenReturnStatus byte = 0x79
	tokenColMetadata  byte = 0x81
	tokenError        byte = 0xaa
	tokenInfo         byte = 0xab
	tokenLoginAck     byte = 0xad
	tokenRow          byte = 0xd1
	tokenEnvChange    byte = 0xe3
	tokenDone         byte = 0xfd
	tokenDoneProc     byte = 0xfe
	tokenDoneInProc   byte = 0xff

	doneMore      uint16 = 0x01
	doneError     uint16 = 0x02
	doneCount     uint16 = 0x10
	doneAttention uint16 = 0x20

	commandSelect uint16 = 0xc1
	commandInsert uint16 = 0xc3

	environmentDatabase    byte   = 1
	environmentLanguage    byte   = 2
	environmentPacketSize  byte   = 4
	environmentCollation   byte   = 7
	environmentBegin       byte   = 8
	environmentCommit      byte   = 9
	environmentRollback    byte   = 10
	environmentRouting     byte   = 20
	transactionBegin       uint16 = 5
	transactionCommit      uint16 = 7
	transactionRollback    uint16 = 8
	executeSQLProcedureID  uint16 = 10
	preloginEncryption     byte   = 1
	preloginTerminator     byte   = 0xff
	packetSize                    = 4096
	tdsVersion74           uint32 = 0x74000004
	collationLatin1General        = "\x09\x04\xd0\x00\x34"
)

// TDS type identifiers the server encodes and decodes.
const (
	typeNull           byte = 0x1f
	typeIntN           byte = 0x26
	typeGUID           byte = 0x24
	typeDateN          byte = 0x28
	typeTimeN          byte = 0x29
	typeDateTime2N     byte = 0x2a
	typeDateTimeOffset byte = 0x2b
	typeSQLVariant     byte = 0x62
	typeBitN           byte = 0x68
	typeDecimalN       byte = 0x6a
	typeFloatN         byte = 0x6d
	typeMoneyN         byte = 0x6e
	typeDateTimeN      byte = 0x6f
	typeBigVarBinary   byte = 0xa5
	typeBigVarChar     byte = 0xa7
	typeBigBinary      byte = 0xad
	typeNVarChar       byte = 0xe7
	typeXML            byte = 0xf1
	typeUDT            byte = 0xf0
	typeInt4           byte = 0x38
)

const (
	lengthMax          = 0xffff
	plpNull     uint64 = math.MaxUint64
	plpUnknown  uint64 = math.MaxUint64 - 1
	defaultTime        = 7
)

// tokenWriter accumulates one reply message.
type tokenWriter struct{ buffer []byte }

func (writer *tokenWriter) byte(value byte) { writer.buffer = append(writer.buffer, value) }
func (writer *tokenWriter) uint16(value uint16) {
	writer.buffer = binary.LittleEndian.AppendUint16(writer.buffer, value)
}
func (writer *tokenWriter) uint32(value uint32) {
	writer.buffer = binary.LittleEndian.AppendUint32(writer.buffer, value)
}
func (writer *tokenWriter) uint64(value uint64) {
	writer.buffer = binary.LittleEndian.AppendUint64(writer.buffer, value)
}
func (writer *tokenWriter) bytes(value []byte) { writer.buffer = append(writer.buffer, value...) }

// byteText writes a B_VARCHAR: a character count byte and UCS-2 text.
func (writer *tokenWriter) byteText(value string) {
	encoded := encodeUCS2(value)
	writer.byte(byte(len(encoded) / 2))
	writer.bytes(encoded)
}

// shortText writes a US_VARCHAR: a character count and UCS-2 text.
func (writer *tokenWriter) shortText(value string) {
	encoded := encodeUCS2(value)
	writer.uint16(uint16(len(encoded) / 2))
	writer.bytes(encoded)
}

func (writer *tokenWriter) done(token byte, status uint16, command uint16, rowCount uint64) {
	writer.byte(token)
	writer.uint16(status)
	writer.uint16(command)
	writer.uint64(rowCount)
}

func (writer *tokenWriter) environmentChange(kind byte, newValue []byte, oldValue []byte) {
	writer.byte(tokenEnvChange)
	writer.uint16(uint16(1 + 1 + len(newValue) + 1 + len(oldValue)))
	writer.byte(kind)
	writer.byte(byte(len(newValue)))
	writer.bytes(newValue)
	writer.byte(byte(len(oldValue)))
	writer.bytes(oldValue)
}

func (writer *tokenWriter) textEnvironmentChange(kind byte, newValue string, oldValue string) {
	newEncoded, oldEncoded := encodeUCS2(newValue), encodeUCS2(oldValue)
	writer.byte(tokenEnvChange)
	writer.uint16(uint16(1 + 1 + len(newEncoded) + 1 + len(oldEncoded)))
	writer.byte(kind)
	writer.byte(byte(len(newEncoded) / 2))
	writer.bytes(newEncoded)
	writer.byte(byte(len(oldEncoded) / 2))
	writer.bytes(oldEncoded)
}

func (writer *tokenWriter) message(token byte, serverError ServerError) {
	body := tokenWriter{}
	body.uint32(uint32(serverError.Number))
	body.byte(serverError.State)
	body.byte(serverError.Severity)
	body.shortText(serverError.Message)
	body.byteText("scripted")
	body.byteText("")
	body.uint32(1)
	writer.byte(token)
	writer.uint16(uint16(len(body.buffer)))
	writer.bytes(body.buffer)
}

func (writer *tokenWriter) loginAcknowledgement(productVersion string) error {
	parts := strings.Split(productVersion, ".")
	if len(parts) < 3 {
		return fmt.Errorf("product version %q needs major.minor.build", productVersion)
	}
	numbers := make([]int, 3)
	for index := range numbers {
		number, err := strconv.Atoi(parts[index])
		if err != nil {
			return err
		}
		numbers[index] = number
	}
	body := tokenWriter{}
	body.byte(1)
	body.buffer = binary.BigEndian.AppendUint32(body.buffer, tdsVersion74)
	body.byteText("Microsoft SQL Server")
	body.bytes([]byte{byte(numbers[0]), byte(numbers[1]), byte(numbers[2] >> 8), byte(numbers[2])})
	writer.byte(tokenLoginAck)
	writer.uint16(uint16(len(body.buffer)))
	writer.bytes(body.buffer)
	return nil
}

func (writer *tokenWriter) columnMetadata(columns []Column) error {
	writer.byte(tokenColMetadata)
	writer.uint16(uint16(len(columns)))
	for _, column := range columns {
		writer.uint32(0)
		writer.uint16(0x0001)
		if err := writer.typeInfo(column); err != nil {
			return err
		}
		writer.byteText(column.Name)
	}
	return nil
}

func (writer *tokenWriter) typeInfo(column Column) error {
	switch column.Type {
	case TypeTinyInt, TypeSmallInt, TypeInt, TypeBigInt:
		writer.byte(typeIntN)
		writer.byte(byte(integerSize(column.Type)))
	case TypeBit:
		writer.byte(typeBitN)
		writer.byte(1)
	case TypeReal:
		writer.byte(typeFloatN)
		writer.byte(4)
	case TypeFloat:
		writer.byte(typeFloatN)
		writer.byte(8)
	case TypeDecimal:
		writer.byte(typeDecimalN)
		writer.byte(byte(1 + decimalMagnitudeSize(column.Precision)))
		writer.byte(column.Precision)
		writer.byte(column.Scale)
	case TypeMoney:
		writer.byte(typeMoneyN)
		writer.byte(8)
	case TypeSmallMoney:
		writer.byte(typeMoneyN)
		writer.byte(4)
	case TypeNVarChar:
		writer.byte(typeNVarChar)
		writer.uint16(uint16(2 * lengthOrDefault(column.Length, 4000)))
		writer.bytes([]byte(collationLatin1General))
	case TypeNVarCharMax:
		writer.byte(typeNVarChar)
		writer.uint16(lengthMax)
		writer.bytes([]byte(collationLatin1General))
	case TypeVarChar:
		writer.byte(typeBigVarChar)
		writer.uint16(uint16(lengthOrDefault(column.Length, 8000)))
		writer.bytes([]byte(collationLatin1General))
	case TypeVarBinary:
		writer.byte(typeBigVarBinary)
		writer.uint16(uint16(lengthOrDefault(column.Length, 8000)))
	case TypeVarBinaryMax:
		writer.byte(typeBigVarBinary)
		writer.uint16(lengthMax)
	case TypeBinary:
		writer.byte(typeBigBinary)
		writer.uint16(uint16(lengthOrDefault(column.Length, 8)))
	case TypeUniqueIdentifier:
		writer.byte(typeGUID)
		writer.byte(16)
	case TypeDate:
		writer.byte(typeDateN)
	case TypeTime:
		writer.byte(typeTimeN)
		writer.byte(temporalScale(column))
	case TypeDateTime2:
		writer.byte(typeDateTime2N)
		writer.byte(temporalScale(column))
	case TypeDateTimeOffset:
		writer.byte(typeDateTimeOffset)
		writer.byte(temporalScale(column))
	case TypeDateTime:
		writer.byte(typeDateTimeN)
		writer.byte(8)
	case TypeSmallDateTime:
		writer.byte(typeDateTimeN)
		writer.byte(4)
	case TypeXML:
		writer.byte(typeXML)
		writer.byte(0)
	case TypeSQLVariant:
		writer.byte(typeSQLVariant)
		writer.uint32(8009)
	case TypeGeography:
		writer.byte(typeUDT)
		writer.uint16(lengthMax)
		writer.byteText("master")
		writer.byteText("sys")
		writer.byteText("geography")
		writer.shortText("Microsoft.SqlServer.Types.SqlGeography, Microsoft.SqlServer.Types")
	default:
		return fmt.Errorf("unsupported scripted column type %d", column.Type)
	}
	return nil
}

func (writer *tokenWriter) row(columns []Column, values []any) error {
	if len(values) != len(columns) {
		return fmt.Errorf("row has %d values for %d columns", len(values), len(columns))
	}
	writer.byte(tokenRow)
	for index, column := range columns {
		if err := writer.value(column, values[index]); err != nil {
			return fmt.Errorf("column %q: %w", column.Name, err)
		}
	}
	return nil
}

// value writes one column value; nil is SQL NULL in every type's own encoding.
func (writer *tokenWriter) value(column Column, value any) error {
	if isPartiallyLengthPrefixed(column.Type) {
		return writer.partiallyLengthPrefixedValue(column, value)
	}
	if isShortLengthPrefixed(column.Type) {
		encoded, err := encodeVariableValue(column, value)
		if err != nil {
			return err
		}
		if encoded == nil {
			writer.uint16(lengthMax)
			return nil
		}
		writer.uint16(uint16(len(encoded)))
		writer.bytes(encoded)
		return nil
	}
	if column.Type == TypeSQLVariant {
		if value == nil {
			writer.uint32(0)
			return nil
		}
		integer, isInteger := value.(int64)
		if !isInteger {
			return errors.New("a scripted sql_variant holds an int64")
		}
		writer.uint32(2 + 4)
		writer.byte(typeInt4)
		writer.byte(0)
		writer.uint32(uint32(int32(integer)))
		return nil
	}
	if value == nil {
		writer.byte(0)
		return nil
	}
	encoded, err := encodeFixedValue(column, value)
	if err != nil {
		return err
	}
	writer.byte(byte(len(encoded)))
	writer.bytes(encoded)
	return nil
}

func (writer *tokenWriter) partiallyLengthPrefixedValue(column Column, value any) error {
	if value == nil {
		writer.uint64(plpNull)
		return nil
	}
	encoded, err := encodeVariableValue(column, value)
	if err != nil {
		return err
	}
	writer.uint64(uint64(len(encoded)))
	if len(encoded) > 0 {
		writer.uint32(uint32(len(encoded)))
		writer.bytes(encoded)
	}
	writer.uint32(0)
	return nil
}

func isPartiallyLengthPrefixed(columnType ColumnType) bool {
	switch columnType {
	case TypeNVarCharMax, TypeVarBinaryMax, TypeXML, TypeGeography:
		return true
	}
	return false
}

func isShortLengthPrefixed(columnType ColumnType) bool {
	switch columnType {
	case TypeNVarChar, TypeVarChar, TypeVarBinary, TypeBinary:
		return true
	}
	return false
}

func encodeVariableValue(column Column, value any) ([]byte, error) {
	if value == nil {
		return nil, nil
	}
	switch column.Type {
	case TypeNVarChar, TypeNVarCharMax, TypeXML:
		text, isText := value.(string)
		if !isText {
			return nil, errors.New("expected a string")
		}
		return encodeUCS2(text), nil
	case TypeVarChar:
		text, isText := value.(string)
		if !isText {
			return nil, errors.New("expected a string")
		}
		return []byte(text), nil
	default:
		bytes, isBytes := value.([]byte)
		if !isBytes {
			return nil, errors.New("expected []byte")
		}
		return bytes, nil
	}
}

// encodeFixedValue encodes the byte-length-prefixed types: integers, bit, floats, decimals, GUIDs, and dates.
func encodeFixedValue(column Column, value any) ([]byte, error) {
	switch column.Type {
	case TypeTinyInt, TypeSmallInt, TypeInt, TypeBigInt:
		integer, isInteger := value.(int64)
		if !isInteger {
			return nil, errors.New("expected int64")
		}
		encoded := binary.LittleEndian.AppendUint64(nil, uint64(integer))
		return encoded[:integerSize(column.Type)], nil
	case TypeBit:
		flag, isBool := value.(bool)
		if !isBool {
			return nil, errors.New("expected bool")
		}
		if flag {
			return []byte{1}, nil
		}
		return []byte{0}, nil
	case TypeReal:
		number, isFloat := value.(float32)
		if !isFloat {
			return nil, errors.New("expected float32")
		}
		return binary.LittleEndian.AppendUint32(nil, math.Float32bits(number)), nil
	case TypeFloat:
		number, isFloat := value.(float64)
		if !isFloat {
			return nil, errors.New("expected float64")
		}
		return binary.LittleEndian.AppendUint64(nil, math.Float64bits(number)), nil
	case TypeDecimal:
		return encodeDecimal(value, column.Precision, column.Scale)
	case TypeMoney, TypeSmallMoney:
		return encodeMoney(value, column.Type)
	case TypeUniqueIdentifier:
		return encodeUniqueIdentifier(value)
	case TypeDate, TypeTime, TypeDateTime2, TypeDateTimeOffset, TypeDateTime, TypeSmallDateTime:
		return encodeTemporal(column, value)
	default:
		return nil, fmt.Errorf("unsupported scripted column type %d", column.Type)
	}
}

func encodeDecimal(value any, precision byte, scale byte) ([]byte, error) {
	text, isText := value.(string)
	if !isText {
		return nil, errors.New("expected decimal text")
	}
	scaled, err := scaledInteger(text, int(scale))
	if err != nil {
		return nil, err
	}
	sign := byte(1)
	if scaled.Sign() < 0 {
		sign = 0
		scaled.Neg(scaled)
	}
	magnitude := scaled.Bytes()
	size := decimalMagnitudeSize(precision)
	if len(magnitude) > size {
		return nil, errors.New("decimal exceeds its precision")
	}
	encoded := make([]byte, 1+size)
	encoded[0] = sign
	for index, digit := range magnitude {
		encoded[1+len(magnitude)-1-index] = digit
	}
	return encoded, nil
}

// scaledInteger parses decimal text as an integer count of 10^-scale units.
func scaledInteger(text string, scale int) (*big.Int, error) {
	whole, fraction, _ := strings.Cut(text, ".")
	if len(fraction) > scale {
		return nil, fmt.Errorf("decimal %q has more than %d fractional digits", text, scale)
	}
	scaled, isValid := new(big.Int).SetString(whole+fraction+strings.Repeat("0", scale-len(fraction)), 10)
	if !isValid {
		return nil, fmt.Errorf("invalid decimal %q", text)
	}
	return scaled, nil
}

func encodeMoney(value any, columnType ColumnType) ([]byte, error) {
	text, isText := value.(string)
	if !isText {
		return nil, errors.New("expected money text")
	}
	scaled, err := scaledInteger(text, 4)
	if err != nil {
		return nil, err
	}
	units := scaled.Int64()
	if columnType == TypeSmallMoney {
		return binary.LittleEndian.AppendUint32(nil, uint32(int32(units))), nil
	}
	encoded := binary.LittleEndian.AppendUint32(nil, uint32(uint64(units)>>32))
	return binary.LittleEndian.AppendUint32(encoded, uint32(units)), nil
}

func encodeUniqueIdentifier(value any) ([]byte, error) {
	text, isText := value.(string)
	if !isText {
		return nil, errors.New("expected uniqueidentifier text")
	}
	canonical, err := hex.DecodeString(strings.ReplaceAll(text, "-", ""))
	if err != nil || len(canonical) != 16 {
		return nil, errors.New("invalid uniqueidentifier")
	}
	return []byte{
		canonical[3], canonical[2], canonical[1], canonical[0], canonical[5], canonical[4], canonical[7], canonical[6],
		canonical[8], canonical[9], canonical[10], canonical[11], canonical[12], canonical[13], canonical[14], canonical[15],
	}, nil
}

// encodeTemporal encodes the date and time types from ISO 8601 text.
func encodeTemporal(column Column, value any) ([]byte, error) {
	text, isText := value.(string)
	if !isText {
		return nil, errors.New("expected ISO 8601 text")
	}
	scale := int(temporalScale(column))
	switch column.Type {
	case TypeDate:
		date, err := time.Parse("2006-01-02", text)
		if err != nil {
			return nil, err
		}
		return encodeDays(date), nil
	case TypeTime:
		clock, err := time.Parse("15:04:05.9999999", text)
		if err != nil {
			return nil, err
		}
		return encodeTimeOfDay(clock, scale), nil
	case TypeDateTime2:
		instant, err := time.Parse("2006-01-02T15:04:05.9999999", text)
		if err != nil {
			return nil, err
		}
		return append(encodeTimeOfDay(instant, scale), encodeDays(instant)...), nil
	case TypeDateTimeOffset:
		instant, err := time.Parse(time.RFC3339Nano, text)
		if err != nil {
			return nil, err
		}
		_, offsetSeconds := instant.Zone()
		utc := instant.UTC()
		encoded := append(encodeTimeOfDay(utc, scale), encodeDays(utc)...)
		return binary.LittleEndian.AppendUint16(encoded, uint16(int16(offsetSeconds/60))), nil
	case TypeDateTime:
		instant, err := time.Parse("2006-01-02T15:04:05.999", text)
		if err != nil {
			return nil, err
		}
		midnight := time.Date(instant.Year(), instant.Month(), instant.Day(), 0, 0, 0, 0, time.UTC)
		days := int32(midnight.Sub(time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)).Hours() / 24)
		ticks := uint32(math.Round(float64(instant.Sub(midnight)) / float64(time.Second) * 300))
		return binary.LittleEndian.AppendUint32(binary.LittleEndian.AppendUint32(nil, uint32(days)), ticks), nil
	default:
		instant, err := time.Parse("2006-01-02T15:04:05", text)
		if err != nil {
			return nil, err
		}
		midnight := time.Date(instant.Year(), instant.Month(), instant.Day(), 0, 0, 0, 0, time.UTC)
		days := uint16(midnight.Sub(time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)).Hours() / 24)
		minutes := uint16(instant.Sub(midnight) / time.Minute)
		return binary.LittleEndian.AppendUint16(binary.LittleEndian.AppendUint16(nil, days), minutes), nil
	}
}

// encodeDays counts proleptic Gregorian days since 0001-01-01; a time.Duration cannot span that range.
func encodeDays(instant time.Time) []byte {
	previousYears := instant.Year() - 1
	days := uint32(previousYears*365 + previousYears/4 - previousYears/100 + previousYears/400 + instant.YearDay() - 1)
	return []byte{byte(days), byte(days >> 8), byte(days >> 16)}
}

func encodeTimeOfDay(instant time.Time, scale int) []byte {
	sinceMidnight := time.Duration(instant.Hour())*time.Hour + time.Duration(instant.Minute())*time.Minute +
		time.Duration(instant.Second())*time.Second + time.Duration(instant.Nanosecond())
	units := uint64(sinceMidnight) / uint64(math.Pow10(9-scale))
	encoded := binary.LittleEndian.AppendUint64(nil, units)
	return encoded[:temporalSize(scale)]
}

func temporalSize(scale int) int {
	switch {
	case scale <= 2:
		return 3
	case scale <= 4:
		return 4
	default:
		return 5
	}
}

func temporalScale(column Column) byte {
	if column.Scale == 0 && column.Precision == 0 {
		return defaultTime
	}
	return column.Scale
}

func integerSize(columnType ColumnType) int {
	switch columnType {
	case TypeTinyInt:
		return 1
	case TypeSmallInt:
		return 2
	case TypeInt:
		return 4
	default:
		return 8
	}
}

func decimalMagnitudeSize(precision byte) int {
	switch {
	case precision <= 9:
		return 4
	case precision <= 19:
		return 8
	case precision <= 28:
		return 12
	default:
		return 16
	}
}

func lengthOrDefault(length int, fallback int) int {
	if length == 0 {
		return fallback
	}
	return length
}

func encodeUCS2(text string) []byte {
	units := utf16.Encode([]rune(text))
	encoded := make([]byte, 2*len(units))
	for index, unit := range units {
		binary.LittleEndian.PutUint16(encoded[2*index:], unit)
	}
	return encoded
}

func decodeUCS2(encoded []byte) (string, error) {
	if len(encoded)%2 != 0 {
		return "", errors.New("odd UCS-2 length")
	}
	units := make([]uint16, len(encoded)/2)
	for index := range units {
		units[index] = binary.LittleEndian.Uint16(encoded[2*index:])
	}
	return string(utf16.Decode(units)), nil
}

// requestReader reads the fields of one client message.
type requestReader struct {
	buffer []byte
	offset int
}

var errRequestTruncated = errors.New("scripted TDS request is truncated")

func (reader *requestReader) take(count int) ([]byte, error) {
	if count < 0 || reader.offset+count > len(reader.buffer) {
		return nil, errRequestTruncated
	}
	value := reader.buffer[reader.offset : reader.offset+count]
	reader.offset += count
	return value, nil
}

func (reader *requestReader) byte() (byte, error) {
	value, err := reader.take(1)
	if err != nil {
		return 0, err
	}
	return value[0], nil
}

func (reader *requestReader) uint16() (uint16, error) {
	value, err := reader.take(2)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint16(value), nil
}

func (reader *requestReader) uint32() (uint32, error) {
	value, err := reader.take(4)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(value), nil
}

func (reader *requestReader) uint64() (uint64, error) {
	value, err := reader.take(8)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(value), nil
}

func (reader *requestReader) byteText() (string, error) {
	length, err := reader.byte()
	if err != nil {
		return "", err
	}
	encoded, err := reader.take(2 * int(length))
	if err != nil {
		return "", err
	}
	return decodeUCS2(encoded)
}

// skipAllHeaders skips the ALL_HEADERS block and returns its transaction descriptor.
func (reader *requestReader) skipAllHeaders() (uint64, error) {
	start := reader.offset
	total, err := reader.uint32()
	if err != nil {
		return 0, err
	}
	var descriptor uint64
	for reader.offset < start+int(total) {
		headerLength, err := reader.uint32()
		if err != nil {
			return 0, err
		}
		headerType, err := reader.uint16()
		if err != nil {
			return 0, err
		}
		data, err := reader.take(int(headerLength) - 6)
		if err != nil {
			return 0, err
		}
		if headerType == 2 && len(data) >= 8 {
			descriptor = binary.LittleEndian.Uint64(data)
		}
	}
	return descriptor, nil
}

// parameter reads one RPC parameter: its name, status, TYPE_INFO, and value.
func (reader *requestReader) parameter() (Parameter, error) {
	name, err := reader.byteText()
	if err != nil {
		return Parameter{}, err
	}
	if _, err := reader.byte(); err != nil {
		return Parameter{}, err
	}
	typeID, err := reader.byte()
	if err != nil {
		return Parameter{}, err
	}
	parameter := Parameter{Name: name, TypeID: typeID}
	switch typeID {
	case typeNull:
		parameter.IsNull = true
		return parameter, nil
	case typeIntN, typeBitN, typeFloatN, typeGUID, typeDateTimeN, typeMoneyN:
		if _, err := reader.byte(); err != nil {
			return Parameter{}, err
		}
	case typeDecimalN:
		if _, err := reader.take(3); err != nil {
			return Parameter{}, err
		}
	case typeTimeN, typeDateTime2N, typeDateTimeOffset:
		if _, err := reader.byte(); err != nil {
			return Parameter{}, err
		}
	case typeDateN:
	case typeBigVarBinary, typeBigVarChar, typeNVarChar, typeBigBinary:
		maximumLength, err := reader.uint16()
		if err != nil {
			return Parameter{}, err
		}
		if typeID == typeBigVarChar || typeID == typeNVarChar {
			if _, err := reader.take(5); err != nil {
				return Parameter{}, err
			}
		}
		return reader.variableParameterValue(parameter, maximumLength == lengthMax)
	default:
		return Parameter{}, fmt.Errorf("scripted TDS cannot decode parameter type 0x%02x", typeID)
	}
	length, err := reader.byte()
	if err != nil {
		return Parameter{}, err
	}
	if length == 0 {
		parameter.IsNull = true
		return parameter, nil
	}
	data, err := reader.take(int(length))
	if err != nil {
		return Parameter{}, err
	}
	parameter.Text, err = describeFixedParameter(typeID, data)
	return parameter, err
}

func (reader *requestReader) variableParameterValue(parameter Parameter, isPartiallyLengthPrefixed bool) (Parameter, error) {
	var data []byte
	if isPartiallyLengthPrefixed {
		total, err := reader.uint64()
		if err != nil {
			return Parameter{}, err
		}
		if total == plpNull {
			parameter.IsNull = true
			return parameter, nil
		}
		for {
			chunkLength, err := reader.uint32()
			if err != nil {
				return Parameter{}, err
			}
			if chunkLength == 0 {
				break
			}
			chunk, err := reader.take(int(chunkLength))
			if err != nil {
				return Parameter{}, err
			}
			data = append(data, chunk...)
		}
	} else {
		length, err := reader.uint16()
		if err != nil {
			return Parameter{}, err
		}
		if length == lengthMax {
			parameter.IsNull = true
			return parameter, nil
		}
		if data, err = reader.take(int(length)); err != nil {
			return Parameter{}, err
		}
	}
	if parameter.TypeID == typeNVarChar {
		text, err := decodeUCS2(data)
		parameter.Text = text
		return parameter, err
	}
	if parameter.TypeID == typeBigVarChar {
		parameter.Text = string(data)
		return parameter, nil
	}
	parameter.Text = hex.EncodeToString(data)
	return parameter, nil
}

// describeFixedParameter renders a fixed-size parameter value as text for test assertions.
func describeFixedParameter(typeID byte, data []byte) (string, error) {
	switch typeID {
	case typeIntN:
		var integer int64
		switch len(data) {
		case 1:
			integer = int64(data[0])
		case 2:
			integer = int64(int16(binary.LittleEndian.Uint16(data)))
		case 4:
			integer = int64(int32(binary.LittleEndian.Uint32(data)))
		case 8:
			integer = int64(binary.LittleEndian.Uint64(data))
		default:
			return "", errors.New("invalid integer parameter")
		}
		return strconv.FormatInt(integer, 10), nil
	case typeBitN:
		return strconv.Itoa(int(data[0])), nil
	case typeFloatN:
		if len(data) == 4 {
			return strconv.FormatFloat(float64(math.Float32frombits(binary.LittleEndian.Uint32(data))), 'g', -1, 32), nil
		}
		return strconv.FormatFloat(math.Float64frombits(binary.LittleEndian.Uint64(data)), 'g', -1, 64), nil
	case typeDateTimeOffset:
		return describeDateTimeOffset(data)
	default:
		return hex.EncodeToString(data), nil
	}
}

// describeDateTimeOffset renders a datetimeoffset(7) parameter as RFC 3339 text in its own offset.
func describeDateTimeOffset(data []byte) (string, error) {
	if len(data) != 10 {
		return "", errors.New("expected datetimeoffset(7)")
	}
	units := uint64(data[0]) | uint64(data[1])<<8 | uint64(data[2])<<16 | uint64(data[3])<<24 | uint64(data[4])<<32
	days := int(data[5]) | int(data[6])<<8 | int(data[7])<<16
	offsetMinutes := int(int16(binary.LittleEndian.Uint16(data[8:])))
	utc := time.Date(1, 1, 1+days, 0, 0, 0, int(units*100), time.UTC)
	return utc.In(time.FixedZone("", offsetMinutes*60)).Format(time.RFC3339Nano), nil
}
