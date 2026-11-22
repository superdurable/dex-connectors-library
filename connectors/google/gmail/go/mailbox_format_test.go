// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gmail_test

import (
	"net/mail"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	gmail "github.com/superdurable/dex-connectors-library/connectors/google/gmail/go"
)

// RFC 2047 section 5(3) admits only these characters in an encoded word that replaces a phrase word.
var phraseEncodedWord = regexp.MustCompile(`^=\?utf-8\?[bq]\?[A-Za-z0-9!*+\-/=_]*\?=$`)

// displayNameMailboxCases is Go's To: mailbox per display name; conformance replays these names in Python and TypeScript.
var displayNameMailboxCases = []struct {
	name    string
	mailbox string
}{
	{"Zoë (Ops), Inc", "=?utf-8?b?Wm/DqyAoT3BzKSwgSW5j?= <zoe@example.com>"},
	{`Zoë "Q" Ops`, "=?utf-8?b?Wm/DqyAiUSIgT3Bz?= <zoe@example.com>"},
	{`Zoë\Ops`, "=?utf-8?b?Wm/Dq1xPcHM=?= <zoe@example.com>"},
	{"Zoë <Ops>", "=?utf-8?b?Wm/DqyA8T3BzPg==?= <zoe@example.com>"},
	{"Zoë@Ops: Team; Leads. [EU]", "=?utf-8?b?Wm/Dq0BPcHM6IFRlYW07IExlYWRzLiBbRVVd?= <zoe@example.com>"},
	{"Renée O'Brien #1 & {Ops}", "=?utf-8?b?UmVuw6llIE8nQnJpZW4gIzEgJiB7T3BzfQ==?= <zoe@example.com>"},
	{"Zoë/Ops+1-2*3!=?_", "=?utf-8?q?Zo=C3=AB/Ops+1-2*3!=3D=3F=5F?= <zoe@example.com>"},
	{`Doe, "JD" \ John`, `"Doe, \"JD\" \\ John" <zoe@example.com>`},
	{
		"Zoë Ångström-Øresund Ελληνικά Кириллица 中文 🎉 Party Planning Committee of the Year",
		"=?utf-8?q?Zo=C3=AB_=C3=85ngstr=C3=B6m-=C3=98resund_=CE=95=CE=BB=CE=BB?= " +
			"=?utf-8?q?=CE=B7=CE=BD=CE=B9=CE=BA=CE=AC_=D0=9A=D0=B8=D1=80=D0=B8=D0=BB?= " +
			"=?utf-8?q?=D0=BB=D0=B8=D1=86=D0=B0_=E4=B8=AD=E6=96=87_=F0=9F=8E=89_Party_?= " +
			"=?utf-8?q?Planning_Committee_of_the_Year?= <zoe@example.com>",
	},
	{
		"Zoë Ångström (Øresund), Ελληνικά & Кириллица: 中文 🎉 Party Planning Committee of the Year",
		"=?utf-8?b?Wm/DqyDDhW5nc3Ryw7ZtICjDmHJlc3VuZCksIM6VzrvOu863zr3Ouc66zqwg?= " +
			"=?utf-8?b?JiDQmtC40YDQuNC70LvQuNGG0LA6IOS4reaWhyDwn46JIFBhcnR5IFBsYW5u?= " +
			"=?utf-8?b?aW5nIENvbW1pdHRlZSBvZiB0aGUgWWVhcg==?= <zoe@example.com>",
	},
	{
		`Zoë\Ops Ångström-Øresund Ελληνικά Кириллица 中文 Party Planning`,
		"=?utf-8?b?Wm/Dq1xPcHMgw4VuZ3N0csO2bS3DmHJlc3VuZCDOlc67zrvOt869zrnOus6s?= " +
			"=?utf-8?b?INCa0LjRgNC40LvQu9C40YbQsCDkuK3mlocgUGFydHkgUGxhbm5pbmc=?= <zoe@example.com>",
	},
	{
		"日本語の名前とても長い名前です日本語の名前とても長い名前です",
		"=?utf-8?q?=E6=97=A5=E6=9C=AC=E8=AA=9E=E3=81=AE=E5=90=8D=E5=89=8D=E3=81=A8?= " +
			"=?utf-8?q?=E3=81=A6=E3=82=82=E9=95=B7=E3=81=84=E5=90=8D=E5=89=8D=E3=81=A7?= " +
			"=?utf-8?q?=E3=81=99=E6=97=A5=E6=9C=AC=E8=AA=9E=E3=81=AE=E5=90=8D=E5=89=8D?= " +
			"=?utf-8?q?=E3=81=A8=E3=81=A6=E3=82=82=E9=95=B7=E3=81=84=E5=90=8D=E5=89=8D?= " +
			"=?utf-8?q?=E3=81=A7=E3=81=99?= <zoe@example.com>",
	},
	{
		"Team 👩‍💻 👨🏽‍🚀",
		"=?utf-8?q?Team_=F0=9F=91=A9=E2=80=8D=F0=9F=92=BB_=F0=9F=91=A8=F0=9F=8F=BD?= " +
			"=?utf-8?q?=E2=80=8D=F0=9F=9A=80?= <zoe@example.com>",
	},
	{"שלום Zoë مرحبا", "=?utf-8?q?=D7=A9=D7=9C=D7=95=D7=9D_Zo=C3=AB_=D9=85=D8=B1=D8=AD=D8=A8?= =?utf-8?q?=D8=A7?= <zoe@example.com>"},
}

// TestDisplayNameMailboxParsesBackToOneMailbox checks the send, getMessage, and reply forms of each display name.
func TestDisplayNameMailboxParsesBackToOneMailbox(t *testing.T) {
	for _, test := range displayNameMailboxCases {
		quoted := `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(test.name) + `" <zoe@example.com>`
		recipients, isValid := gmail.FormatRecipientsForTest([]string{quoted})
		require.True(t, isValid, test.name)
		require.Equal(t, []string{test.mailbox}, recipients, test.name)

		if !strings.HasPrefix(test.mailbox, `"`) {
			words := strings.Fields(strings.TrimSuffix(test.mailbox, " <zoe@example.com>"))
			for _, word := range words {
				require.LessOrEqual(t, len(word), 75, test.name)
				require.Regexp(t, phraseEncodedWord, word, test.name)
			}
		}
		addresses, err := mail.ParseAddressList(test.mailbox + ", other@example.com")
		require.NoError(t, err, test.name)
		require.Equal(t, []*mail.Address{{Name: test.name, Address: "zoe@example.com"}, {Address: "other@example.com"}}, addresses, test.name)

		require.Equal(t, []string{test.mailbox, "<other@example.com>"}, gmail.DecodeToHeaderForTest(test.mailbox+", other@example.com"), test.name)
		reply, isValid := gmail.ReplyRecipientForTest(gmail.Message{ReplyTo: test.mailbox, From: "sender@example.com"})
		require.True(t, isValid, test.name)
		require.Equal(t, test.mailbox, reply, test.name)
		query, isValid := gmail.SentMailCandidateQueryForSendForTest([]string{quoted})
		require.True(t, isValid, test.name)
		require.Equal(t, "in:sent newer_than:2d to:zoe@example.com", query, test.name)
	}
}

// TestBackslashDisplayNameIsBEncoded pins the one name mail.Address.String would Q-encode with a literal backslash.
func TestBackslashDisplayNameIsBEncoded(t *testing.T) {
	standard := (&mail.Address{Name: `Zoë\Ops`, Address: "zoe@example.com"}).String()
	require.Equal(t, `=?utf-8?q?Zo=C3=AB\Ops?= <zoe@example.com>`, standard)
	_, err := mail.ParseAddress(standard)
	require.Error(t, err)

	recipients, isValid := gmail.FormatRecipientsForTest([]string{`"Zoë\\Ops" <zoe@example.com>`, `"Zoe\\Ops" <ascii@example.com>`})
	require.True(t, isValid)
	require.Equal(t, []string{"=?utf-8?b?Wm/Dq1xPcHM=?= <zoe@example.com>", `"Zoe\\Ops" <ascii@example.com>`}, recipients)
}
