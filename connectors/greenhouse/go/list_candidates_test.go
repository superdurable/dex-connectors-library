// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package greenhouse

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDecodeCandidatePageKeepsContactDataOutAndReturnsCursor(t *testing.T) {
	page, err := decodeCandidatePage(
		[]byte(`[{"id":53883394,"first_name":"Sample","last_name":"Candidate","email_addresses":[{"value":"secret@example.com"}]}]`),
		`<https://harvest.greenhouse.io/v3/candidates?cursor=next-page>; rel="next"`,
		"https://harvest.greenhouse.io/v3",
	)
	require.NoError(t, err)
	require.Equal(t, "next-page", page.NextCursor)
	require.Equal(t, []CandidateProfile{{ID: 53883394, FirstName: "Sample", LastName: "Candidate"}}, page.Candidates)
}

func TestDecodeCandidatePageRejectsForeignPaginationLink(t *testing.T) {
	_, err := decodeCandidatePage([]byte(`[]`), `<https://elsewhere.example/v3/candidates?cursor=next-page>; rel="next"`, "https://harvest.greenhouse.io/v3")
	require.Error(t, err)
}
