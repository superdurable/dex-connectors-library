# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Keep Graph query and path escaping aligned with Go's net/url."""

import pytest

from dex_connectors.microsoft_outlook_mail.client import path_escape, query_escape


@pytest.mark.parametrize(
    ("value", "path", "query"),
    (
        ("a b", "a%20b", "a%20b"),
        ("~*!'()", "~%2A%21%27%28%29", "~%2A%21%27%28%29"),
        ("A+B=", "A+B=", "A%2BB%3D"),
        ("support@contoso.com", "support@contoso.com", "support%40contoso.com"),
        ("$filter", "$filter", "%24filter"),
        ("é space", "%C3%A9%20space", "%C3%A9%20space"),
        ("a&b=c", "a&b=c", "a%26b%3Dc"),
    ),
)
def test_go_escaped_components(value: str, path: str, query: str) -> None:
    """Match Go url.PathEscape and QueryEscape with Graph's %20 space rule."""
    assert path_escape(value) == path
    assert query_escape(value) == query
