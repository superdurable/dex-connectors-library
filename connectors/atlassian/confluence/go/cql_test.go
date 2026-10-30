// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package confluence_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/confluence"
)

func TestQuoteCQLStringEscapesQuotesAndBackslashes(t *testing.T) {
	quoted, err := confluence.QuoteCQLString(`C:\ops "night" shift`)
	require.NoError(t, err)
	require.Equal(t, `"C:\\ops \"night\" shift"`, quoted)
	require.Equal(t, `"OPS"`, confluence.MustQuoteCQLString("OPS"))
	for _, value := range []string{"line\nbreak", "\x00", string([]byte{0xff})} {
		_, err := confluence.QuoteCQLString(value)
		require.Error(t, err, "%q", value)
	}
	require.Panics(t, func() { confluence.MustQuoteCQLString("tab\there") })
}

func TestFilterCQLJoinsEveryClauseAndChoosesTheOrder(t *testing.T) {
	modifiedSince := time.Date(2026, time.September, 28, 5, 0, 0, 0, time.FixedZone("PDT", -7*3600))
	cql, err := confluence.PageSearchFilter{
		SpaceKeys: []string{"OPS", "~5b10ac8d82e05b22cc7d4ef5"}, TitlePhrase: ` remote "work" `, Labels: []string{"policy", "in-force"},
		ModifiedSince: &modifiedSince, AdditionalCQL: `ancestor = 65537 OR parent = 65537`, Order: confluence.PageSearchOrderTitleAscending,
	}.CQL()
	require.NoError(t, err)
	require.Equal(t, `type = page AND space in ("OPS", "~5b10ac8d82e05b22cc7d4ef5") AND label in ("policy", "in-force") AND `+
		`title ~ "\"remote \\\"work\\\"\"" AND lastmodified >= "2026-09-28" AND (ancestor = 65537 OR parent = 65537) ORDER BY title ASC`, cql)

	minimal, err := confluence.PageSearchFilter{}.CQL()
	require.NoError(t, err)
	require.Equal(t, "type = page ORDER BY lastmodified DESC", minimal)
}

func TestFilterCQLWidensTheModifiedSinceDateForEveryTimeZone(t *testing.T) {
	for _, test := range []struct {
		modifiedSince time.Time
		date          string
	}{
		{time.Date(2026, time.September, 28, 5, 0, 0, 0, time.UTC), "2026-09-27"},
		{time.Date(2026, time.September, 28, 13, 0, 0, 0, time.UTC), "2026-09-28"},
	} {
		cql, err := confluence.PageSearchFilter{ModifiedSince: &test.modifiedSince}.CQL()
		require.NoError(t, err)
		require.Equal(t, `type = page AND lastmodified >= "`+test.date+`" ORDER BY lastmodified DESC`, cql)
	}
}

func TestFilterValuesCannotChangeTheQueryStructure(t *testing.T) {
	for _, test := range []struct {
		name    string
		filter  confluence.PageSearchFilter
		message string
	}{
		{"space key with a quote", confluence.PageSearchFilter{SpaceKeys: []string{`OPS") OR space = ("HR`}}, "filter space values must each be a space key such as OPS"},
		{"label with whitespace", confluence.PageSearchFilter{Labels: []string{"in force"}}, "filter label values must each be a label without whitespace"},
		{"too many spaces", confluence.PageSearchFilter{SpaceKeys: make([]string, 51)}, "filter accepts at most 50 space values"},
		{"title with a control character", confluence.PageSearchFilter{TitlePhrase: "a\x01b"}, "filter titlePhrase: CQL value cannot contain control characters"},
		{"clause that closes the group", confluence.PageSearchFilter{AdditionalCQL: `x = 1) OR (type = blogpost`}, "filter additionalCql closes a parenthesis it did not open"},
		{"clause with an open group", confluence.PageSearchFilter{AdditionalCQL: `(x = 1`}, "filter additionalCql has an unclosed parenthesis"},
		{"clause with an open string", confluence.PageSearchFilter{AdditionalCQL: `title = "a`}, "filter additionalCql has an unterminated string"},
		{"clause with an order", confluence.PageSearchFilter{AdditionalCQL: `label = x order  by created`}, "filter additionalCql cannot contain ORDER BY; use order instead"},
		{"clause with a control character", confluence.PageSearchFilter{AdditionalCQL: "label =\tx"}, "filter additionalCql cannot contain control characters"},
		{"unknown order", confluence.PageSearchFilter{Order: "random"}, "filter order must be lastModifiedDescending, createdDescending, or titleAscending"},
		{"zero modification time", confluence.PageSearchFilter{ModifiedSince: &time.Time{}}, "filter modifiedSince must be a time"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := test.filter.CQL()
			require.EqualError(t, err, test.message)
		})
	}
	cql, err := confluence.PageSearchFilter{AdditionalCQL: `title ~ "a ) order by (" AND label = 'x)'`}.CQL()
	require.NoError(t, err, "parentheses and ORDER BY inside string literals are values")
	require.Equal(t, `type = page AND (title ~ "a ) order by (" AND label = 'x)') ORDER BY lastmodified DESC`, cql)
}
