# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Display-name To: mailboxes match Go's mail.Address.String and parse back to one mailbox."""

import base64
import json
import re
from email import policy
from email.header import decode_header, make_header
from email.parser import Parser
from email.utils import getaddresses
from uuid import uuid4

import pytest
from dex_connectors.sdk import Call, CallID, ConnectionRef, IdempotencyKey, OperationRef

from dex_connectors.google_gmail import SendMessageInput
from dex_connectors.google_gmail.provider import (
    _format_go_mailbox,
    _sent_mail_candidate_query,
    build_send_message,
    decode_message,
    validate_send_input,
)

# RFC 2047 section 5(3) admits only these characters in an encoded word that replaces a phrase word.
_PHRASE_ENCODED_WORD = re.compile(r"=\?utf-8\?[bq]\?[A-Za-z0-9!*+\-/=_]*\?=")

# The Go connector's go/mailbox_format_test.go pins the same names; conformance replays them against Go.
DISPLAY_NAMES = [
    "Zoë (Ops), Inc",
    'Zoë "Q" Ops',
    "Zoë\\Ops",
    "Zoë <Ops>",
    "Zoë@Ops: Team; Leads. [EU]",
    "Renée O'Brien #1 & {Ops}",
    "Zoë/Ops+1-2*3!=?_",
    'Doe, "JD" \\ John',
    "Zoë Ångström-Øresund Ελληνικά Кириллица 中文 🎉 Party Planning Committee of the Year",
    "Zoë Ångström (Øresund), Ελληνικά & Кириллица: 中文 🎉 Party Planning Committee of the Year",
    "Zoë\\Ops Ångström-Øresund Ελληνικά Кириллица 中文 Party Planning",
    "日本語の名前とても長い名前です日本語の名前とても長い名前です",
    "Team 👩‍💻 👨🏽‍🚀",
    "שלום Zoë مرحبا",
]


_ZOE_OPS = "=?utf-8?q?Zo=C3=AB_Ops?= <zoe@example.com>"


def _quote(name: str) -> str:
    return '"' + name.replace("\\", "\\\\").replace('"', '\\"') + '" <zoe@example.com>'


def _format_recipient(recipient: str) -> str:
    send_input = SendMessageInput(to=[recipient], subject="Progress", text_body="Update")
    return validate_send_input(send_input, "owner@example.com")[0]


def _decode_to_header(value: str) -> list[str]:
    resource = {
        "id": "message-1", "threadId": "thread-1", "internalDate": "1000",
        "payload": {"headers": [{"name": "To", "value": value}]},
    }
    return decode_message(resource).to


def test_mailbox_matches_go_with_a_backslash_b_encoded() -> None:
    """Go's mail.Address.String output, except that a backslash forces B-encoding."""
    assert _format_recipient(_quote("Zoë (Ops), Inc")) == "=?utf-8?b?Wm/DqyAoT3BzKSwgSW5j?= <zoe@example.com>"
    assert _format_recipient(_quote("Zoë\\Ops")) == "=?utf-8?b?Wm/Dq1xPcHM=?= <zoe@example.com>"
    assert _format_recipient(_quote("Zoë/Ops+1-2*3!=?_")) == (
        "=?utf-8?q?Zo=C3=AB/Ops+1-2*3!=3D=3F=5F?= <zoe@example.com>"
    )
    assert _format_recipient(_quote('Doe, "JD" \\ John')) == '"Doe, \\"JD\\" \\\\ John" <zoe@example.com>'
    assert _format_recipient(_quote("Team 👩‍💻 👨🏽‍🚀")) == (
        "=?utf-8?q?Team_=F0=9F=91=A9=E2=80=8D=F0=9F=92=BB_=F0=9F=91=A8=F0=9F=8F=BD?= "
        "=?utf-8?q?=E2=80=8D=F0=9F=9A=80?= <zoe@example.com>"
    )


@pytest.mark.parametrize("name", DISPLAY_NAMES)
def test_mailbox_parses_back_to_exactly_one_mailbox(name: str) -> None:
    """Each name gives phrase-safe encoded words of at most 75 characters that decode to one mailbox."""
    mailbox = _format_recipient(_quote(name))
    assert mailbox.endswith(" <zoe@example.com>")
    phrase = mailbox.removesuffix(" <zoe@example.com>")
    if not phrase.startswith('"'):
        for word in phrase.split(" "):
            assert len(word) <= 75
            assert _PHRASE_ENCODED_WORD.fullmatch(word), word
        assert str(make_header(decode_header(phrase))) == name
    header = f"{mailbox}, other@example.com"
    assert [address for _, address in getaddresses([header])] == ["zoe@example.com", "other@example.com"]
    parsed = Parser(policy=policy.default).parsestr(f"To: {header}\n\n")["To"]
    assert [address.addr_spec for address in parsed.addresses] == ["zoe@example.com", "other@example.com"]
    assert _decode_to_header(header) == [mailbox, "<other@example.com>"]
    # A reply formats its source's Reply-To or From this way.
    assert _format_go_mailbox(mailbox) == mailbox
    assert _sent_mail_candidate_query([mailbox]) == "in:sent newer_than:2d to:zoe@example.com"


@pytest.mark.parametrize(
    ("value", "formatted"),
    [
        (
            "=?UTF-8?B?WsO2IChPcHMpLCBJbmM=?= <zoe@example.com>",
            "=?utf-8?b?WsO2IChPcHMpLCBJbmM=?= <zoe@example.com>",
        ),
        ("=?utf-8?q?Zo=C3=AB?= Ops <zoe@example.com>", _ZOE_OPS),
        ("=?utf-8?q?Zo=C3=AB?= =?utf-8?q?_Ops?= <zoe@example.com>", _ZOE_OPS),
        ("=?utf-8?q?Zo=C3=AB_?=   =?utf-8?q?Ops?= <zoe@example.com>", _ZOE_OPS),
        ("Ada   Lovelace <ada@example.com>", '"Ada Lovelace" <ada@example.com>'),
        ("Zoë\tOps <zoe@example.com>", _ZOE_OPS),
        ('"Ada  Lovelace" <ada@example.com>', '"Ada  Lovelace" <ada@example.com>'),
        ("=?iso-8859-1?q?Z=F6e?= <zoe@example.com>", "=?utf-8?q?Z=C3=B6e?= <zoe@example.com>"),
        ("=?us-ascii?q?Z=F6e?= <zoe@example.com>", "=?utf-8?q?Z=EF=BF=BDe?= <zoe@example.com>"),
        (
            "=?UTF-8?Q?Ren=C3=A9e_O=27Brien?= <renee@example.com>",
            "=?utf-8?b?UmVuw6llIE8nQnJpZW4=?= <renee@example.com>",
        ),
        ("=?koi8-r?b?8M/Q?= <zoe@example.com>", "=?utf-8?q?=D0=9F=D0=BE=D0=BF?= <zoe@example.com>"),
        ("=?utf-8?q?bad=ZZ?= <zoe@example.com>", '"=?utf-8?q?bad=ZZ?=" <zoe@example.com>'),
        ("=?x-unknown?q?Z?= <zoe@example.com>", '"=?x-unknown?q?Z?=" <zoe@example.com>'),
        ('"=?utf-8?q?Zo=C3=AB?=" <zoe@example.com>', '"=?utf-8?q?Zo=C3=AB?=" <zoe@example.com>'),
        ("=?utf-8?q??= Ops <zoe@example.com>", '"Ops" <zoe@example.com>'),
    ],
)
def test_to_header_decodes_display_names_as_go_does(value: str, formatted: str) -> None:
    """Decode encoded words like Go's net/mail; the standard parser keeps the space that separates them."""
    assert _decode_to_header(value) == [formatted]
    assert _format_go_mailbox(value) == formatted


def test_to_header_splits_only_outside_quoted_strings() -> None:
    """A quoted comma or escaped quote stays in its mailbox; one invalid mailbox omits them all, as in Go."""
    assert _decode_to_header('"Doe, \\"JD\\" \\\\ John" <j@example.com>, "x\\"y" <xy@example.com>') == [
        '"Doe, \\"JD\\" \\\\ John" <j@example.com>', '"x\\"y" <xy@example.com>',
    ]
    assert _decode_to_header('"Doe, John" <j@example.com>, ada@example.com') == [
        '"Doe, John" <j@example.com>', "<ada@example.com>",
    ]
    assert _decode_to_header('"Doe, John" <j@example.com>, not-an-address') == []
    assert _decode_to_header("undisclosed-recipients:;") == []


# Go 1.27's mail.ParseAddressList and the Go connector's formatMailbox give these, recorded with Go.
RFC_5322_TO_HEADERS = [
    ("Team: alice@example.com, bob@example.com;", ["<alice@example.com>", "<bob@example.com>"]),
    ("alice@example.com (Alice Smith)", ['"Alice Smith" <alice@example.com>']),
    ("alice@example.com (Alice (nested) Smith)", ['"Alice (nested) Smith" <alice@example.com>']),
    ('"Alice" Smith <alice@example.com>', ['"Alice Smith" <alice@example.com>']),
    (
        "Alice <alice@example.com> (work), Bob <bob@example.com>",
        ['"Alice" <alice@example.com>', '"Bob" <bob@example.com>'],
    ),
    ('Zoë <"zoe ops"@example.com>', ['=?utf-8?q?Zo=C3=AB?= <"zoe ops"@example.com>']),
    ('Alice <"a..b"@example.com>', ['"Alice" <"a..b"@example.com>']),
    ('"alice"@example.com', ["<alice@example.com>"]),
    ('"Zoë" <zoe@example.com>, ', ["=?utf-8?q?Zo=C3=AB?= <zoe@example.com>"]),
    ("a@example.com, , b@example.com", ["<a@example.com>", "<b@example.com>"]),
    (
        'Team: "Zoë, Ops" <zoe@example.com>, =?utf-8?q?Zo=C3=AB?= <z2@example.com>;',
        ["=?utf-8?b?Wm/DqywgT3Bz?= <zoe@example.com>", "=?utf-8?q?Zo=C3=AB?= <z2@example.com>"],
    ),
    ("=?utf-8?q?Zo=C3=AB?= =?utf-8?q?_Ops?= <zoe@example.com> (work)", [_ZOE_OPS]),
    ("Alice <alice@[IPv6:::1]>", ['"Alice" <alice@[IPv6:::1]>']),
    ("Alice <alice@[127.0.0.1]>", ['"Alice" <alice@[127.0.0.1]>']),
]

# Go rejects each of these, so getMessage omits to and send and reply refuse them.
GO_REJECTED_MAILBOXES = [
    # The form the Go connector v0.29.1 wrote for a non-ASCII name with a backslash.
    "=?utf-8?q?Zo=C3=AB\\Ops?= <zoe@example.com>",
    "Alice <alice@example.com",
    "Alice @ Home <alice@example.com>",
    "alice@exa mple.com",
    "alice.@example.com",
    'a"b <ab@example.com>',
    "Alice <@route:alice@example.com>",
    "(comment) Alice <alice@example.com>",
    "Alice <alice@[::1]>",
]


@pytest.mark.parametrize(("value", "formatted"), RFC_5322_TO_HEADERS)
def test_to_header_reads_rfc_5322_forms_as_go_does(value: str, formatted: list[str]) -> None:
    """Groups, comments, mixed phrases, quoted local parts, and empty entries read as in Go."""
    assert _decode_to_header(value) == formatted
    if len(formatted) == 1 and "," not in value and ":" not in value:
        assert _format_go_mailbox(value) == formatted[0]
        assert _format_recipient(value) == formatted[0]
    # Each formatted mailbox is a valid recipient that formats to itself.
    for mailbox in formatted:
        assert _format_go_mailbox(mailbox) == mailbox


@pytest.mark.parametrize("value", GO_REJECTED_MAILBOXES)
def test_mailboxes_go_rejects_are_rejected_everywhere(value: str) -> None:
    """getMessage omits to, and send and reply refuse the mailbox, as in Go."""
    assert _decode_to_header(value) == []
    with pytest.raises(ValueError):
        _format_go_mailbox(value)
    with pytest.raises(ValueError, match="recipient email is invalid"):
        _format_recipient(value)


@pytest.mark.parametrize(
    "value", ["=?UTF-8?B??= <ada@example.com>", "=?utf-8?q??= =?utf-8?q??= <ada@example.com>"]
)
def test_display_name_of_only_empty_encoded_words_reads_as_no_name(value: str) -> None:
    """Go 1.24, the Go connector's go.mod minimum and CI toolchain, reads it so; Go 1.26 rejects it."""
    assert _format_go_mailbox(value) == "<ada@example.com>"
    assert _format_recipient(value) == "<ada@example.com>"
    assert _decode_to_header(f"{value}, bob@example.com") == ["<ada@example.com>", "<bob@example.com>"]


def test_recipients_are_duplicates_by_unquoted_address() -> None:
    """Go compares the parsed address, so a quoted local part duplicates its plain form."""
    recipients = ['"alice"@example.com', "Alice <ALICE@example.com>"]
    send_input = SendMessageInput(to=recipients, subject="Progress", text_body="Update")
    with pytest.raises(ValueError, match="recipient email is duplicated"):
        validate_send_input(send_input, "owner@example.com")


def test_control_characters_are_rejected() -> None:
    """Go's net/mail admits a tab but no other raw control character in a display name."""
    with pytest.raises(ValueError, match="recipient email is invalid"):
        _format_recipient("Zoë\x01Ops <zoe@example.com>")
    with pytest.raises(ValueError, match="recipient email is invalid"):
        _format_recipient('"a\x7fb" <zoe@example.com>')


def test_send_body_writes_the_formatted_to_header() -> None:
    """The raw MIME To: header joins the formatted mailboxes unchanged."""
    call_id = CallID(str(uuid4()))
    call = Call(
        call_id,
        ConnectionRef("google", "support"),
        OperationRef("gmail", "sendMessage"),
        IdempotencyKey(call_id),
    )
    input_value = SendMessageInput([_quote("Zoë (Ops), Inc"), "other@example.com"], "Progress", "Update")
    recipients = validate_send_input(input_value, "owner@example.com")
    body = build_send_message(call, "owner@example.com", recipients, input_value, "run-1", None)
    document = json.loads(body)
    raw = base64.urlsafe_b64decode(document["raw"] + "=" * (-len(document["raw"]) % 4))
    assert b"\r\nTo: =?utf-8?b?Wm/DqyAoT3BzKSwgSW5j?= <zoe@example.com>, <other@example.com>\r\n" in raw
