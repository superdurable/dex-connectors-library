# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Check Go-compatible Notion input boundaries and request encoding."""

import pytest

from dex_connectors.notion import DateRange, PropertyValue, QueryFilter, QuerySort
from dex_connectors.notion.client import escaped_path
from dex_connectors.notion.encoding import (
    cursor,
    encode_filter,
    encode_paragraphs,
    encode_properties,
    encode_sorts,
    select_data_source,
)
from dex_connectors.notion.provider_models import decode_page, parse_id


@pytest.mark.parametrize(
    "identifier",
    [
        "1F3C9A2E5B7D4E8A9C0123456789ABCD",
        "1F3C9A2E-5B7D-4E8A-9C01-23456789ABCD",
        "https://www.notion.so/workspace/Page-1F3C9A2E5B7D4E8A9C0123456789ABCD?v=view",
    ],
)
def test_parse_id_accepts_verified_notion_forms(identifier: str) -> None:
    """Canonical IDs survive URL and compact spellings."""
    assert parse_id(identifier) == "1f3c9a2e-5b7d-4e8a-9c01-23456789abcd"


def test_path_escape_matches_go_reserved_segment_rules() -> None:
    """Go PathEscape retains its permitted punctuation and escapes slashes."""
    assert escaped_path("a/b?c d:@") == "a%2Fb%3Fc%20d:@"
    assert escaped_path("!$&'()*+,;=") == "%21$&%27%28%29%2A+%2C%3B="


def test_property_encoding_preserves_clear_and_explicit_false() -> None:
    """Null clears and false checkbox values keep distinct wire meanings."""
    assert encode_properties({"Done": PropertyValue(type="checkbox", checkbox=False)}) == {
        "Done": {"checkbox": False}
    }
    assert encode_properties({"Due": PropertyValue(type="date")}) == {"Due": {"date": None}}
    with pytest.raises(ValueError, match="checkbox"):
        encode_properties({"Done": PropertyValue(type="checkbox")})


@pytest.mark.parametrize("invalid_address", ["a b@example.com", "a@example..com", "a@@b"])
def test_email_validation_rejects_addresses_go_mail_parse_address_rejects(invalid_address: str) -> None:
    """Require one syntactically valid addr-spec, including dot and whitespace rules."""
    with pytest.raises(ValueError, match="email"):
        encode_properties({"Email": PropertyValue(type="email", email=invalid_address)})


def test_date_and_rich_text_chunk_boundaries() -> None:
    """Large Unicode text splits at UTF-16 units and dates map to Notion names."""
    text = "😀" * 1001
    rich = encode_properties({"Name": PropertyValue(type="title", text=text)})["Name"]["title"]
    assert len(rich) == 2
    due = encode_properties(
        {"Due": PropertyValue(type="date", date=DateRange("2026-11-02", time_zone="America/Los_Angeles"))}
    )
    assert due["Due"]["date"]["time_zone"] == "America/Los_Angeles"
    assert encode_properties(
        {"Due": {"type": "date", "date": {"start": "2026-11-02", "timeZone": "America/Los_Angeles"}}}
    ) == due


def test_page_rejects_malformed_property_objects() -> None:
    """A malformed provider property must fail the page response."""
    with pytest.raises(ValueError, match="unusable page"):
        decode_page(
            {
                "object": "page",
                "id": "1f3c9a2e-5b7d-4e8a-9c01-23456789abcd",
                "properties": {"Name": "invalid"},
            }
        )


def test_filter_and_sort_validation() -> None:
    """Typed conditions and sort directions reject unsupported combinations."""
    query = QueryFilter(property="Name", type="title", condition="contains", text="Ada")
    assert encode_filter(query) == {"property": "Name", "title": {"contains": "Ada"}}
    assert encode_sorts((QuerySort(property="Name", direction="descending"),)) == [
        {"property": "Name", "direction": "descending"}
    ]
    with pytest.raises(ValueError, match="exactly one"):
        encode_filter(QueryFilter(property="Name", type="title", condition="contains", text="Ada", number=1))


def test_selection_cursor_and_paragraph_limits() -> None:
    """Only one source ID, printable cursors, and bounded paragraphs are accepted."""
    identifier = "2a4d6f80-1b3c-4d5e-8f70-112233445566"
    assert select_data_source(identifier, "") == (identifier, "")
    with pytest.raises(ValueError, match="exactly one"):
        select_data_source(identifier, identifier)
    with pytest.raises(ValueError, match="cursor"):
        cursor("bad\nvalue")
    assert len(encode_paragraphs("first\n\nsecond")) == 2
