# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Check URL escaping against Go net/url QueryEscape and PathEscape."""

import pytest

from dex_connectors.zoho_desk.client import _query_escape
from dex_connectors.zoho_desk.operations import _path


@pytest.mark.parametrize(
    ("value", "query", "path"),
    [
        ("a b", "a+b", "/tickets/a%20b"),
        ("~*!'()", "~%2A%21%27%28%29", "/tickets/~%2A%21%27%28%29"),
        ("a/b?c", "a%2Fb%3Fc", "/tickets/a%2Fb%3Fc"),
        ("é 中文", "%C3%A9+%E4%B8%AD%E6%96%87", "/tickets/%C3%A9%20%E4%B8%AD%E6%96%87"),
        ("$&+=:@", "%24%26%2B%3D%3A%40", "/tickets/$&+=:@"),
    ],
)
def test_go_url_escaping(value: str, query: str, path: str) -> None:
    assert _query_escape(value) == query
    assert _path(value) == path
