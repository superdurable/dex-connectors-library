# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""One safe, heartbeat-backed provider text generation attempt."""

from __future__ import annotations

import asyncio
import math
import re
from collections.abc import AsyncIterator, Awaitable, Callable
from dataclasses import dataclass, replace
from datetime import UTC, datetime, timedelta
from typing import Any

import httpx

from ..credentials import SecretString
from ..errors import Failure, FailureKind
from ..providerhttp import (
    MAX_ERROR_BODY_BYTES,
    BodyTooLargeError,
    ProviderTransportError,
    is_header_safe_credential,
    new_provider_http_client,
    parse_retry_after,
    read_error_tokens,
    send_traced,
    validate_base_url,
)
from ..types import BranchDefinition, Call, QueryAttempt, QueryDefinition, Receipt
from .contracts import (
    BLOCKED_BRANCH_ID,
    DEFECT_BRANCH_ID,
    GENERATED_BRANCH_ID,
    INVALID_RESPONSE_BRANCH_ID,
    PROVIDER_REJECTED_BRANCH_ID,
    TEXT_GENERATION_OPERATION_ID,
    TRUNCATED_BRANCH_ID,
    DecodedResponse,
    EncodedRequest,
    EncodeRequestInput,
    ErrorOutcome,
    FinishReason,
    ModelIDRule,
    ProviderReportedError,
    RequestFeatures,
    StructuredOutput,
    StructuredOutputMode,
    TextGenerationRequest,
    TextGenerationResponse,
    WireFormat,
    invalid_response_outcome,
    provider_rejected_outcome,
    quota_exhausted_outcome,
    resolve_model,
    retry_outcome,
)
from .structured_output import (
    _check_portable_request,
    _extract_structured_answer_from_schema,
    json_object_instruction,
    transform_schema_for_provider,
)

_TOKEN = re.compile(r"^[A-Za-z0-9_.:-]{1,64}$")
_HEADER = re.compile(r"^[!#$%&'*+.^_`|~0-9A-Za-z-]{1,128}$")


class _ProviderStalledError(TimeoutError):
    pass


def text_generation_branch_definitions() -> tuple[BranchDefinition, ...]:
    """Return the six stable branches; only generated is required."""
    return (
        BranchDefinition(GENERATED_BRANCH_ID, "The model finished normally and returned text."),
        BranchDefinition(
            TRUNCATED_BRANCH_ID,
            "The model stopped at the output token limit and returned any partial text.",
            True,
        ),
        BranchDefinition(
            BLOCKED_BRANCH_ID,
            "The provider stopped the response for a content policy, or the model refused.",
            True,
        ),
        BranchDefinition(
            PROVIDER_REJECTED_BRANCH_ID, "The provider conclusively rejected the request.", True
        ),
        BranchDefinition(
            INVALID_RESPONSE_BRANCH_ID,
            "The provider returned a malformed, oversized, or unusable response.",
            True,
        ),
        BranchDefinition(
            DEFECT_BRANCH_ID,
            "Local input, connection configuration, or connector definition is invalid.",
            True,
        ),
    )


@dataclass(frozen=True, slots=True)
class TextGenerationQueryConfig:
    """One connection's operation, dialect, HTTP limits, and credential resolver."""

    definition: QueryDefinition
    wire_format: WireFormat
    base_url: str
    connection_model: str
    request_timeout_seconds: float
    resolve_credential: Callable[[Call], Awaitable[SecretString]]
    max_response_bytes: int
    max_stream_event_bytes: int = 0
    http_client: httpx.AsyncClient | None = None


class TextGenerationQuery:
    """Portable generateText Query; every invocation owns its mutable attempt state.

    The Query requires sync Execute durability. Stream deltas are provisional
    across retries; only the terminal Result text is authoritative. The
    caller owns any supplied HTTP client and must keep it open during calls.
    """

    _config: TextGenerationQueryConfig
    _wire_format: WireFormat
    _base_url: str
    _connection_model: str
    _heartbeat_interval_seconds: float

    def __init__(self, config: TextGenerationQueryConfig) -> None:
        if config is None:
            raise ValueError("text generation query config is required")
        config.definition.validate()
        if config.definition.operation.operation_id != TEXT_GENERATION_OPERATION_ID:
            raise ValueError("text generation query definition must use operation ID generateText")
        expected = {branch.id: branch.optional for branch in text_generation_branch_definitions()}
        actual = {branch.id: branch.optional for branch in config.definition.branches or ()}
        if len(config.definition.branches or ()) != len(expected) or actual != expected:
            raise ValueError(
                "generateText must declare exactly the six text generation branches and optionality"
            )
        defaults = config.definition.step_defaults
        if defaults.execute_durability != 1:
            raise ValueError("generateText must use sync Execute durability")
        if timedelta(0) < defaults.heartbeat_timeout < timedelta(seconds=10):
            raise ValueError("generateText heartbeat timeout must be zero or at least 10 seconds")
        if (
            defaults.execute_method_timeout
            and defaults.execute_method_timeout.total_seconds() <= config.request_timeout_seconds
        ):
            raise ValueError("generateText Execute timeout must exceed the request timeout")
        _validate_wire_format(config.wire_format)
        self._base_url = validate_base_url(config.base_url)
        self._connection_model = config.connection_model.strip()
        if self._connection_model:
            self._connection_model = config.wire_format.model_id_rule.validate_model_id(
                self._connection_model
            )
        if config.request_timeout_seconds <= 0 or not math.isfinite(config.request_timeout_seconds):
            raise ValueError("text generation query request timeout must be positive")
        if config.resolve_credential is None:
            raise ValueError("text generation query credential resolver is required")
        if config.max_response_bytes < 1:
            raise ValueError("text generation query maxResponseBytes must be positive")
        if config.wire_format.decode_stream is not None and config.max_stream_event_bytes < 1:
            raise ValueError(
                "text generation query maxStreamEventBytes must be positive for a streaming wire format"
            )
        self._config = config
        self._wire_format = replace(
            config.wire_format,
            finish_reasons=dict(config.wire_format.finish_reasons),
            error_rules=tuple(config.wire_format.error_rules),
            error_token_pointers=tuple(config.wire_format.error_token_pointers),
            request_id_headers=tuple(config.wire_format.request_id_headers),
            rate_limit_headers=tuple(config.wire_format.rate_limit_headers),
        )
        self._heartbeat_interval_seconds = 5.0

    def definition(self) -> QueryDefinition:
        """Return the connector's generateText operation definition."""
        return self._config.definition

    def request_features(self) -> RequestFeatures:
        """Return optional fields the dialect sends."""
        return self._wire_format.features

    def model_id_rule(self) -> ModelIDRule:
        """Return the dialect's model ID validation rule."""
        return self._wire_format.model_id_rule

    async def invoke(
        self, call: Call, request: TextGenerationRequest
    ) -> QueryAttempt[TextGenerationResponse]:
        """Validate, dispatch once, and classify the complete attempt."""
        return await _TextGenerationAttempt(self, call).run(request)


def _validate_wire_format(wire: WireFormat) -> None:
    if not wire.provider_name:
        raise ValueError("wire format provider name is required")
    if wire.model_id_rule not in ModelIDRule:
        raise ValueError("wire format model ID rule is invalid")
    if wire.rules_for_model is None or wire.encode_request is None:
        raise ValueError("wire format RulesForModel and EncodeRequest are required")
    if wire.decode_response is None and wire.decode_stream is None:
        raise ValueError("wire format requires DecodeResponse, DecodeStream, or both")
    if not _HEADER.fullmatch(wire.credential_header.name):
        raise ValueError("wire format credential header name is invalid")
    if not all(32 <= ord(char) <= 126 for char in wire.credential_header.prefix):
        raise ValueError("wire format credential header prefix must be printable ASCII")
    for token, reason in wire.finish_reasons.items():
        if not _TOKEN.fullmatch(token) or reason not in FinishReason:
            raise ValueError("wire format finish token or reason is invalid")
    for index, rule in enumerate(wire.error_rules):
        if rule.status_code != 0 and not 300 <= rule.status_code <= 599:
            raise ValueError(f"error rule {index} status must be zero or 300 through 599")
        if rule.error_token and not _TOKEN.fullmatch(rule.error_token):
            raise ValueError(f"error rule {index} token is invalid")
        if rule.outcome.disposition not in ("retry", "providerRejected", "blocked", "invalidResponse"):
            raise ValueError(f"error rule {index} outcome is invalid")
    if any(pointer and not pointer.startswith("/") for pointer in wire.error_token_pointers):
        raise ValueError("wire format error-token pointer must be empty or start with /")
    if any(not _HEADER.fullmatch(name) for name in wire.request_id_headers + wire.rate_limit_headers):
        raise ValueError("wire format response header name is invalid")
    if wire.stall_timeout_seconds < 0:
        raise ValueError("wire format stall timeout cannot be negative")


class _TextGenerationAttempt:
    def __init__(self, query: TextGenerationQuery, call: Call) -> None:
        self.query = query
        self.call = call
        self.response = TextGenerationResponse()
        self.credential = ""

    def _failure(self, kind: FailureKind, message: str) -> Failure:
        return Failure(
            kind,
            self.query._wire_format.provider_name,
            self.query._config.definition.operation.operation_id,
            message,
        )

    def _branch(
        self, branch: str, kind: FailureKind, message: str, receipt: Receipt
    ) -> QueryAttempt[TextGenerationResponse]:
        return QueryAttempt.branch_result(branch, self.response, receipt, self._failure(kind, message))

    def _defect(self, kind: FailureKind, message: str) -> QueryAttempt[TextGenerationResponse]:
        return self._branch(
            DEFECT_BRANCH_ID, kind, message, Receipt(provider=self.query._wire_format.provider_name)
        )

    def _retry(
        self, kind: FailureKind, message: str, delay: float = 0
    ) -> QueryAttempt[TextGenerationResponse]:
        return QueryAttempt.retry(self._failure(kind, message), delay)

    async def run(self, request: TextGenerationRequest) -> QueryAttempt[TextGenerationResponse]:
        if self.call.context is None:
            return self._defect(FailureKind.LOCAL_DEFECT, "a Dex Step context is required")
        model = resolve_model(request.model, self.query._connection_model)
        if not model:
            return self._defect(
                FailureKind.VALIDATION, "no model is selected; set the request Model or the connection model"
            )
        try:
            model = self.query._wire_format.model_id_rule.validate_model_id(model)
        except ValueError as error:
            return self._defect(FailureKind.VALIDATION, str(error))
        self.response = replace(self.response, requested_model=model)
        canonical, portable_failure = _check_portable_request(request)
        if portable_failure:
            return self._defect(FailureKind.VALIDATION, portable_failure.message)
        features = self.query._wire_format.features
        unsupported = (
            (bool(request.instructions), features.supports_instructions, "Instructions"),
            (request.structured_output is not None, features.supports_structured_output, "StructuredOutput"),
            (request.max_output_tokens != 0, features.supports_max_output_tokens, "MaxOutputTokens"),
            (request.temperature is not None, features.supports_temperature, "Temperature"),
            (bool(request.reasoning_effort), features.supports_reasoning_effort, "ReasoningEffort"),
        )
        for is_set, supported, name in unsupported:
            if is_set and not supported:
                return self._defect(
                    FailureKind.VALIDATION,
                    f"this connector's wire format does not send {name}; leave it unset",
                )
        rules = self.query._wire_format.rules_for_model(model)
        if request.temperature is not None:
            policy = rules.temperature
            if not math.isfinite(request.temperature) or request.temperature < 0:
                return self._defect(
                    FailureKind.VALIDATION, "temperature must be a finite, non-negative number"
                )
            if not policy.accepted:
                return self._defect(
                    FailureKind.VALIDATION, "the model does not accept a temperature; leave Temperature nil"
                )
            if (
                policy.minimum is not None
                and policy.maximum is not None
                and (
                    not math.isfinite(policy.minimum)
                    or not math.isfinite(policy.maximum)
                    or policy.minimum > policy.maximum
                )
            ):
                return self._defect(FailureKind.VALIDATION, "the wire format's temperature range is invalid")
            if (policy.minimum is not None and request.temperature < policy.minimum) or (
                policy.maximum is not None and request.temperature > policy.maximum
            ):
                if policy.minimum is not None and policy.maximum is not None:
                    message = (
                        f"the model accepts a temperature from {policy.minimum:g} through {policy.maximum:g}"
                    )
                else:
                    message = "the model does not accept this temperature"
                return self._defect(FailureKind.VALIDATION, message)
        if request.reasoning_effort:
            if request.reasoning_effort not in ("none", "minimal", "low", "medium", "high", "xhigh", "max"):
                return self._defect(
                    FailureKind.VALIDATION,
                    "reasoning effort must be none, minimal, low, medium, high, xhigh, or max",
                )
            if request.reasoning_effort not in rules.reasoning_efforts:
                return self._defect(
                    FailureKind.VALIDATION,
                    f'the model does not accept reasoning effort "{request.reasoning_effort}"',
                )
        prepared = replace(request, model=model)
        if request.structured_output is not None:
            output = request.structured_output
            try:
                rules.structured_output.validate()
            except ValueError as error:
                return self._defect(
                    FailureKind.LOCAL_DEFECT,
                    "the wire format's structured output rules are invalid: " + str(error),
                )
            mode = rules.structured_output.mode
            if mode == StructuredOutputMode.NONE:
                return self._defect(FailureKind.VALIDATION, "the model does not support structured output")
            if (
                mode == StructuredOutputMode.JSON_OBJECT_WITH_INSTRUCTION
                and not features.supports_instructions
            ):
                return self._defect(
                    FailureKind.LOCAL_DEFECT, "the JSON object mode requires the Instructions feature"
                )
            assert canonical is not None
            transformed = transform_schema_for_provider(canonical, rules.structured_output)
            instructions = request.instructions
            if mode == StructuredOutputMode.JSON_OBJECT_WITH_INSTRUCTION:
                instructions += ("\n\n" if instructions else "") + json_object_instruction(output, canonical)
            prepared = replace(
                prepared,
                instructions=instructions,
                structured_output=StructuredOutput(output.name, transformed, output.description),
            )
        try:
            secret = await self.query._config.resolve_credential(self.call)
            self.credential = secret.reveal()
        except Exception:
            return self._defect(FailureKind.AUTHENTICATION, "connection credentials are unavailable")
        if not is_header_safe_credential(self.credential):
            return self._defect(FailureKind.AUTHENTICATION, "connection credentials are invalid")
        try:
            encoded = self.query._wire_format.encode_request(EncodeRequestInput(prepared, rules))
        except ValueError as error:
            return self._defect(
                FailureKind.VALIDATION, "the request cannot be encoded for the provider: " + str(error)
            )
        try:
            self._validate_encoded(encoded)
        except ValueError as error:
            return self._defect(
                FailureKind.LOCAL_DEFECT, "the wire format built an invalid request: " + str(error)
            )
        if (
            encoded.is_streaming
            and self.query._wire_format.decode_stream is None
            or not encoded.is_streaming
            and self.query._wire_format.decode_response is None
        ):
            return self._defect(
                FailureKind.LOCAL_DEFECT, "the wire format cannot decode the response form it requested"
            )
        return await self._exchange(encoded, canonical)

    def _validate_encoded(self, encoded: EncodedRequest) -> None:
        if not encoded.path.startswith("/") or any(
            ord(char) <= 32 or ord(char) > 126 or char == "#" for char in encoded.path
        ):
            raise ValueError(
                "encoded request path must start with / and contain printable ASCII "
                "without spaces or fragments"
            )
        reserved = {
            self.query._wire_format.credential_header.name.lower(),
            "content-type",
            "content-length",
            "accept",
            "host",
        }
        for name, value in encoded.headers.items():
            if not _HEADER.fullmatch(name) or name.lower() in reserved:
                raise ValueError("encoded request header name is invalid or reserved")
            if any(ord(char) < 32 or ord(char) > 126 for char in value):
                raise ValueError("encoded request header value must be printable ASCII")

    async def _heartbeat(self) -> None:
        while True:
            await asyncio.sleep(self.query._heartbeat_interval_seconds)
            try:
                context = self.call.context
                if context is not None:
                    await context.heartbeat(None)
            except Exception:
                pass

    async def _exchange(
        self, encoded: EncodedRequest, canonical_schema: dict[str, Any] | None
    ) -> QueryAttempt[TextGenerationResponse]:
        async def dispatch(client: httpx.AsyncClient) -> QueryAttempt[TextGenerationResponse]:
            headers = dict(encoded.headers)
            headers["Content-Type"] = "application/json"
            headers["Accept"] = "text/event-stream" if encoded.is_streaming else "application/json"
            credential_header = self.query._wire_format.credential_header
            headers[credential_header.name] = credential_header.prefix + self.credential
            try:
                http_request = client.build_request(
                    "POST", self.query._base_url + encoded.path, content=encoded.body, headers=headers
                )
            except Exception:
                return self._defect(FailureKind.LOCAL_DEFECT, "the provider request could not be built")
            heartbeat = asyncio.create_task(self._heartbeat())
            try:
                try:
                    response, _ = await asyncio.wait_for(
                        send_traced(client, http_request),
                        self.query._wire_format.stall_timeout_seconds or None,
                    )
                except TimeoutError:
                    return self._retry(
                        FailureKind.AVAILABILITY, "provider sent no bytes within the stall timeout"
                    )
                except (ProviderTransportError, httpx.TimeoutException):
                    return self._retry(
                        FailureKind.TRANSPORT, "provider request failed before a response was received"
                    )
                try:
                    receipt = self._receipt(response.headers)
                    if not 200 <= response.status_code <= 299:
                        try:
                            body = await self._read_body(response, MAX_ERROR_BODY_BYTES)
                        except (httpx.HTTPError, BodyTooLargeError, TimeoutError):
                            body = b""
                        return self._classify_error(response.status_code, response.headers, body, receipt)
                    is_event_stream = (
                        encoded.is_streaming
                        and response.headers.get("content-type", "").split(";", 1)[0].strip().lower()
                        == "text/event-stream"
                    )
                    if not is_event_stream and self.query._wire_format.decode_response is None:
                        return self._branch(
                            INVALID_RESPONSE_BRANCH_ID,
                            FailureKind.PROTOCOL,
                            "provider did not return the event stream it was asked for",
                            receipt,
                        )
                    try:
                        if is_event_stream:
                            assert self.query._wire_format.decode_stream is not None
                            decoded = await self.query._wire_format.decode_stream(
                                self._bounded_chunks(response), self.call.write_text
                            )
                        else:
                            body = await self._read_body(response, self.query._config.max_response_bytes)
                            assert self.query._wire_format.decode_response is not None
                            decoded = self.query._wire_format.decode_response(body)
                    except ProviderReportedError as error:
                        tokens = self._safe_tokens(error.error_tokens)
                        outcome = self._match_error(0, tokens)
                        return self._error_outcome(
                            outcome,
                            "provider reported an error inside a 2xx response"
                            + (" " + " ".join(tokens) if tokens else ""),
                            0,
                            receipt,
                        )
                    except BodyTooLargeError:
                        return self._branch(
                            INVALID_RESPONSE_BRANCH_ID,
                            FailureKind.RESPONSE_TOO_LARGE,
                            "provider response exceeds the configured size limit",
                            receipt,
                        )
                    except _ProviderStalledError:
                        return self._retry(
                            FailureKind.AVAILABILITY, "provider sent no bytes within the stall timeout"
                        )
                    except (httpx.HTTPError, TimeoutError, EOFError):
                        return self._retry(
                            FailureKind.TRANSPORT, "provider response ended before it was complete"
                        )
                    except OSError:
                        return self._retry(FailureKind.AVAILABILITY, "the text stream could not be written")
                    except (ValueError, TypeError, UnicodeError):
                        return self._branch(
                            INVALID_RESPONSE_BRANCH_ID,
                            FailureKind.PROTOCOL,
                            "provider response does not match its documented format",
                            receipt,
                        )
                    return self._complete(decoded, canonical_schema, is_event_stream, receipt)
                finally:
                    await response.aclose()
            finally:
                heartbeat.cancel()
                try:
                    await heartbeat
                except asyncio.CancelledError:
                    pass

        if self.query._config.http_client is not None:
            return await dispatch(self.query._config.http_client)
        async with new_provider_http_client(self.query._config.request_timeout_seconds) as client:
            return await dispatch(client)

    async def _bounded_chunks(self, response: httpx.Response) -> AsyncIterator[bytes]:
        count = 0
        event_bytes = 0
        pending_line = b""
        chunks = response.aiter_bytes().__aiter__()
        while True:
            try:
                chunk = await asyncio.wait_for(
                    chunks.__anext__(), self.query._wire_format.stall_timeout_seconds or None
                )
            except StopAsyncIteration:
                return
            except TimeoutError as error:
                raise _ProviderStalledError("provider stalled") from error
            count += len(chunk)
            if count > self.query._config.max_response_bytes:
                raise BodyTooLargeError(response.status_code, response.headers)
            pending_line += chunk
            while b"\n" in pending_line:
                line, pending_line = pending_line.split(b"\n", 1)
                event_bytes += len(line) + 1
                if event_bytes > self.query._config.max_stream_event_bytes:
                    raise BodyTooLargeError(response.status_code, response.headers)
                if not line.rstrip(b"\r"):
                    event_bytes = 0
            if event_bytes + len(pending_line) > self.query._config.max_stream_event_bytes:
                raise BodyTooLargeError(response.status_code, response.headers)
            yield chunk

    async def _read_body(self, response: httpx.Response, limit: int) -> bytes:
        chunks = []
        count = 0
        iterator = response.aiter_bytes().__aiter__()
        while True:
            try:
                chunk = await asyncio.wait_for(
                    iterator.__anext__(), self.query._wire_format.stall_timeout_seconds or None
                )
            except StopAsyncIteration:
                break
            except TimeoutError as error:
                raise _ProviderStalledError("provider stalled") from error
            count += len(chunk)
            if count > limit:
                raise BodyTooLargeError(response.status_code, response.headers)
            chunks.append(chunk)
        return b"".join(chunks)

    def _receipt(self, headers: httpx.Headers) -> Receipt:
        request_id = ""
        metadata: dict[str, str] = {}
        for name in self.query._wire_format.request_id_headers:
            value = headers.get(name, "").strip()
            if value and self._identifier(value) and self.credential not in value:
                request_id = value
                break
        for name in self.query._wire_format.rate_limit_headers:
            value = headers.get(name, "").strip()
            if (
                value
                and len(value) <= 256
                and all(32 <= ord(char) <= 126 for char in value)
                and self.credential not in value
            ):
                metadata[name.lower()] = value
        return Receipt(
            call_id=self.call.id,
            provider=self.query._wire_format.provider_name,
            observed_at=datetime.now(UTC),
            provider_request_id=request_id,
            metadata=metadata,
        )

    def _safe_tokens(self, tokens: tuple[str, ...]) -> tuple[str, ...]:
        return tuple(token for token in tokens if _TOKEN.fullmatch(token) and self.credential not in token)

    def _match_error(self, status: int, tokens: tuple[str, ...]) -> ErrorOutcome:
        for rule in self.query._wire_format.error_rules:
            if (rule.status_code == 0 or status and rule.status_code == status) and (
                not rule.error_token or rule.error_token in tokens
            ):
                return rule.outcome
        if status == 0 or 300 <= status < 400:
            return invalid_response_outcome()
        if status == 429:
            return retry_outcome(FailureKind.RATE_LIMIT)
        if status == 408 or status >= 500 and status != 501:
            return retry_outcome(FailureKind.AVAILABILITY)
        return {
            401: provider_rejected_outcome(FailureKind.AUTHENTICATION),
            403: provider_rejected_outcome(FailureKind.AUTHORIZATION),
            404: provider_rejected_outcome(FailureKind.NOT_FOUND),
            409: provider_rejected_outcome(FailureKind.CONFLICT),
            402: quota_exhausted_outcome(),
        }.get(status, provider_rejected_outcome(FailureKind.PROVIDER_REJECTION))

    def _classify_error(
        self, status: int, headers: httpx.Headers, body: bytes, receipt: Receipt
    ) -> QueryAttempt[TextGenerationResponse]:
        tokens = self._safe_tokens(read_error_tokens(body, self.query._wire_format.error_token_pointers))
        outcome = self._match_error(status, tokens)
        delay = 0.0
        if outcome.disposition == "retry":
            reader = self.query._wire_format.read_error_retry_delay
            if reader:
                delay = max(0, min(reader(body), 3600))
            if not delay:
                delay = parse_retry_after(headers.get("retry-after", ""), datetime.now(UTC))
        message = f"provider returned HTTP {status}" + (" " + " ".join(tokens) if tokens else "")
        return self._error_outcome(outcome, message, delay, receipt)

    def _error_outcome(
        self, outcome: ErrorOutcome, message: str, delay: float, receipt: Receipt
    ) -> QueryAttempt[TextGenerationResponse]:
        if outcome.disposition == "retry":
            return self._retry(outcome.kind, message, delay)
        if outcome.disposition == "blocked":
            self.response = replace(self.response, finish_reason=FinishReason.CONTENT_POLICY)
        return self._branch(outcome.disposition, outcome.kind, message, receipt)

    @staticmethod
    def _identifier(value: str) -> bool:
        return not value or (len(value.encode()) <= 256 and is_header_safe_credential(value))

    def _complete(
        self,
        decoded: DecodedResponse,
        canonical_schema: dict[str, Any] | None,
        is_streaming: bool,
        receipt: Receipt,
    ) -> QueryAttempt[TextGenerationResponse]:
        if (
            not self._identifier(decoded.served_model)
            or not self._identifier(decoded.response_id)
            or self.credential in decoded.served_model
            or self.credential in decoded.response_id
        ):
            return self._branch(
                INVALID_RESPONSE_BRANCH_ID,
                FailureKind.PROTOCOL,
                "provider returned an invalid model or response identifier",
                receipt,
            )
        receipt = replace(receipt, provider_object_id=decoded.response_id)
        usage = decoded.usage
        if any(
            value < 0
            for value in (
                usage.input_tokens,
                usage.cached_input_tokens,
                usage.output_tokens,
                usage.reasoning_tokens,
                usage.total_tokens,
            )
        ):
            return self._branch(
                INVALID_RESPONSE_BRANCH_ID,
                FailureKind.PROTOCOL,
                "provider reported negative token usage",
                receipt,
            )
        usage = replace(usage, total_tokens=usage.total_tokens or usage.input_tokens + usage.output_tokens)
        self.response = replace(
            self.response, served_model=decoded.served_model, response_id=decoded.response_id, usage=usage
        )
        token = decoded.provider_finish_reason
        if self.credential in token:
            return self._branch(
                INVALID_RESPONSE_BRANCH_ID,
                FailureKind.PROTOCOL,
                "provider returned an invalid finish reason",
                receipt,
            )
        if _TOKEN.fullmatch(token):
            self.response = replace(self.response, provider_finish_reason=token)
        if decoded.is_refusal:
            reason = FinishReason.REFUSAL
        elif not self.response.provider_finish_reason:
            return self._branch(
                INVALID_RESPONSE_BRANCH_ID,
                FailureKind.PROTOCOL,
                "provider returned no recognizable finish reason",
                receipt,
            )
        elif self.response.provider_finish_reason not in self.query._wire_format.finish_reasons:
            return self._branch(
                INVALID_RESPONSE_BRANCH_ID,
                FailureKind.PROTOCOL,
                "provider stopped for an unsupported reason: " + token,
                receipt,
            )
        else:
            reason = self.query._wire_format.finish_reasons[token]
        self.response = replace(self.response, finish_reason=reason)
        text = "".join(part.text for part in decoded.parts if not part.is_reasoning)
        if reason == FinishReason.STOP:
            if not text:
                return self._branch(
                    INVALID_RESPONSE_BRANCH_ID,
                    FailureKind.PROTOCOL,
                    "provider finished without text",
                    receipt,
                )
            if canonical_schema is not None:
                try:
                    text = _extract_structured_answer_from_schema(canonical_schema, text)
                except ValueError as error:
                    return self._branch(
                        INVALID_RESPONSE_BRANCH_ID,
                        FailureKind.PROTOCOL,
                        "structured output does not match its schema " + str(error),
                        receipt,
                    )
            return self._text_branch(GENERATED_BRANCH_ID, text, is_streaming, None, receipt)
        if reason == FinishReason.LENGTH:
            return self._text_branch(
                TRUNCATED_BRANCH_ID,
                text,
                is_streaming,
                self._failure(FailureKind.RESPONSE_TOO_LARGE, "provider stopped at the output token limit"),
                receipt,
            )
        if reason == FinishReason.REFUSAL:
            return self._branch(
                BLOCKED_BRANCH_ID, FailureKind.PROVIDER_REJECTION, "the model refused the request", receipt
            )
        return self._branch(
            BLOCKED_BRANCH_ID,
            FailureKind.PROVIDER_REJECTION,
            "provider stopped the response for a content policy",
            receipt,
        )

    def _text_branch(
        self, branch: str, text: str, is_streaming: bool, failure: Failure | None, receipt: Receipt
    ) -> QueryAttempt[TextGenerationResponse]:
        if not is_streaming and text:
            try:
                self.call.write_text(text)
            except Exception:
                return self._retry(FailureKind.AVAILABILITY, "the text stream could not be written")
        self.response = replace(self.response, text=text)
        return QueryAttempt.branch_result(branch, self.response, receipt, failure)
