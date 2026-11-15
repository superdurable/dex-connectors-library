# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""The search cursor read from _links.next matches Go's readNextCursor on Go-recorded links."""

import json
from pathlib import Path
from typing import Any

import pytest

from dex_connectors.atlassian_confluence._go_url import read_next_cursor
from dex_connectors.atlassian_confluence._page_change_search import decode_page_change_search

CASES: list[dict[str, Any]] = json.loads(
    (Path(__file__).parent / "fixtures" / "go-next-link-cursors.json").read_text()
)["cases"]


@pytest.mark.parametrize("case", CASES, ids=lambda case: repr(case["nextLink"]))
def test_next_link_cursor_matches_go(case: dict[str, Any]) -> None:
    if case["isValid"]:
        assert read_next_cursor(case["nextLink"]) == case["cursor"]
    else:
        with pytest.raises(ValueError):
            read_next_cursor(case["nextLink"])


@pytest.mark.parametrize("case", CASES, ids=lambda case: repr(case["nextLink"]))
def test_page_change_search_reads_the_go_cursor(case: dict[str, Any]) -> None:
    body = json.dumps({"results": [], "_links": {"next": case["nextLink"]}}).encode()
    if case["isValid"]:
        assert decode_page_change_search(body).search_cursor == case["cursor"]
    else:
        with pytest.raises(ValueError, match="invalid search cursor"):
            decode_page_change_search(body)
