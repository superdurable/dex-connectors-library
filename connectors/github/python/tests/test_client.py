# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Focused GitHub client validation and transport tests."""

import httpx
import pytest
from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    IdempotencyKey,
    OperationRef,
    SecretString,
    StaticCredentialProvider,
)

from dex_connectors.github import Client, Config, Credentials, ListPullRequestFilesInput
from dex_connectors.github.client import _path_escape, _query
from dex_connectors.github.operations import _time, _url


@pytest.mark.parametrize(
    ("value", "escaped"),
    [("a/b", "a%2Fb"), ("a!b", "a%21b"), ("a$b&c+d=e:f@g", "a$b&c+d=e:f@g")],
)
def test_go_path_escape(value: str, escaped: str) -> None:
    """Keep GitHub URL segments aligned with Go url.PathEscape."""
    assert _path_escape(value) == escaped


def test_go_query_escape_and_key_order() -> None:
    """Encode sorted keys and plus-spaces like Go url.Values."""
    assert _query({"z": "a b~*", "a": "x"}) == "a=x&z=a+b~%2A"


def test_provider_url_escapes_path_and_fragment_like_go() -> None:
    assert _url("https://example.com/a b?x=a b#frag ment") == "https://example.com/a%20b?x=a b#frag%20ment"
    assert _url("https://example.com/path?") == "https://example.com/path?"


def test_provider_time_rejects_unrepresentable_nanoseconds() -> None:
    with pytest.raises(ValueError, match="exactly"):
        _time("2026-01-01T00:00:00.123456789Z")


def test_reject_non_loopback_plain_http() -> None:
    """Reject an insecure provider endpoint before constructing transport."""
    connection = ConnectionRef("github", "github")
    credentials = Credentials("client", SecretString("secret"), SecretString("token"), SecretString(""))
    with pytest.raises(ValueError, match="HTTPS or loopback"):
        Client(Config(base_url="http://api.github.com"), StaticCredentialProvider({connection: credentials}))


@pytest.mark.asyncio
async def test_request_uses_configured_timeout() -> None:
    """Honor the manifest timeout even with an injected HTTP client."""
    connection = ConnectionRef("github", "github")
    credentials = Credentials("client", SecretString("secret"), SecretString("token"), SecretString(""))
    observed: list[float] = []

    def answer(request: httpx.Request) -> httpx.Response:
        observed.append(request.extensions["timeout"]["read"])
        return httpx.Response(404, json={})

    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as transport:
        client = Client(
            Config(base_url="http://localhost", timeout_seconds=0.25),
            StaticCredentialProvider({connection: credentials}),
            transport,
        )
        call = Call(
            CallID("call-1"), connection, OperationRef("github", "listPullRequestFiles"), IdempotencyKey("")
        )
        result = await client.list_pull_request_files().invoke(
            call, ListPullRequestFilesInput("octocat", "repo", 1)
        )
    assert result.branch == "notFound"
    assert observed == [0.25]


@pytest.mark.parametrize(
    ("address", "valid"),
    [
        ("a@example.com", True),
        ("ü@example.com", True),
        ("a@exämple.com", True),
        ("a@[127.0.0.1]", True),
        ('"a b"@example.com', True),
        ("a@@example.com", False),
        ("a b@example.com", False),
        ("a..b@example.com", False),
        ("a@example..com", False),
        ("Name <a@example.com>", False),
        ("a@example.com trailing", False),
    ],
)
def test_primary_email_uses_go_mail_address_shape(address: str, valid: bool) -> None:
    """Retain the address forms Go mail.ParseAddress accepts without display names."""
    from dex_connectors.github.operations import _is_go_mail_address

    assert _is_go_mail_address(address) is valid


@pytest.mark.parametrize(
    ("value", "valid"),
    [
        ("2026-02-28T12:00:00Z", True),
        ("2024-02-29T12:00:00Z", True),
        ("2026-01-01T12:00:00,5+99:59", True),
        ("2026-02-30T12:00:00Z", False),
        ("2026-02-29T12:00:00Z", False),
        ("2026-01-01T24:00:00Z", False),
    ],
)
def test_go_rfc3339_dates(value: str, valid: bool) -> None:
    """Match Go time.Parse calendar and timezone acceptance."""
    from dex_connectors.github.operations import _time

    if valid:
        assert _time(value) is not None
    else:
        with pytest.raises(ValueError):
            _time(value)


def test_project_configuration_preserves_all_manifest_fields() -> None:
    """Decode each project field, including nanosecond timeout, into the provider client config."""
    from dex_connectors.github._generated.connection import _decode_config

    config = _decode_config(
        {
            "baseUrl": "https://api.github.com",
            "apiVersion": "2026-03-10",
            "timeout": 250_000_000,
            "maxResponseBytes": 4096,
            "defaultRepositoryLimit": 2,
            "maxRepositories": 4,
            "maxPullRequestBodyCharacters": 100,
            "maxPatchCharacters": 200,
            "maxCommitMessageCharacters": 300,
        }
    )
    assert config.timeout_seconds == 0.25
    assert config.max_response_bytes == 4096
    assert config.default_repository_limit == 2
    assert config.max_repositories == 4
    assert config.max_pull_request_body_characters == 100
    assert config.max_patch_characters == 200
    assert config.max_commit_message_characters == 300
