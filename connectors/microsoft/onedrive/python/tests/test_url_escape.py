# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Go net/url.QueryEscape and PathEscape reference vectors."""

import pytest

from dex_connectors.microsoft_onedrive.client import path_escape, query_escape
from dex_connectors.microsoft_onedrive.operations import _go_equal_fold, _go_path_extension


@pytest.mark.parametrize(
    ("source", "query", "path"),
    [
        ("a b~*!'()", "a+b~%2A%21%27%28%29", "a%20b~%2A%21%27%28%29"),
        ("a/b;c,d?e", "a%2Fb%3Bc%2Cd%3Fe", "a%2Fb%3Bc%2Cd%3Fe"),
        ("é &+%", "%C3%A9+%26%2B%25", "%C3%A9%20&+%25"),
        ("$select=id,name", "%24select%3Did%2Cname", "$select=id%2Cname"),
        ("O'Neil", "O%27Neil", "O%27Neil"),
    ],
)
def test_go_url_escape(source: str, query: str, path: str) -> None:
    """Preserve Go's exact escaped bytes, including literal tilde and path delimiters."""
    assert query_escape(source) == query
    assert path_escape(source) == path


def test_go_equal_fold_does_not_expand_sharp_s() -> None:
    assert not _go_equal_fold("Straße", "STRASSE")
    assert _go_equal_fold("Straße", "STRAẞE")


def test_go_path_extension_includes_leading_dot() -> None:
    assert _go_path_extension(".txt") == ".txt"
    assert _go_path_extension("folder/file.txt") == ".txt"
