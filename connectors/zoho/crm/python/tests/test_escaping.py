# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Go net/url escaping parity for provider paths and query values."""

import pytest

from dex_connectors.zoho_crm.client import path_escape, query_escape


@pytest.mark.parametrize(
    ("raw", "query", "path"),
    [
        ("a b", "a+b", "a%20b"),
        ("~*!'()", "~%2A%21%27%28%29", "~%2A%21%27%28%29"),
        ("a/b?c", "a%2Fb%3Fc", "a%2Fb%3Fc"),
        ("é &+", "%C3%A9+%26%2B", "%C3%A9%20&+"),
        ("x:y@z", "x%3Ay%40z", "x:y@z"),
        ("[a]", "%5Ba%5D", "%5Ba%5D"),
    ],
)
def test_go_net_url_escaping(raw: str, query: str, path: str) -> None:
    """Match values recorded by Go url.QueryEscape and url.PathEscape."""
    assert query_escape(raw) == query
    assert path_escape(raw) == path
