# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Go net/url derived query encoding cases."""

import pytest

from dex_connectors.google_youtube.query_values import encode_query_values


@pytest.mark.parametrize(
    ("value", "escaped"),
    [
        ("~", "~"),
        ("*", "%2A"),
        ("!", "%21"),
        ("'", "%27"),
        ("(", "%28"),
        (")", "%29"),
        (" ", "+"),
        ("+", "%2B"),
        ("/", "%2F"),
        ("?", "%3F"),
        ("&", "%26"),
        ("=", "%3D"),
        ("é", "%C3%A9"),
        ("中", "%E4%B8%AD"),
    ],
)
def test_go_query_escape(value: str, escaped: str) -> None:
    assert encode_query_values({"key": value}) == "key=" + escaped


def test_go_values_encode_sorts_keys_and_keeps_value_order() -> None:
    assert encode_query_values({"z": ["1", "2"], "é": "中", "a": "~ *!'() +/?&="}) == (
        "a=~+%2A%21%27%28%29+%2B%2F%3F%26%3D&z=1&z=2&%C3%A9=%E4%B8%AD"
    )
