// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package excel

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// maximumExcelColumns and maximumExcelRows are the worksheet limits of current Excel, XFD and 1048576.
	maximumExcelColumns = 16384
	maximumExcelRows    = 1048576
	// maximumWorksheetNameRunes is Excel's limit for a worksheet name.
	maximumWorksheetNameRunes = 31
	// maximumTableReferenceRunes bounds a table or column name, which Excel limits to 255 characters.
	maximumTableReferenceRunes = 255
)

var (
	// graphDriveItemIDPattern accepts Graph drive and drive item IDs, such as b!AbC-1_x and 01BYE5RZ6QN3ZWBTUFOFD3GSPGOHDJD36K.
	graphDriveItemIDPattern = regexp.MustCompile(`^[A-Za-z0-9!_.~-]{1,512}$`)
	worksheetIDPattern      = regexp.MustCompile(`^\{[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}\}$`)
	a1CellPattern           = regexp.MustCompile(`^([A-Z]{1,3})([1-9][0-9]{0,6})$`)
)

// a1Range is one validated, bounded A1 range such as B2:D4.
type a1Range struct {
	address     string
	rowCount    int64
	columnCount int64
}

// cellCount returns the number of cells the range covers.
func (address a1Range) cellCount() int64 { return address.rowCount * address.columnCount }

func validateWorkbookLocation(driveID string, workbookID string) error {
	if !graphDriveItemIDPattern.MatchString(driveID) {
		return errors.New("driveId must be a Microsoft Graph drive ID, such as the one the workbookPicker unit stores")
	}
	if !graphDriveItemIDPattern.MatchString(workbookID) {
		return errors.New("workbookId must be a Microsoft Graph drive item ID, such as the one the workbookPicker unit stores")
	}
	return nil
}

// validateWorksheetReference accepts a worksheet ID in braces or a name Excel allows.
func validateWorksheetReference(worksheet string) error {
	if worksheetIDPattern.MatchString(worksheet) {
		return nil
	}
	runeCount := utf8.RuneCountInString(worksheet)
	switch {
	case !utf8.ValidString(worksheet) || runeCount == 0 || runeCount > maximumWorksheetNameRunes:
		return errors.New("worksheet must be a worksheet ID or a name of 1 to 31 characters")
	case strings.ContainsAny(worksheet, `\/?*[]:`) || hasControlCharacter(worksheet):
		return errors.New(`worksheet name cannot contain \ / ? * [ ] : or control characters`)
	case strings.HasPrefix(worksheet, "'") || strings.HasSuffix(worksheet, "'"):
		return errors.New("worksheet name cannot begin or end with an apostrophe")
	}
	return nil
}

// validateTableReference accepts an opaque table ID or a table name.
func validateTableReference(table string) error {
	if err := validateTableIdentifierText(table); err != nil {
		return errors.New("table must be a table ID or name of 1 to 255 characters without slashes or control characters")
	}
	return nil
}

func validateColumnName(columnName string) error {
	if err := validateTableIdentifierText(columnName); err != nil {
		return errors.New("a column name must be 1 to 255 characters without slashes or control characters")
	}
	return nil
}

func validateTableIdentifierText(text string) error {
	runeCount := utf8.RuneCountInString(text)
	if !utf8.ValidString(text) || runeCount == 0 || runeCount > maximumTableReferenceRunes ||
		strings.TrimSpace(text) == "" || strings.ContainsRune(text, '/') || hasControlCharacter(text) {
		return errors.New("invalid table identifier")
	}
	return nil
}

// parseA1Range accepts one bounded cell or rectangle, such as a2:c10, and returns it uppercased.
func parseA1Range(address string) (a1Range, error) {
	upper := strings.ToUpper(strings.TrimSpace(address))
	first, last, isRange := strings.Cut(upper, ":")
	if !isRange {
		last = first
	}
	firstColumn, firstRow, err := parseA1Cell(first)
	if err != nil {
		return a1Range{}, err
	}
	lastColumn, lastRow, err := parseA1Cell(last)
	if err != nil {
		return a1Range{}, err
	}
	if lastColumn < firstColumn || lastRow < firstRow {
		return a1Range{}, errors.New("address must name its top-left cell first, such as A1:C3")
	}
	return a1Range{address: upper, rowCount: lastRow - firstRow + 1, columnCount: lastColumn - firstColumn + 1}, nil
}

func parseA1Cell(cell string) (column int64, row int64, err error) {
	match := a1CellPattern.FindStringSubmatch(cell)
	if match == nil {
		return 0, 0, errors.New("address must be one cell or range in A1 form, such as A1 or A2:C10, without a worksheet name, $, or whole columns or rows")
	}
	for _, letter := range match[1] {
		column = column*26 + int64(letter-'A'+1)
	}
	row, err = strconv.ParseInt(match[2], 10, 64)
	if err != nil || column > maximumExcelColumns || row > maximumExcelRows {
		return 0, 0, errors.New("address is outside Excel's worksheet limits of column XFD and row 1048576")
	}
	return column, row, nil
}

func hasControlCharacter(text string) bool {
	return strings.ContainsFunc(text, unicode.IsControl)
}
