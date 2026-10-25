//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package supportreply

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-mail/outlookmailmock"
)

func TestSupportReplyUsesMockDefaultsWithRealDex(t *testing.T) {
	mock := outlookmailmock.New(t, ConnectionName)
	harness := newReplyHarnessForConnection(t, mock.Connection())
	outcome := harness.runReply(t, "mock-default", integrationInput())
	require.Equal(t, ReplyActionReplied, outcome.Action)
	require.False(t, outcome.NeedsReview)
	require.Equal(t, "AAMkAGI2MDEyMzQ1AAABC002", outcome.CustomerMessageID)
	require.Equal(t, "RE: Refund request for order 88213", outcome.Sent.Subject)
	require.Equal(t, []string{integrationCustomer}, outcome.Sent.Recipients)
	require.NotNil(t, outcome.Flags)
	require.True(t, outcome.Flags.IsRead)
	require.NotNil(t, outcome.ArchivedTo)
	require.Equal(t, "Archive", outcome.ArchivedTo.DestinationFolderName)
	require.Empty(t, mock.SendMessage().Calls())
}
