# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Deterministic text copy selection decisions."""

from dex_connectors.sdk import QueryResult, Receipt

from dex_connectors.google_drive import FileSummary, ReadFileTextInput, SearchFilesOutput
from examples.text_copy.flow import SelectSourceFile


def test_selects_one_complete_search_match() -> None:
    """Pass one unambiguous source ID to the text read Step."""
    page = SearchFilesOutput(
        files=(FileSummary(id="source_file", name="Ops Policy", mime_type="text/plain"),)
    )
    decision = SelectSourceFile().execute(None, QueryResult("found", page, Receipt()))
    assert decision.kind.value == "next"
    assert decision.movements[0].input == ReadFileTextInput("source_file")


def test_rejects_incomplete_or_ambiguous_search() -> None:
    """Never upload when another page or an incomplete search may hide a duplicate."""
    source = FileSummary(id="source_file", name="Ops Policy", mime_type="text/plain")
    for page in (
        SearchFilesOutput(files=(source,), next_page_token="next"),
        SearchFilesOutput(files=(source,), is_incomplete_search=True),
        SearchFilesOutput(files=(source, source)),
    ):
        decision = SelectSourceFile().execute(None, QueryResult("found", page, Receipt()))
        assert decision.kind.value == "force_fail"
