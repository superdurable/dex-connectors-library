# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""iFLYTEK Spark HTTP chat transport and Go-compatible Query decisions."""

import json
from datetime import UTC, datetime
from typing import Any, cast
from urllib.parse import urlsplit

import httpx
from dex_connectors.sdk import (
    Call,
    CredentialProvider,
    Failure,
    FailureKind,
    QueryAttempt,
    QueryDefinition,
    Receipt,
)
from dex_connectors.sdk.providerhttp import (
    BodyTooLargeError,
    ProviderTransportError,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import CREATE_CHAT_COMPLETION_DEFINITION
from .models import ChatCompletion, CreateChatCompletionInput

_MODELS = frozenset(("lite", "generalv3", "pro-128k", "4.0Ultra"))


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ChatCompletion]:
    return QueryAttempt.branch_result(
        branch, ChatCompletion(), Receipt(), Failure(kind, "iflytek", "createChatCompletion", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[ChatCompletion]:
    return QueryAttempt.retry(Failure(kind, "iflytek", "createChatCompletion", message), delay)


class Client:
    """Own Spark settings and resolve APIPassword on each generation attempt."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Spark settings and credential source are required")
        parsed = urlsplit(config.base_url)
        if (
            parsed.scheme != "https"
            or not parsed.hostname
            or parsed.path not in ("", "/")
            or parsed.query
            or parsed.fragment
            or parsed.username
            or parsed.password
            or config.timeout_seconds <= 0
            or not 0 < config.max_response_bytes <= 16 << 20
            or config.model not in _MODELS
            or not 1 <= config.max_output_tokens <= 4096
        ):
            raise ValueError("Spark configuration is invalid")
        self.base_url = config.base_url.rstrip("/")
        self.model = config.model
        self.max_output_tokens = config.max_output_tokens
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def create_chat_completion(self) -> "CreateChatCompletionOperation":
        """Bind the nonstreaming chat Query to this client."""
        return CreateChatCompletionOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP transport when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()


class CreateChatCompletionOperation:
    """Generate one bounded assistant answer with Spark HTTP."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Spark client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated chat Query identity."""
        return CREATE_CHAT_COMPLETION_DEFINITION

    async def invoke(self, call: Call, input: CreateChatCompletionInput) -> QueryAttempt[ChatCompletion]:
        """Make one nonstreaming generation request and classify its outcome."""
        try:
            input.prompt.encode("utf-8")
        except UnicodeEncodeError:
            return _branch(
                "defect", FailureKind.VALIDATION, "Spark prompt must contain 1 to 2000 UTF-8 characters"
            )
        if not input.prompt.strip() or len(input.prompt) > 2000:
            return _branch(
                "defect", FailureKind.VALIDATION, "Spark prompt must contain 1 to 2000 UTF-8 characters"
            )
        try:
            credentials = await self.client.credentials.resolve(call)
            password = credentials.api_password.reveal()
            if not password or any(character in password for character in "\r\n "):
                raise ValueError("invalid APIPassword")
        except (ValueError, LookupError, OSError):
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "Spark APIPassword is unavailable or invalid"
            )
        try:
            request = self.client.http_client.build_request(
                "POST",
                self.client.base_url + "/v1/chat/completions",
                json={
                    "model": self.client.model,
                    "messages": [{"role": "user", "content": input.prompt}],
                    "stream": False,
                    "max_tokens": self.client.max_output_tokens,
                },
                headers={
                    "Authorization": "Bearer " + password,
                    "Content-Type": "application/json",
                    "Accept": "application/json",
                },
            )
        except (httpx.HTTPError, ValueError):
            return _retry(FailureKind.TRANSPORT, "Spark request failed")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.TRANSPORT, "Spark request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "Spark rate limited the request", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "Spark is temporarily unavailable", delay)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Spark response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.TRANSPORT, "Spark response was interrupted")
            if status != 200:
                return _branch(
                    "providerRejected", FailureKind.PROVIDER_REJECTION, f"Spark rejected HTTP {status}"
                )
        finally:
            await response.aclose()
        return _decode_completion(body, self.client.model, delay)


def _decode_completion(body: bytes, model: str, delay: float) -> QueryAttempt[ChatCompletion]:
    try:
        document: Any = json.loads(body.decode("utf-8"))
    except (ValueError, UnicodeDecodeError):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "Spark returned invalid JSON")
    if not isinstance(document, dict) or type(document.get("code")) is not int:
        return _branch("invalidResponse", FailureKind.PROTOCOL, "Spark omitted code")
    code = document["code"]
    if code in (10007, 11202, 11203):
        return _retry(FailureKind.RATE_LIMIT, f"Spark rate limited generation ({code})", delay)
    if code != 0:
        return _branch(
            "providerRejected", FailureKind.PROVIDER_REJECTION, f"Spark rejected generation ({code})"
        )
    choices = document.get("choices")
    usage = document.get("usage")
    if not isinstance(choices, list) or len(choices) != 1 or not isinstance(choices[0], dict):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "Spark returned invalid assistant content")
    message = choices[0].get("message")
    if (
        not isinstance(message, dict)
        or message.get("role") != "assistant"
        or not isinstance(message.get("content"), str)
        or not message["content"]
        or len(message["content"].encode("utf-8")) > 65536
    ):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "Spark returned invalid assistant content")
    if not isinstance(usage, dict):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "Spark returned invalid token usage")
    tokens = (usage.get("prompt_tokens"), usage.get("completion_tokens"), usage.get("total_tokens"))
    if any(type(value) is not int or value < 0 for value in tokens):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "Spark returned invalid token usage")
    return QueryAttempt.branch_result(
        "completed",
        ChatCompletion(
            content=message["content"],
            model=model,
            prompt_tokens=cast(int, tokens[0]),
            completion_tokens=cast(int, tokens[1]),
            total_tokens=cast(int, tokens[2]),
        ),
        Receipt(),
    )
