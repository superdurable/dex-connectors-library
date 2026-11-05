# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Check provider validation that spans more cases than recorded fixtures."""

import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.atlassian_confluence import Client, Config, Credentials, PageSearchFilter
from dex_connectors.atlassian_confluence.client import Exchange, path_escape
from dex_connectors.atlassian_confluence.operations import _build_cql, _page, _page_summary

_CONNECTION = ConnectionRef("atlassian", "confluence-pages")
_CREDENTIALS = Credentials(
    "inert-client",
    SecretString("inert-secret"),
    SecretString("inert-access-token"),
    SecretString("inert-refresh-token"),
)


def test_path_escape_matches_go_reserved_characters() -> None:
    """Keep URL path segments byte-for-byte compatible with Go url.PathEscape."""
    assert path_escape("A~$&+=:@ /?!'()*,;") == "A~$&+=:@%20%2F%3F%21%27%28%29%2A%2C%3B"


def test_cql_rejects_outer_order_by_but_accepts_quoted_words() -> None:
    """A caller clause stays inside its parenthesized CQL filter."""
    assert '(title = "ORDER BY")' in _build_cql(PageSearchFilter(additional_cql='title = "ORDER BY"'))
    for clause in ('title = "x" ORDER BY created', "parent = 1) OR type = blog", 'title = "open'):
        with pytest.raises(ValueError):
            _build_cql(PageSearchFilter(additional_cql=clause))


def test_provider_wrong_typed_fields_fail_instead_of_coercing() -> None:
    """A wrong-typed page identifier is an invalid provider response."""
    page = {
        "id": 600002,
        "title": "Policy",
        "status": "current",
        "spaceId": "98306",
        "version": {"number": 1},
        "body": {"storage": {"value": "<p>Hello</p>"}},
    }
    with pytest.raises(ValueError):
        _page(page, "storage", "markdown", 200)


def test_search_keeps_page_with_unparseable_version_time() -> None:
    summary = _page_summary(
        {
            "type": "page", "id": "600002", "title": "Policy", "status": "current",
            "space": {"id": 98306, "key": "OPS"},
            "version": {"number": 1, "when": "not-a-time"},
            "_links": {"webui": "/spaces/OPS/pages/600002"},
        },
        "https://ops.atlassian.net/wiki",
    )
    assert summary is not None and summary.last_modified_at.year == 1


@pytest.mark.asyncio
async def test_large_server_error_still_retries_by_status() -> None:
    """A large 503 response remains availability even when its body exceeds the read limit."""
    client = Client(
        Config(cloud_id="11223344-a1b2-4b33-8c44-def123456789"),
        StaticCredentialProvider({_CONNECTION: _CREDENTIALS}),
        httpx.AsyncClient(transport=httpx.MockTransport(lambda _: httpx.Response(503))),
    )
    classified = client.classify_read("getPage", "page read", Exchange(status=503, is_too_large=True))
    assert classified.outcome == "retry"
    assert classified.failure is not None and classified.failure.kind.value == "AVAILABILITY"
