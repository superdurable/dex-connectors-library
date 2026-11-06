# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Keep Graph path and query escaping aligned with Go net/url."""

import pytest

from dex_connectors.microsoft_teams.client import path_escape, query_encode
from dex_connectors.microsoft_teams.operations import _go_json_body


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
