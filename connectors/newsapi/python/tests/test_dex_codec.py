# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Released Dex 1.28 codec parity for NewsAPI's Go wire shapes."""

import json
from datetime import UTC, datetime

from dex import CodecRegistry, Value, WireKind

from dex_connectors.newsapi import ArticlePage, SearchArticlesInput
from examples.company_news.request import CompanyNewsRequest


def test_search_input_omits_defaults_and_round_trips_go_names() -> None:
    codec = CodecRegistry().resolve(SearchArticlesInput)
    input_value = SearchArticlesInput(query="Meridian", search_in=("title",), page_size=20)
    encoded = codec.encode(input_value)
    assert isinstance(encoded.data, str)
    assert json.loads(encoded.data) == {
        "query": "Meridian",
        "searchIn": ["title"],
        "pageSize": 20,
    }
    assert codec.decode(encoded) == input_value
    assert codec.decode(Value(WireKind.JSON, '{"query":"Meridian"}')) == SearchArticlesInput(query="Meridian")


def test_article_page_decodes_go_nullable_fields_and_utc_timestamp() -> None:
    codec = CodecRegistry().resolve(ArticlePage)
    go_page = {
        "totalResults": 1,
        "page": 1,
        "hasMore": False,
        "articles": [
            {
                "source": {"id": None, "name": "Meridian News"},
                "author": None,
                "title": "Meridian opens a lab",
                "description": "",
                "url": "https://news.example/lab",
                "urlToImage": None,
                "publishedAt": "2026-10-26T12:00:00Z",
                "content": None,
            }
        ],
    }
    page = codec.decode(Value(WireKind.JSON, json.dumps(go_page)))
    assert page.articles[0].description == ""
    assert page.articles[0].author is None
    assert page.articles[0].published_at == datetime(2026, 10, 26, 12, tzinfo=UTC)
    assert codec.decode(codec.encode(page)) == page
    reencoded = codec.encode(page)
    assert isinstance(reencoded.data, str)
    encoded = json.loads(reencoded.data)
    assert encoded["totalResults"] == 1 and encoded["hasMore"] is False
    assert encoded["articles"][0]["publishedAt"] == "2026-10-26T12:00:00Z"
    offset_wire = json.loads(json.dumps(go_page))
    offset_wire["articles"][0]["publishedAt"] = "2026-10-26T05:00:00-07:00"
    offset_page = codec.decode(Value(WireKind.JSON, json.dumps(offset_wire)))
    assert offset_page.articles[0].published_at == page.articles[0].published_at


def test_go_mock_article_with_missing_nullable_fields_decodes() -> None:
    codec = CodecRegistry().resolve(ArticlePage)
    page = codec.decode(
        Value(
            WireKind.JSON,
            json.dumps(
                {
                    "totalResults": 1,
                    "page": 1,
                    "hasMore": False,
                    "articles": [{"source": {"name": "Meridian News"}, "title": "[Removed]"}],
                }
            ),
        )
    )
    assert page.articles[0].source.id is None
    assert page.articles[0].author is None
    assert page.articles[0].published_at == datetime.min.replace(tzinfo=UTC)


def test_example_start_input_keeps_go_field_names() -> None:
    codec = CodecRegistry().resolve(CompanyNewsRequest)
    request = CompanyNewsRequest("Meridian")
    encoded = codec.encode(request)
    assert isinstance(encoded.data, str)
    assert json.loads(encoded.data) == {
        "company": "Meridian",
        "days": 0,
        "maxArticles": 0,
        "language": "",
    }
    assert codec.decode(Value(WireKind.JSON, '{"company":"Meridian"}')) == request
