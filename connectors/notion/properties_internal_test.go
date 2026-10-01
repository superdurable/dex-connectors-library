// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package notion

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/stretchr/testify/require"
)

func TestEveryWritablePropertyTypeEncodesNotionsShape(t *testing.T) {
	encoded, err := encodePropertyValues(map[string]PropertyValue{
		"Name":      TitleValue("Ada Lovelace"),
		"Notes":     RichTextValue("First line\nsecond line"),
		"Score":     NumberValue(42.5),
		"Done":      CheckboxValue(false),
		"Stage":     SelectValue(" Review "),
		"Tags":      MultiSelectValue("web", "beta"),
		"State":     StatusValue("In progress"),
		"Due":       DateRangeValue("2026-09-30", "2026-10-02"),
		"Meeting":   {Type: PropertyTypeDate, Date: &DateRange{Start: "2026-09-30T14:00:00Z", TimeZone: "America/Los_Angeles"}},
		"Site":      URLValue("https://example.com/form"),
		"Email":     EmailValue("ada@example.com"),
		"Phone":     PhoneNumberValue("+1 206 555 0100"),
		"Owner":     PeopleValue("0b3c9a2e5b7d4e8a9c0123456789abcd"),
		"Related":   RelationValue("1f3c9a2e-5b7d-4e8a-9c01-23456789abcd", "1f3c9a2e5b7d4e8a9c0123456789abcd"),
		"Cleared":   ClearedValue(PropertyTypeSelect),
		"NoDate":    ClearedValue(PropertyTypeDate),
		"NoNumber":  ClearedValue(PropertyTypeNumber),
		"NoTags":    ClearedValue(PropertyTypeMultiSelect),
		"NoNotes":   ClearedValue(PropertyTypeRichText),
		"NoAddress": ClearedValue(PropertyTypeEmail),
	}, "properties")
	require.NoError(t, err)
	contents, err := json.Marshal(encoded)
	require.NoError(t, err)
	require.JSONEq(t, `{
		"Name":{"title":[{"type":"text","text":{"content":"Ada Lovelace"}}]},
		"Notes":{"rich_text":[{"type":"text","text":{"content":"First line\nsecond line"}}]},
		"Score":{"number":42.5},
		"Done":{"checkbox":false},
		"Stage":{"select":{"name":"Review"}},
		"Tags":{"multi_select":[{"name":"web"},{"name":"beta"}]},
		"State":{"status":{"name":"In progress"}},
		"Due":{"date":{"start":"2026-09-30","end":"2026-10-02"}},
		"Meeting":{"date":{"start":"2026-09-30T14:00:00Z","time_zone":"America/Los_Angeles"}},
		"Site":{"url":"https://example.com/form"},
		"Email":{"email":"ada@example.com"},
		"Phone":{"phone_number":"+1 206 555 0100"},
		"Owner":{"people":[{"object":"user","id":"0b3c9a2e-5b7d-4e8a-9c01-23456789abcd"}]},
		"Related":{"relation":[{"id":"1f3c9a2e-5b7d-4e8a-9c01-23456789abcd"}]},
		"Cleared":{"select":null},
		"NoDate":{"date":null},
		"NoNumber":{"number":null},
		"NoTags":{"multi_select":[]},
		"NoNotes":{"rich_text":[]},
		"NoAddress":{"email":null}
	}`, string(contents))
}

func TestInvalidPropertyValuesAreRejectedBeforeAnyRequest(t *testing.T) {
	notANumber := math.NaN()
	for name, value := range map[string]PropertyValue{
		"a field of another type":        {Type: PropertyTypeSelect, Text: "Review"},
		"no type":                        {Text: "Ada"},
		"an unknown type":                {Type: "spreadsheet", Text: "Ada"},
		"a read-only type":               {Type: PropertyTypeFormula},
		"an unset checkbox":              ClearedValue(PropertyTypeCheckbox),
		"a cleared status":               ClearedValue(PropertyTypeStatus),
		"a non-finite number":            {Type: PropertyTypeNumber, Number: &notANumber},
		"a select option with a comma":   SelectValue("Review, later"),
		"a repeated multi_select option": MultiSelectValue("web", "web"),
		"a date in another format":       DateValue("30/09/2026"),
		"an impossible date":             DateValue("2026-02-30"),
		"an invalid time zone":           {Type: PropertyTypeDate, Date: &DateRange{Start: "2026-09-30", TimeZone: "not a zone"}},
		"a relative URL":                 URLValue("/form"),
		"a named email address":          EmailValue("Ada <ada@example.com>"),
		"a user ID that is not an ID":    PeopleValue("ada"),
		"text over the property limit":   TitleValue(strings.Repeat("a", MaximumPropertyTextCharacters+1)),
		"a multi-line phone number":      PhoneNumberValue("+1\n206"),
		"too many relation pages":        RelationValue(repeatedIDs(101)...),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := encodePropertyValues(map[string]PropertyValue{"Property": value}, "properties")
			require.Error(t, err)
		})
	}
	_, err := encodePropertyValues(map[string]PropertyValue{" Name": TitleValue("Ada")}, "properties")
	require.ErrorContains(t, err, "without surrounding spaces")
}

func TestLongTextIsSplitIntoNotionSizedRichTextObjects(t *testing.T) {
	text := strings.Repeat("a", 1999) + "😀" + strings.Repeat("b", 2500)
	richText, err := encodeRichText(text, MaximumPropertyTextCharacters)
	require.NoError(t, err)
	require.Len(t, richText, 3)
	var rejoined strings.Builder
	for _, segment := range richText {
		content := segment["text"].(map[string]string)["content"]
		require.LessOrEqual(t, len(utf16.Encode([]rune(content))), maximumRichTextChunkUnits, "Notion counts UTF-16 code units")
		rejoined.WriteString(content)
	}
	require.Equal(t, text, rejoined.String())
	require.Equal(t, strings.Repeat("a", 1999), richText[0]["text"].(map[string]string)["content"], "the emoji does not split across objects")
}

func TestPagePropertiesDecodeEveryTypeIntoTypedValuesAndPlainText(t *testing.T) {
	raw := json.RawMessage(`{
		"Name":{"id":"title","type":"title","title":[{"plain_text":"Ada "},{"plain_text":"Lovelace"}]},
		"Score":{"id":"s","type":"number","number":7},
		"Done":{"id":"d","type":"checkbox","checkbox":true},
		"Stage":{"id":"g","type":"select","select":{"name":"Review"}},
		"Empty":{"id":"e","type":"select","select":null},
		"Tags":{"id":"t","type":"multi_select","multi_select":[{"name":"web"},{"name":"beta"}]},
		"State":{"id":"st","type":"status","status":{"name":"Done"}},
		"Due":{"id":"du","type":"date","date":{"start":"2026-09-30","end":"2026-10-02","time_zone":null}},
		"Owner":{"id":"o","type":"people","people":[{"object":"user","id":"u1","name":"Ada"},{"object":"user","id":"u2"}]},
		"Related":{"id":"r","type":"relation","relation":[{"id":"p1"}],"has_more":true},
		"Total":{"id":"f","type":"formula","formula":{"type":"number","number":12.5}},
		"Flag":{"id":"fb","type":"formula","formula":{"type":"boolean","boolean":false}},
		"Opaque":{"id":"fu","type":"formula","formula":{"type":"unsupported","unsupported":{}}},
		"Sum":{"id":"ro","type":"rollup","rollup":{"type":"array","array":[{"type":"number","number":1},{"type":"title","title":[{"plain_text":"x"}]}],"function":"show_original"}},
		"Ticket":{"id":"u","type":"unique_id","unique_id":{"prefix":"SUB","number":42}},
		"Created":{"id":"c","type":"created_time","created_time":"2026-09-30T16:00:00.000Z"},
		"Author":{"id":"cb","type":"created_by","created_by":{"object":"user","id":"u1","name":"Ada"}},
		"Files":{"id":"fi","type":"files","files":[{"name":"cv.pdf","type":"external"}]},
		"Future":{"id":"x","type":"place","place":{"name":"Seattle"}}
	}`)
	properties, err := decodeRawProperties(raw)
	require.NoError(t, err)
	require.Equal(t, "Ada Lovelace", titleOfProperties(properties))
	plainText := map[string]string{}
	for name, property := range properties {
		plainText[name] = property.PlainText
	}
	require.Equal(t, map[string]string{
		"Name": "Ada Lovelace", "Score": "7", "Done": "true", "Stage": "Review", "Empty": "", "Tags": "web, beta",
		"State": "Done", "Due": "2026-09-30/2026-10-02", "Owner": "Ada, u2", "Related": "p1", "Total": "12.5",
		"Flag": "false", "Opaque": "", "Sum": "1, x", "Ticket": "SUB-42", "Created": "2026-09-30T16:00:00.000Z",
		"Author": "Ada", "Files": "cv.pdf", "Future": "",
	}, plainText)
	require.True(t, properties["Related"].IsTruncated)
	require.Equal(t, []string{"web", "beta"}, properties["Tags"].Options)
	require.Equal(t, 12.5, *properties["Total"].Number)
	require.Equal(t, PropertyType("place"), properties["Future"].Type, "a new Notion type keeps its name")
	require.Equal(t, "SUB-42", properties["Ticket"].Text)
	require.Equal(t, []string{"u1"}, properties["Author"].UserIDs)
}

func TestAReadPropertyCanBeWrittenBackUnchanged(t *testing.T) {
	properties, err := decodeRawProperties(json.RawMessage(`{"Stage":{"id":"g","type":"select","select":{"name":"Review"}},` +
		`"Tags":{"id":"t","type":"multi_select","multi_select":[{"name":"web"}]}}`))
	require.NoError(t, err)
	encoded, err := encodePropertyValues(map[string]PropertyValue{
		"Stage": properties["Stage"].PropertyValue, "Tags": properties["Tags"].PropertyValue,
	}, "properties")
	require.NoError(t, err)
	contents, err := json.Marshal(encoded)
	require.NoError(t, err)
	require.JSONEq(t, `{"Stage":{"select":{"name":"Review"}},"Tags":{"multi_select":[{"name":"web"}]}}`, string(contents))
}

func repeatedIDs(count int) []string {
	ids := make([]string, 0, count)
	for index := range count {
		ids = append(ids, strings.Repeat("0", 24)+leftPaddedHex(index))
	}
	return ids
}

func leftPaddedHex(value int) string {
	const digits = "0123456789abcdef"
	text := make([]byte, 8)
	for position := 7; position >= 0; position-- {
		text[position] = digits[value%16]
		value /= 16
	}
	return string(text)
}
