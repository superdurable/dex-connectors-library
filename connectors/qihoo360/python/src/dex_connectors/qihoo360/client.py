# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Qihoo360 text-generation transport and Go-compatible query decisions."""

import json
import re
from datetime import UTC, datetime

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
from ._generated.definitions import GENERATE_TEXT_DEFINITION
from .models import GenerateTextInput, GenerateTextOutput, TokenUsage

COMPLETIONS_URL = "https://api.360.cn/v1/chat/completions"
MODEL_PATTERN = re.compile(r"[A-Za-z0-9_./-]{1,128}\Z")


def _api_key(value: object) -> bool:
    return isinstance(value, str) and 0 < len(value) <= 512 and all(0x20 < ord(c) < 0x7F for c in value)


class Client:
    """Resolve Qihoo360 API credentials for each text generation."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if (
            config is None
            or credentials is None
            or not MODEL_PATTERN.fullmatch(config.model)
            or config.timeout_seconds <= 0
            or not 0 < config.max_response_bytes <= 16 << 20
        ):
            raise ValueError("Qihoo360 model, response bound, and credentials are required")
        self.config = config
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_http_client = http_client is None

    def generate_text(self) -> "GenerateTextOperation":
        """Bind the nonstreaming text-generation query."""
        return GenerateTextOperation(self)

    async def aclose(self) -> None:
        """Close the owned transport."""
        if self._owns_http_client:
            await self.http_client.aclose()


class GenerateTextOperation:
    """Generate bounded text and classify the provider outcome."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated query definition."""
        return GENERATE_TEXT_DEFINITION

    def _failure(self, kind: FailureKind) -> Failure:
        return Failure(kind, "qihoo360", "generateText", "Qihoo360 request failed")

    def _branch(
        self, branch: str, kind: FailureKind, output: GenerateTextOutput | None = None
    ) -> QueryAttempt[GenerateTextOutput]:
        return QueryAttempt.branch_result(
            branch, output or GenerateTextOutput(), Receipt(), self._failure(kind)
        )

    def _retry(self, kind: FailureKind, delay: float = 0) -> QueryAttempt[GenerateTextOutput]:
        return QueryAttempt.retry(self._failure(kind), delay)

    async def invoke(self, call: Call, input: GenerateTextInput) -> QueryAttempt[GenerateTextOutput]:
        """Generate text without storing the provider API key or prompt in logs."""
        if (
            not isinstance(input, GenerateTextInput)
            or not input.prompt.strip()
            or len(input.prompt.encode()) > 64 << 10
            or not 0 <= input.max_output_tokens <= 4096
        ):
            return self._branch("defect", FailureKind.VALIDATION)
        try:
            credentials = await self.client.credentials.resolve(call)
        except (ValueError, LookupError, OSError, AttributeError):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        token = credentials.api_key.reveal()
        if not _api_key(token):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        body = json.dumps(
            {
                "model": self.client.config.model,
                "messages": [{"role": "user", "content": input.prompt}],
                "stream": False,
                "max_tokens": input.max_output_tokens or 1024,
            },
            separators=(",", ":"),
        )
        request = self.client.http_client.build_request(
            "POST",
            COMPLETIONS_URL,
            content=body,
            headers={
                "Authorization": "Bearer " + token,
                "Content-Type": "application/json",
                "Accept": "application/json",
            },
        )
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return self._retry(FailureKind.AVAILABILITY)
        try:
            status = response.status_code
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return self._retry(FailureKind.RATE_LIMIT, delay)
            if status == 408 or status >= 500:
                return self._retry(FailureKind.AVAILABILITY, delay)
            if status != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if status == 401
                    else FailureKind.AUTHORIZATION
                    if status == 403
                    else FailureKind.PROVIDER_REJECTION
                )
                return self._branch("providerRejected", kind)
            try:
                data = await read_bounded_body(response, self.client.config.max_response_bytes)
            except BodyTooLargeError:
                return self._branch("invalidResponse", FailureKind.RESPONSE_TOO_LARGE)
            except ProviderTransportError:
                return self._retry(FailureKind.AVAILABILITY)
        finally:
            await response.aclose()
        try:
            wire = json.loads(data)
        except (ValueError, UnicodeDecodeError):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        if isinstance(wire, dict) and type(wire.get("code")) is int and wire["code"] != 0:
            if wire["code"] == 1003:
                return self._retry(FailureKind.AVAILABILITY, delay)
            if wire["code"] in (1005, 10133):
                return self._retry(FailureKind.RATE_LIMIT, delay)
            return self._branch(
                "providerRejected",
                FailureKind.AUTHENTICATION if wire["code"] == 1002 else FailureKind.PROVIDER_REJECTION,
            )
        if (
            not isinstance(wire, dict)
            or not isinstance(wire.get("id"), str)
            or not wire["id"]
            or not isinstance(wire.get("model"), str)
            or not wire["model"]
            or not isinstance(wire.get("choices"), list)
            or len(wire["choices"]) != 1
        ):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        choice = wire["choices"][0]
        usage = wire.get("usage", {})
        if not isinstance(usage, dict):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        if (
            not isinstance(choice, dict)
            or not isinstance(choice.get("message"), dict)
            or choice["message"].get("role") != "assistant"
            or choice["message"].get("tool_calls") is not None
            or any(
                type(usage.get(name, 0)) is not int or usage.get(name, 0) < 0
                for name in ("prompt_tokens", "completion_tokens", "total_tokens")
            )
        ):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        reason = choice.get("finish_reason")
        content = choice["message"].get("content")
        if content is not None and not isinstance(content, str):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        output = GenerateTextOutput(
            text=content or "",
            model=wire["model"],
            response_id=wire["id"],
            finish_reason=reason if isinstance(reason, str) else "",
            usage=TokenUsage(
                prompt_tokens=usage.get("prompt_tokens", 0),
                completion_tokens=usage.get("completion_tokens", 0),
                total_tokens=usage.get("total_tokens", 0),
            ),
        )
        if reason in ("stop", "") and output.text:
            return QueryAttempt.branch_result("generated", output, Receipt())
        if reason == "length":
            return QueryAttempt.branch_result("truncated", output, Receipt())
        if reason in ("sensitive", "content_filter"):
            return self._branch("blocked", FailureKind.PROVIDER_REJECTION, output)
        return self._branch("invalidResponse", FailureKind.PROTOCOL)
