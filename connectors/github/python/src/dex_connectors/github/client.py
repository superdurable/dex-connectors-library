# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Authenticated GitHub HTTP client and operation bindings."""

import ipaddress
import json
import re
from dataclasses import dataclass, replace
from datetime import UTC, datetime
from typing import Any
from urllib.parse import quote, quote_plus, urlencode, urlsplit

import httpx
from dex_connectors.sdk import Call, CredentialProvider, Failure, FailureKind, QueryAttempt, Receipt
from dex_connectors.sdk.providerhttp import (
    BodyTooLargeError,
    ProviderTransportError,
    new_provider_http_client,
    read_bounded_body,
    send_traced,
)

from ._generated import definitions as definitions
from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials

_LOGIN = re.compile(r"[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?\Z")
_REPOSITORY = re.compile(r"[A-Za-z0-9._-]{1,100}\Z")
_API_VERSION = re.compile(r"\d{4}-\d{2}-\d{2}\Z")


def _is_loopback(host: str | None) -> bool:
    if host is None:
        return False
    if host.lower() == "localhost":
        return True
    try:
        return ipaddress.ip_address(host).is_loopback
    except ValueError:
        return False


def _path_escape(value: str) -> str:
    return quote(value, safe="-._~$&+=:@")


def _query(values: dict[str, str]) -> str:
    return urlencode(sorted(values.items()), quote_via=quote_plus).replace("~", "%7E")


def _failure(operation: str, kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "github", operation, message)


class _Response:
    def __init__(self, response: httpx.Response, body: bytes) -> None:
        self.status = response.status_code
        self.headers = response.headers
        self.body = body
        self.request_id = ""
        for name in ("X-GitHub-Request-Id", "X-Request-Id"):
            candidate = response.headers.get(name, "").strip()
            if candidate and len(candidate) <= 128 and re.fullmatch(r"[A-Za-z0-9_.:/-]+", candidate):
                self.request_id = candidate
                break

    def receipt(self, metadata: dict[str, str] | None = None) -> Receipt:
        return Receipt(provider_request_id=self.request_id, metadata=metadata or {})


def _decode(body: bytes, expected: type) -> Any:
    value = json.loads(body)
    if not isinstance(value, expected):
        raise ValueError("GitHub response type differs")
    return value


def _scopes_match(response: _Response) -> bool:
    return {
        scope.strip() for scope in response.headers.get("X-OAuth-Scopes", "").split(",") if scope.strip()
    } == {"read:user", "user:email"}


@dataclass(slots=True, repr=False)
class _CredentialState:
    value: Credentials


class Client:
    """Execute bounded GitHub queries and own the shared provider and refresh transport."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        now: Any = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("GitHub configuration and credentials are required")
        defaults = Config()
        config = replace(
            config,
            base_url=config.base_url or defaults.base_url,
            api_version=config.api_version or defaults.api_version,
            timeout_seconds=config.timeout_seconds or defaults.timeout_seconds,
            max_response_bytes=config.max_response_bytes or defaults.max_response_bytes,
            default_repository_limit=config.default_repository_limit or defaults.default_repository_limit,
            max_repositories=config.max_repositories or defaults.max_repositories,
            max_pull_request_body_characters=(
                config.max_pull_request_body_characters or defaults.max_pull_request_body_characters
            ),
            max_patch_characters=config.max_patch_characters or defaults.max_patch_characters,
            max_commit_message_characters=(
                config.max_commit_message_characters or defaults.max_commit_message_characters
            ),
        )
        endpoint = urlsplit(config.base_url)
        if (
            not endpoint.scheme
            or not endpoint.hostname
            or (endpoint.scheme != "https" and not _is_loopback(endpoint.hostname))
        ):
            raise ValueError("GitHub base URL must be HTTPS or loopback")
        if (
            endpoint.username is not None
            or endpoint.password is not None
            or endpoint.query
            or endpoint.fragment
        ):
            raise ValueError("GitHub base URL cannot contain user info, query, or fragment")
        if not _API_VERSION.fullmatch(config.api_version):
            raise ValueError("GitHub API version must use YYYY-MM-DD")
        if config.max_repositories > 500 or config.default_repository_limit > config.max_repositories:
            raise ValueError("GitHub repository limits are invalid")
        if any(
            value <= 0
            for value in (
                config.timeout_seconds,
                config.max_response_bytes,
                config.default_repository_limit,
                config.max_repositories,
                config.max_pull_request_body_characters,
                config.max_patch_characters,
                config.max_commit_message_characters,
            )
        ):
            raise ValueError("GitHub configuration limits must be positive")
        self.config = config
        self._credentials = credentials
        self._http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._base_url = config.base_url.rstrip("/")
        self._now = now or (lambda: datetime.now(UTC))

    async def aclose(self) -> None:
        """Close the shared HTTP transport after Worker shutdown."""
        await self._http_client.aclose()

    async def _credential(
        self, call: Call, operation: str, empty: Any
    ) -> tuple[_CredentialState | None, QueryAttempt[Any] | None]:
        try:
            credential = await self._credentials.resolve(call)
            if not isinstance(credential, Credentials) or not credential.access_token.reveal():
                raise ValueError("GitHub access token is required")
            return _CredentialState(credential), None
        except Exception:
            return None, self._branch(
                operation,
                "defect",
                empty,
                FailureKind.AUTHENTICATION,
                "GitHub authorization is unavailable or revoked",
            )

    async def _get(
        self, call: Call, credential: _CredentialState, path: str, query: dict[str, str] | None = None
    ) -> _Response:
        target = self._base_url + path + (("?" + _query(query)) if query else "")
        for attempt in range(2):
            request = self._http_client.build_request(
                "GET",
                target,
                headers={
                    "Accept": "application/vnd.github+json",
                    "Authorization": "Bearer " + credential.value.access_token.reveal(),
                    "User-Agent": "superdurable-dex-github-connector/0.1",
                    "X-GitHub-Api-Version": self.config.api_version,
                },
                timeout=self.config.timeout_seconds,
            )
            response, _ = await send_traced(self._http_client, request)
            try:
                body = await read_bounded_body(response, self.config.max_response_bytes)
            except BodyTooLargeError:
                raise _Oversized(_Response(response, b"")) from None
            result = _Response(response, body)
            if (
                response.status_code != 401
                or attempt == 1
                or not hasattr(self._credentials, "resolve_after_rejection")
            ):
                return result
            try:
                replacement = await self._credentials.resolve_after_rejection(call)
            except Exception:
                return result
            if not isinstance(replacement, Credentials) or not replacement.access_token.reveal():
                return result
            credential.value = replacement
        raise AssertionError("request attempt exhausted")

    def _branch(
        self,
        operation: str,
        branch: str,
        empty: Any,
        kind: FailureKind,
        message: str,
        receipt: Receipt | None = None,
    ) -> QueryAttempt[Any]:
        return QueryAttempt.branch_result(
            branch, empty, receipt or Receipt(), _failure(operation, kind, message)
        )

    def _classify(self, operation: str, response: _Response, empty: Any) -> QueryAttempt[Any] | None:
        status = response.status
        if 200 <= status < 300:
            return None
        if status == 429 or (
            status == 403
            and (
                response.headers.get("Retry-After")
                or response.headers.get("X-RateLimit-Remaining") == "0"
                or self._rate_limit_message(response.body)
            )
        ):
            delay = 60.0
            retry_after = response.headers.get("Retry-After", "")
            if retry_after.isdecimal() and int(retry_after) > 0:
                delay = float(int(retry_after))
            elif response.headers.get("X-RateLimit-Remaining") == "0":
                reset = response.headers.get("X-RateLimit-Reset", "")
                if reset.isdecimal():
                    delay = max(0.0, int(reset) - self._now().timestamp()) or 60.0
            return QueryAttempt.retry(
                _failure(operation, FailureKind.RATE_LIMIT, "GitHub rate limit was reached"), delay
            )
        cases = {
            401: (
                "authorizationRevoked",
                FailureKind.AUTHENTICATION,
                "GitHub authorization is invalid or revoked",
            ),
            403: (
                "insufficientScope",
                FailureKind.AUTHORIZATION,
                "GitHub authorization is forbidden or lacks scope",
            ),
            404: ("notFound", FailureKind.NOT_FOUND, "GitHub resource was not found"),
        }
        if status in cases:
            branch, kind, message = cases[status]
            return self._branch(operation, branch, empty, kind, message, response.receipt())
        if status >= 500:
            return QueryAttempt.retry(_failure(operation, FailureKind.AVAILABILITY, "GitHub is unavailable"))
        return self._branch(
            operation,
            "providerRejected",
            empty,
            FailureKind.PROVIDER_REJECTION,
            "GitHub rejected the request",
            response.receipt(),
        )

    @staticmethod
    def _rate_limit_message(body: bytes) -> bool:
        try:
            value = _decode(body, dict)
            return isinstance(value.get("message"), str) and "rate limit" in value["message"].lower()
        except (ValueError, json.JSONDecodeError):
            return False

    async def _request(
        self,
        call: Call,
        operation: str,
        credential: _CredentialState,
        path: str,
        query: dict[str, str],
        empty: Any,
        oversized_message: str,
    ) -> tuple[_Response | None, QueryAttempt[Any] | None]:
        try:
            response = await self._get(call, credential, path, query)
        except _Oversized as error:
            return None, self._branch(
                operation,
                "invalidResponse",
                empty,
                FailureKind.RESPONSE_TOO_LARGE,
                oversized_message,
                error.response.receipt(),
            )
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return None, QueryAttempt.retry(
                _failure(operation, FailureKind.AVAILABILITY, "GitHub is unavailable")
            )
        classified = (
            None
            if operation == "listCommits" and response.status == 409
            else self._classify(operation, response, empty)
        )
        if classified is not None:
            return response, classified
        if not _scopes_match(response):
            return response, self._branch(
                operation,
                "insufficientScope",
                empty,
                FailureKind.AUTHORIZATION,
                "GitHub OAuth grant does not match required scopes",
                response.receipt(),
            )
        return response, None

    def get_authenticated_profile(self) -> Any:
        """Return the authenticated profile Query."""
        from .operations import GetAuthenticatedProfileOperation

        return GetAuthenticatedProfileOperation(self)

    def list_public_repositories(self) -> Any:
        """Return the public repositories Query."""
        from .operations import ListPublicRepositoriesOperation

        return ListPublicRepositoriesOperation(self)

    def list_merged_pull_requests(self) -> Any:
        """Return the merged pull requests Query."""
        from .operations import ListMergedPullRequestsOperation

        return ListMergedPullRequestsOperation(self)

    def list_pull_request_files(self) -> Any:
        """Return the changed files Query."""
        from .operations import ListPullRequestFilesOperation

        return ListPullRequestFilesOperation(self)

    def list_releases(self) -> Any:
        """Return the releases Query."""
        from .operations import ListReleasesOperation

        return ListReleasesOperation(self)

    def list_commits(self) -> Any:
        """Return the commits Query."""
        from .operations import ListCommitsOperation

        return ListCommitsOperation(self)


class _Oversized(Exception):
    def __init__(self, response: _Response) -> None:
        self.response = response
