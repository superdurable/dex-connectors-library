# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Reject lossy or non-Go Graph response numbers."""

import pytest

from dex_connectors.microsoft_outlook_mail import Config
from dex_connectors.microsoft_outlook_mail.client import Client, is_bare_address, validate_next_page_link
from dex_connectors.microsoft_outlook_mail.draft_send import _draft_created_key
from dex_connectors.microsoft_outlook_mail.message import (
    decode_attachment_list,
    decode_full_message,
    json_object,
)
from dex_connectors.microsoft_outlook_mail.operations import validate_address


@pytest.mark.parametrize("size", ["5.0", "1e3", "NaN", "Infinity", "-Infinity"])
def test_attachment_size_requires_go_integer(size: str) -> None:
    """Go's int64 decoder rejects floats, exponents, and nonfinite numbers."""
    response = ('{"value":[{"id":"AAMkABC","size":' + size + "}]}").encode()
    with pytest.raises(ValueError):
        decode_attachment_list(response)


def test_large_integer_is_not_rounded() -> None:
    """Preserve the exact int64 value when decoding an attachment."""
    attachments, truncated = decode_attachment_list(b'{"value":[{"id":"AAMkABC","size":9007199254740993}]}')
    assert attachments[0].size_bytes == 9007199254740993
    assert not truncated


def test_json_object_rejects_nan_outside_a_known_field() -> None:
    """Reject nonfinite JSON anywhere before building a connector result."""
    with pytest.raises(ValueError):
        json_object(b'{"value":[],"unexpected":NaN}')


@pytest.mark.parametrize(
    "address",
    ("jane", "@x.com", "jane doe@x.com", "a.@x.com", "jane@x.com.", "a(c)@x.com", "a..b@x.com"),
)
def test_go_rejects_malformed_bare_addresses(address: str) -> None:
    assert not is_bare_address(address)
    with pytest.raises(ValueError):
        validate_address("to", address)
    with pytest.raises(ValueError):
        Client(Config(mailbox=address), object())  # type: ignore[arg-type]


def test_go_accepts_unicode_addr_spec() -> None:
    assert is_bare_address("é@bücher.de")


def test_unclosed_html_keeps_final_text_and_go_self_closing_break_behavior() -> None:
    message = decode_full_message(
        b'{"id":"message1","body":{"contentType":"html","content":"<p>Tom&Jerry"}}'
    )
    assert message.text == "Tom&Jerry"
    with_break = decode_full_message(
        b'{"id":"message1","body":{"contentType":"html","content":"first<br/>second"}}'
    )
    assert with_break.text == "firstsecond"


def test_non_object_message_item_is_invalid_response() -> None:
    with pytest.raises(ValueError, match="message object"):
        json_object([1])  # type: ignore[arg-type]


def test_next_link_checks_decoded_path() -> None:
    with pytest.raises(ValueError):
        validate_next_page_link("https://graph.microsoft.com/v1.0/me/%2e%2e/messages")


def test_drafts_order_by_instant_and_not_timestamp_spelling() -> None:
    earlier = {"createdDateTime": "2026-01-01T00:30:00+02:00"}
    later = {"createdDateTime": "2025-12-31T23:00:00Z"}
    assert _draft_created_key(earlier) < _draft_created_key(later)
