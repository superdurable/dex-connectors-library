// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package notion_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/notion"
)

func TestParseIDAcceptsTheFormsUsersCopy(t *testing.T) {
	const canonical = "1f3c9a2e-5b7d-4e8a-9c01-23456789abcd"
	for _, value := range []string{
		canonical,
		"1F3C9A2E-5B7D-4E8A-9C01-23456789ABCD",
		"1f3c9a2e5b7d4e8a9c0123456789abcd",
		"  1f3c9a2e5b7d4e8a9c0123456789abcd  ",
		"https://www.notion.so/acme/Form-submissions-1f3c9a2e5b7d4e8a9c0123456789abcd?v=0123456789abcdef0123456789abcdef",
		"https://www.notion.so/1f3c9a2e5b7d4e8a9c0123456789abcd",
		"https://app.notion.com/p/1f3c9a2e5b7d4e8a9c0123456789abcd",
		"https://app.notion.com/p/" + canonical,
		"https://acme.notion.site/Public-page-1f3c9a2e5b7d4e8a9c0123456789abcd",
	} {
		id, err := notion.ParseID(value)
		require.NoError(t, err, value)
		require.Equal(t, canonical, id, value)
	}
}

func TestParseIDRejectsOtherValuesWithoutRepeatingThem(t *testing.T) {
	for _, value := range []string{
		"",
		"Form submissions",
		"1f3c9a2e5b7d4e8a9c0123456789abc",
		"https://example.com/1f3c9a2e5b7d4e8a9c0123456789abcd",
		"https://notion.so.example.com/1f3c9a2e5b7d4e8a9c0123456789abcd",
		"https://www.notion.so/acme/Form-submissions",
		"http://www.notion.so/1f3c9a2e5b7d4e8a9c0123456789abcd",
	} {
		_, err := notion.ParseID(value)
		require.Error(t, err, value)
		if value != "" {
			require.NotContains(t, err.Error(), value)
		}
	}
}
