# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Keep Graph path and query escaping aligned with Go net/url."""

import pytest

from dex_connectors.microsoft_teams import Config
from dex_connectors.microsoft_teams.client import (
    Client,
    Exchange,
    _parse_go_time,
    decode_message,
    extract_message_text,
    path_escape,
    query_encode,
    safe_token,
)
from dex_connectors.microsoft_teams.operations import (
    _go_json_body,
    _read_classification,
    _thread_link,
    _write_classification,
)


@pytest.mark.parametrize(
    ("values", "expected"),
    [
        ({"$top": "50"}, "%24top=50"),
        ({"space key": "a b", "!": "!*'()~", "~": "~ *"},
         "%21=%21%2A%27%28%29~&space+key=a+b&~=~+%2A"),
        ({"é": "你好 *~!", "a": "a+b/c?d"},
         "a=a%2Bb%2Fc%3Fd&%C3%A9=%E4%BD%A0%E5%A5%BD+%2A~%21"),
    ],
)
def test_query_encode_matches_go_url_values(values: dict[str, str], expected: str) -> None:
    """Check Go's byte-sorted form encoding and literal tilde."""
    assert query_encode(values) == expected


@pytest.mark.parametrize(
    ("value", "expected"),
    [
        ("19:abc@thread.tacv2", "19:abc@thread.tacv2"),
        ("a b!*'()~", "a%20b%21%2A%27%28%29~"),
        ("你好/yes", "%E4%BD%A0%E5%A5%BD%2Fyes"),
        ("-._~:/@!$&'()*+,;=?#[]", "-._~:%2F@%21$&%27%28%29%2A+%2C%3B=%3F%23%5B%5D"),
    ],
)
def test_path_escape_matches_go_url_path_escape(value: str, expected: str) -> None:
    """Check the path component form used by Go."""
    assert path_escape(value) == expected


def test_write_body_uses_go_html_safe_json_escaping() -> None:
    """Check the provider receives the same encoded JSON as Go."""
    assert _go_json_body({"content": "<>&\u2028\u2029"}) == (
        b'{"content":"\\u003c\\u003e\\u0026\\u2028\\u2029"}'
    )


def test_unclosed_html_keeps_final_text() -> None:
    assert extract_message_text("html", "Ship R&D") == "Ship R&D"


def test_percent_encoded_thread_cursor_uses_decoded_path() -> None:
    client = Client(Config(), object())  # type: ignore[arg-type]
    path = "/teams/team/channels/19:abc@thread.tacv2/messages/123/replies"
    link = "https://graph.microsoft.com/v1.0/teams/team/channels/19%3Aabc%40thread.tacv2/messages/123/replies"
    assert _thread_link(client, link, path)


def test_message_keeps_nbsp_and_zwj_but_omits_deleted_content_type() -> None:
    resource = {
        "id": "123",
        "createdDateTime": "2026-10-01T00:00:00Z",
        "subject": "A\u00a0B\u200dC\x00\ufffd",
        "from": {"user": {"id": "user", "displayName": "A\u00a0B\u200dC\x00\ufffd"}},
        "body": {"contentType": "html", "content": "Ship R&D"},
    }
    message = decode_message(resource)
    assert message.subject == "A\u00a0B\u200dC"
    assert message.sender.display_name == "A\u00a0B\u200dC"
    assert message.text == "Ship R&D"
    resource["deletedDateTime"] = "2026-10-01T00:01:00Z"
    assert decode_message(resource).content_type is None


def test_provider_token_is_limited_to_64_bytes() -> None:
    assert safe_token("x" * 64) == "x" * 64
    assert safe_token("x" * 65) == ""


def test_request_build_error_is_a_defect() -> None:
    client = Client(Config(), object())  # type: ignore[arg-type]
    exchange = Exchange(error=ValueError("invalid URL"), request_build_failed=True)
    assert _read_classification(client, "listThreadReplies", "replies", exchange)[0] == "defect"
    assert _write_classification(client, "postChannelMessage", "message", exchange)[0] == "defect"


def test_read_back_accepts_submicrosecond_timestamp() -> None:
    assert _parse_go_time("2026-10-01T00:00:00.123456789Z", "createdDateTime").microsecond == 123456
