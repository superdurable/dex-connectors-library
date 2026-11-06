# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Compare query escaping with Go net/url.Values.Encode results."""

import pytest

from dex_connectors.superdurable_oauth2_http.client import _encode_go_query


@pytest.mark.parametrize(
    ("values", "expected"),
    [
        ([("z", "a b"), ("a", "~*!'()")], "a=~%2A%21%27%28%29&z=a+b"),
        ([("β", "1 2"), ("A", "x+y")], "A=x%2By&%CE%B2=1+2"),
    ],
)
def test_go_query_encoding(values: list[tuple[str, str]], expected: str) -> None:
    """Preserve Go's key order, literal tilde, and form-style spaces."""
    assert _encode_go_query(values) == expected
