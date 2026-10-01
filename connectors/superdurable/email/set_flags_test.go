// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package email_test

import (
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/email"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestSetFlagsWritesAbsoluteValuesOnceAndReadsThemBack(t *testing.T) {
	fixture := newMailFixture(t)
	reference := fixture.appendMessage(t, "INBOX", plainMessage("jane@acme.example.com", testUsername, "Hello", "flags@acme.example.com", "Body."),
		[]imap.Flag{imap.FlagFlagged}, januaryTwentyEighth)
	client := fixture.client(t)
	isTrue, isFalse := true, false
	input := email.SetFlagsInput{Message: reference, IsSeen: &isTrue, IsAnswered: &isTrue, IsFlagged: &isFalse}

	updated, err := sdkgo.RunMutation(newEmailDexContext("flags"), client.SetFlags(), emailConnection, input)
	require.NoError(t, err)
	require.Equal(t, email.SetFlagsBranchUpdated, updated.Branch, updated.Failure)
	require.Equal(t, email.MessageFlags{Message: reference, IsSeen: true, IsAnswered: true}, updated.Value)
	require.ElementsMatch(t, []imap.Flag{imap.FlagSeen, imap.FlagAnswered}, fixture.imap.Messages(t, "INBOX")[0].Flags)
	require.Equal(t, 2, fixture.imap.StoreCount(), "one STORE adds and one removes")

	repeated, err := sdkgo.RunMutation(newEmailDexContext("flags-again"), client.SetFlags(), emailConnection, input)
	require.NoError(t, err)
	require.Equal(t, email.SetFlagsBranchUpdated, repeated.Branch)
	require.True(t, repeated.Value.WasAlreadyApplied)
	require.Equal(t, 2, fixture.imap.StoreCount(), "a repeat writes nothing")
}

func TestSetFlagsRejectsAnEmptyChangeAndAMissingMessage(t *testing.T) {
	fixture := newMailFixture(t)
	reference := fixture.appendMessage(t, "INBOX", plainMessage("jane@acme.example.com", testUsername, "Hello", "missing@acme.example.com", "Body."),
		nil, januaryTwentyEighth)
	client := fixture.client(t)
	isTrue := true

	empty, err := sdkgo.RunMutation(newEmailDexContext("empty"), client.SetFlags(), emailConnection, email.SetFlagsInput{Message: reference})
	require.NoError(t, err)
	require.Equal(t, email.SetFlagsBranchDefect, empty.Branch)

	missing, err := sdkgo.RunMutation(newEmailDexContext("missing"), client.SetFlags(), emailConnection, email.SetFlagsInput{
		Message: email.MessageReference{Mailbox: "INBOX", UIDValidity: reference.UIDValidity, UID: reference.UID + 3}, IsSeen: &isTrue,
	})
	require.NoError(t, err)
	require.Equal(t, email.SetFlagsBranchNotFound, missing.Branch)
	require.Zero(t, fixture.imap.StoreCount())
}
