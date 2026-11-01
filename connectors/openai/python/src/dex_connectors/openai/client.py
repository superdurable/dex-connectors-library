# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""OpenAI Responses calls with Go-compatible checkpoint fencing and classification."""

import asyncio
import json
import time
from dataclasses import dataclass, field
from datetime import UTC, datetime
from datetime import timedelta as timedelta
from typing import Any
from urllib.parse import quote, urlsplit

import httpx
from dex_connectors.sdk import BranchDefinition as BranchDefinition
from dex_connectors.sdk import (
    Call,
    CallID,
    CredentialProvider,
    Failure,
    FailureKind,
    IdempotencyKey,
    MutationAttempt,
    MutationDefinition,
    QueryAttempt,
    QueryDefinition,
    ReauthorizationRequiredError,
    Receipt,
)
from dex_connectors.sdk import OperationRef as OperationRef
from dex_connectors.sdk import SecretString as SecretString
from dex_connectors.sdk import StepDefaults as StepDefaults
from dex_connectors.sdk.providerhttp import (
    BodyTooLargeError,
    ProviderTransportError,
    is_header_safe_credential,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
)
from dex_connectors.sdk.retry_policy import RetryPolicy as RetryPolicy
from dex_connectors.sdk.retry_policy import (
    default_connector_step_execute_retry_policy as default_connector_step_execute_retry_policy,
)

from ._generated.config import (
    Config as Config,
)
from ._generated.config import (
    Credentials as Credentials,
)
from ._generated.definitions import (
    COMPLETED as COMPLETED,
)
from ._generated.definitions import (
    CONNECTOR_ID as CONNECTOR_ID,
)
from ._generated.definitions import (
    CREATE_RESPONSE_DEFINITION as CREATE_RESPONSE_DEFINITION,
)
from ._generated.definitions import (
    CREATE_RESPONSE_OPERATION_ID as CREATE_RESPONSE_OPERATION_ID,
)
from ._generated.definitions import (
    DEFECT as DEFECT,
)
from ._generated.definitions import (
    FAILED as FAILED,
)
from ._generated.definitions import (
    FOUND as FOUND,
)
from ._generated.definitions import (
    INVALID_RESPONSE as INVALID_RESPONSE,
)
from ._generated.definitions import (
    NOT_FOUND as NOT_FOUND,
)
from ._generated.definitions import (
    PROVIDER_REJECTED as PROVIDER_REJECTED,
)
from ._generated.definitions import (
    RETRIEVE_RESPONSE_DEFINITION as RETRIEVE_RESPONSE_DEFINITION,
)
from ._generated.definitions import (
    RETRIEVE_RESPONSE_OPERATION_ID as RETRIEVE_RESPONSE_OPERATION_ID,
)
from ._generated.definitions import (
    UNCERTAIN as UNCERTAIN,
)
from .models import CreateRequest, Response, RetrieveRequest
from .stream import ResponseStreamReader
from .wire import decode_wire_response, is_safe_response_id

UI_UNIT_MODEL_PICKER = "modelPicker"
UI_MODEL_PICKER_PORT_MODEL = "model"
INVALID_CREATE_INPUT_MESSAGE = "input and a model of printable ASCII without spaces are required"
NOT_SERIALIZABLE_MESSAGE = "request is not JSON serializable"
_DEFAULT_MODEL = "gpt-6-sol"
_DEFAULT_ENDPOINT = "https://api.openai.com/v1"
_DEFAULT_MAX_RESPONSE_BYTES = 8_388_608
_DEFAULT_MAX_SSE_EVENT_BYTES = 1_048_576
_HTTP_TIMEOUT_SECONDS = 120
_CHECKPOINT_HEARTBEAT_INTERVAL_SECONDS = 5.0
_READ_BACK_INITIAL_INTERVAL_SECONDS = 1.0
_READ_BACK_MAX_INTERVAL_SECONDS = 5.0
_READ_BACK_REQUEST_TIMEOUT_SECONDS = 10.0
_READ_BACK_DEADLINE_MARGIN_SECONDS = 5.0
_READ_BACK_BUDGET_WITHOUT_DEADLINE_SECONDS = 120.0
_RATE_LIMIT_HEADER_NAMES = (
    "x-ratelimit-limit-requests",
    "x-ratelimit-remaining-requests",
    "x-ratelimit-reset-requests",
    "x-ratelimit-limit-tokens",
    "x-ratelimit-remaining-tokens",
    "x-ratelimit-reset-tokens",
)


@dataclass(frozen=True, slots=True)
class CreateResponseCheckpoint:
    """Heartbeat value that fences createResponse retries; the Go connector writes the same shape."""

    call_id: str = field(default="", metadata={"dex_json_name": "callId"})
    attempt: int = 0
    response_id: str = field(
        default="", metadata={"dex_json_name": "responseId", "dex_json_omit_default": True}
    )
    can_resend: bool = field(
        default=False, metadata={"dex_json_name": "canResend", "dex_json_omit_default": True}
    )


class Client:
    """One redirect-safe HTTP client and call-time credential source."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None:
            raise ValueError("OpenAI configuration is required")
        if credentials is None:
            raise ValueError("credential provider is required")
        model = config.model.strip() or _DEFAULT_MODEL
        if not is_printable_model_id(model):
            raise ValueError("OpenAI model must be 1 to 256 printable ASCII characters without spaces")
        endpoint = config.endpoint or _DEFAULT_ENDPOINT
        parsed = urlsplit(endpoint)
        if not parsed.scheme or not parsed.hostname:
            raise ValueError("OpenAI endpoint must be absolute")
        if parsed.scheme != "https" and parsed.hostname not in ("127.0.0.1", "localhost"):
            raise ValueError("OpenAI endpoint must use HTTPS")
        if config.max_response_bytes < 0 or config.max_sse_event_bytes < 0:
            raise ValueError("OpenAI response limits must be positive")
        self.model = model
        self.endpoint = endpoint.rstrip("/")
        self.max_response_bytes = config.max_response_bytes or _DEFAULT_MAX_RESPONSE_BYTES
        self.max_sse_event_bytes = config.max_sse_event_bytes or _DEFAULT_MAX_SSE_EVENT_BYTES
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(_HTTP_TIMEOUT_SECONDS)
        self.checkpoint_heartbeat_interval_seconds = _CHECKPOINT_HEARTBEAT_INTERVAL_SECONDS

    def create_response(self) -> "CreateResponseOperation":
        """Bind the fenced create Mutation to this client."""
        return CreateResponseOperation(self)

    def retrieve_response(self) -> "RetrieveResponseOperation":
        """Bind the stored response Query to this client."""
        return RetrieveResponseOperation(self)

    async def aclose(self) -> None:
        """Close the owned provider HTTP client after application shutdown."""
        await self.http_client.aclose()

    async def build_request(
        self, call: Call, method: str, path: str, client_request_id: str, payload: Any
    ) -> httpx.Request | Failure:
        """Resolve credentials and build one request, or return the defect Failure."""
        operation = call.operation.operation_id
        try:
            credentials = await self.credentials.resolve(call)
        except (ValueError, LookupError, OSError, ReauthorizationRequiredError):
            return openai_failure(
                FailureKind.AUTHENTICATION, operation, "connection credentials are unavailable"
            )
        api_key = credentials.api_key.reveal()
        if not api_key or not is_header_safe_credential(api_key):
            return openai_failure(FailureKind.AUTHENTICATION, operation, "connection credentials are invalid")
        body: bytes | None = None
        if payload is not None:
            try:
                body = json.dumps(payload, allow_nan=False).encode()
            except (TypeError, ValueError):
                return openai_failure(FailureKind.VALIDATION, operation, NOT_SERIALIZABLE_MESSAGE)
        headers = {"Authorization": "Bearer " + api_key, "Content-Type": "application/json"}
        if client_request_id:
            headers["X-Client-Request-Id"] = client_request_id
        try:
            return self.http_client.build_request(method, self.endpoint + path, content=body, headers=headers)
        except (httpx.HTTPError, ValueError):
            return openai_failure(FailureKind.LOCAL_DEFECT, operation, "request could not be built")

    async def read_response(self, response: httpx.Response, operation: str) -> dict[str, Any] | Failure:
        """Read and decode one bounded JSON response object."""
        try:
            data = await read_bounded_body(response, self.max_response_bytes)
        except BodyTooLargeError:
            return openai_failure(
                FailureKind.RESPONSE_TOO_LARGE,
                operation,
                "provider response exceeds the configured size limit",
            )
        except ProviderTransportError:
            return openai_failure(FailureKind.TRANSPORT, operation, "provider response could not be read")
        try:
            document = json.loads(data)
            decode_wire_response(document)
        except (ValueError, TypeError, UnicodeDecodeError):
            return openai_failure(FailureKind.PROTOCOL, operation, "provider response is invalid")
        return document  # type: ignore[no-any-return]


class _CreateResponseHeartbeat:
    """Record the fencing checkpoint now and every interval until stopped."""

    def __init__(self, call: Call, interval_seconds: float) -> None:
        self._call = call
        self._interval_seconds = interval_seconds
        self._checkpoint = CreateResponseCheckpoint()
        self._lock = asyncio.Lock()
        self._task: asyncio.Task[None] | None = None

    async def begin(self, checkpoint: CreateResponseCheckpoint) -> bool:
        """Record the first checkpoint, start periodic renewal, and report success."""
        is_recorded = await self.record(checkpoint)
        self._task = asyncio.create_task(self._record_until_stopped())
        return is_recorded

    async def record_response_id(self, response_id: str) -> bool:
        """Checkpoint the observed response ID so a retry reads it back instead of resending."""
        return await self.record(
            CreateResponseCheckpoint(str(self._call.id), _attempt(self._call), response_id)
        )

    async def record_can_resend(self) -> bool:
        """Checkpoint that this attempt provably created nothing, so the next attempt may resend."""
        return await self.record(
            CreateResponseCheckpoint(str(self._call.id), _attempt(self._call), can_resend=True)
        )

    async def record(self, checkpoint: CreateResponseCheckpoint) -> bool:
        """Replace and record the checkpoint; False means Dex did not accept it."""
        async with self._lock:
            self._checkpoint = checkpoint
            try:
                await _context(self._call).heartbeat(checkpoint)
            except Exception:  # noqa: BLE001 - any heartbeat failure leaves the outcome to the caller.
                return False
            return True

    async def stop(self) -> None:
        """Stop periodic renewal and wait for the renewal task to finish."""
        if self._task is None:
            return
        self._task.cancel()
        try:
            await self._task
        except asyncio.CancelledError:
            pass

    async def _record_until_stopped(self) -> None:
        while True:
            await asyncio.sleep(self._interval_seconds)
            await self._renew()

    async def _renew(self) -> None:
        """Re-record the current checkpoint, read under the lock so a newer ID is never overwritten."""
        async with self._lock:
            try:
                await _context(self._call).heartbeat(self._checkpoint)
            except Exception:  # noqa: BLE001 - a missed renewal is repaired by the next tick.
                return


@dataclass(slots=True)
class _StoredResponseRead:
    response: Response | None = None
    retry_after_seconds: float = 0
    retry_failure: Failure | None = None
    unconfirmed_failure: Failure | None = None


class CreateResponseOperation:
    """Create one stored response; a heartbeat checkpoint fences every retry."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("OpenAI client is required")
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return manifest identity, branches, and billed-operation retry defaults."""
        return CREATE_RESPONSE_DEFINITION

    def idempotency_key(self, call_id: CallID, input: CreateRequest) -> IdempotencyKey:
        """Use the stable call ID; OpenAI receives it only as X-Client-Request-Id."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: CreateRequest) -> MutationAttempt[Response]:
        """Send, read back, or report uncertainty according to the previous attempt's checkpoint.

        The Step must use sync Execute durability and record no heartbeats of its own.
        """
        if not isinstance(input, CreateRequest) or not isinstance(input.model, str):
            return _create_defect(FailureKind.VALIDATION, INVALID_CREATE_INPUT_MESSAGE)
        model = input.model.strip() or self.client.model
        if input.input is None or not is_printable_model_id(model):
            return _create_defect(FailureKind.VALIDATION, INVALID_CREATE_INPUT_MESSAGE)
        payload: dict[str, Any] = {"model": model, "input": input.input}
        if input.instructions:
            payload["instructions"] = input.instructions
        if input.structured_output is not None:
            output = input.structured_output
            payload["text"] = {
                "format": {
                    "type": "json_schema",
                    "name": output.name,
                    "description": output.description,
                    "schema": output.schema,
                    "strict": output.strict,
                }
            }
        is_streaming = call.has_progress_stream() or call.has_text_stream()
        if is_streaming:
            payload["stream"] = True
        if call.context is None:
            return _create_defect(FailureKind.LOCAL_DEFECT, "createResponse requires a Dex Step context")
        try:
            previous = call.context.get_last_heartbeat_value(CreateResponseCheckpoint)
        except Exception:  # noqa: BLE001 - any undecodable value is a foreign heartbeat.
            previous = _FOREIGN_CHECKPOINT
        if previous is _FOREIGN_CHECKPOINT or (previous is not None and previous.call_id != str(call.id)):
            return _create_defect(
                FailureKind.LOCAL_DEFECT,
                "the Step's heartbeat value is not this call's checkpoint; createResponse must be the "
                "Step's only heartbeat writer",
            )
        heartbeat = _CreateResponseHeartbeat(call, self.client.checkpoint_heartbeat_interval_seconds)
        attempt = _attempt(call)
        try:
            if previous is not None and previous.response_id:
                if not is_safe_response_id(previous.response_id):
                    return _create_defect(FailureKind.LOCAL_DEFECT, "saved response ID is invalid")
                await heartbeat.begin(CreateResponseCheckpoint(str(call.id), attempt, previous.response_id))
                return await self._read_back_response(call, heartbeat, previous.response_id, None, "")
            if (previous is None and attempt == 1) or (
                previous is not None and previous.can_resend and previous.attempt == attempt - 1
            ):
                return await self._send_create_request(call, heartbeat, payload, is_streaming)
            if previous is None:
                return MutationAttempt.uncertain(
                    Response(),
                    openai_failure(
                        FailureKind.TRANSPORT,
                        CREATE_RESPONSE_OPERATION_ID,
                        "an earlier attempt left no checkpoint and may have created a response; "
                        "createResponse requires sync Execute durability",
                    ),
                    response_receipt(call, "", None, ""),
                )
            return MutationAttempt.uncertain(
                Response(),
                openai_failure(
                    FailureKind.TRANSPORT,
                    CREATE_RESPONSE_OPERATION_ID,
                    "an earlier attempt may have created a response without recording its ID",
                ),
                response_receipt(call, "", None, ""),
            )
        finally:
            await heartbeat.stop()

    async def _send_create_request(
        self, call: Call, heartbeat: _CreateResponseHeartbeat, payload: dict[str, Any], is_streaming: bool
    ) -> MutationAttempt[Response]:
        request = await self.client.build_request(call, "POST", "/responses", str(call.id), payload)
        if isinstance(request, Failure):
            return MutationAttempt.branch_result(DEFECT, Response(), Receipt(), request)
        if not await heartbeat.begin(CreateResponseCheckpoint(str(call.id), _attempt(call))):
            return await _retry_create_response(
                heartbeat,
                openai_failure(
                    FailureKind.AVAILABILITY,
                    CREATE_RESPONSE_OPERATION_ID,
                    "dispatch checkpoint could not be recorded",
                ),
                0,
            )
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError as error:
            if error.provably_not_sent:
                return await _retry_create_response(
                    heartbeat,
                    openai_failure(
                        FailureKind.TRANSPORT, CREATE_RESPONSE_OPERATION_ID, "request was not sent"
                    ),
                    0,
                )
            return MutationAttempt.uncertain(
                Response(),
                openai_failure(
                    FailureKind.TRANSPORT, CREATE_RESPONSE_OPERATION_ID, "provider outcome is unknown"
                ),
                response_receipt(call, "", httpx.Headers(), ""),
            )
        try:
            request_id = response.headers.get("X-Request-Id", "")
            if not 200 <= response.status_code < 300:
                return await _create_status_attempt(
                    call, heartbeat, response.status_code, response.headers, request_id
                )
            if is_streaming:
                return await self._read_stream(call, heartbeat, response, request_id)
            document = await self.client.read_response(response, CREATE_RESPONSE_OPERATION_ID)
            if isinstance(document, Failure):
                return MutationAttempt.uncertain(
                    Response(), document, response_receipt(call, request_id, response.headers, "")
                )
            result = decode_wire_response(document)
            if not result.id:
                return MutationAttempt.uncertain(
                    Response(),
                    openai_failure(
                        FailureKind.PROTOCOL, CREATE_RESPONSE_OPERATION_ID, "provider response has no ID"
                    ),
                    response_receipt(call, request_id, response.headers, ""),
                )
            if not is_safe_response_id(result.id):
                return MutationAttempt.uncertain(
                    Response(),
                    openai_failure(
                        FailureKind.PROTOCOL, CREATE_RESPONSE_OPERATION_ID, "provider response ID is invalid"
                    ),
                    response_receipt(call, request_id, response.headers, ""),
                )
            await heartbeat.record_response_id(result.id)
            return await self.resolve_known_response(call, heartbeat, result, response.headers, request_id)
        finally:
            await response.aclose()

    async def _read_stream(
        self, call: Call, heartbeat: _CreateResponseHeartbeat, response: httpx.Response, request_id: str
    ) -> MutationAttempt[Response]:
        reader = ResponseStreamReader(
            call,
            response,
            self.client.max_response_bytes,
            self.client.max_sse_event_bytes,
            heartbeat.record_response_id,
        )
        outcome = await reader.read()
        if outcome.has_invalid_response_id:
            return MutationAttempt.uncertain(
                Response(),
                openai_failure(
                    FailureKind.PROTOCOL, CREATE_RESPONSE_OPERATION_ID, "provider response ID is invalid"
                ),
                response_receipt(call, request_id, response.headers, reader.response_id),
            )
        if outcome.terminal_response is not None:
            receipt = response_receipt(call, request_id, response.headers, outcome.terminal_response.id)
            if outcome.terminal_event_type == "response.completed":
                return MutationAttempt.branch_result(COMPLETED, outcome.terminal_response, receipt)
            failure = openai_failure(
                FailureKind.PROVIDER_REJECTION,
                CREATE_RESPONSE_OPERATION_ID,
                "provider returned " + outcome.terminal_event_type,
            )
            return MutationAttempt.branch_result(FAILED, outcome.terminal_response, receipt, failure)
        if not reader.response_id:
            message = outcome.error_message
            if reader.has_stream_error:
                message = "provider streamed an error before naming a response, which may exist"
            return MutationAttempt.uncertain(
                Response(),
                openai_failure(FailureKind.PROTOCOL, CREATE_RESPONSE_OPERATION_ID, message),
                response_receipt(call, request_id, response.headers, ""),
            )
        return await self._read_back_response(
            call, heartbeat, reader.response_id, response.headers, request_id
        )

    async def resolve_known_response(
        self,
        call: Call,
        heartbeat: _CreateResponseHeartbeat,
        result: Response,
        headers: httpx.Headers | None,
        request_id: str,
    ) -> MutationAttempt[Response]:
        """Select a branch for a response whose ID is checkpointed."""
        receipt = response_receipt(call, request_id, headers, result.id)
        if result.status == "completed":
            return MutationAttempt.branch_result(COMPLETED, result, receipt)
        if result.status in ("failed", "incomplete"):
            failure = openai_failure(
                FailureKind.PROVIDER_REJECTION,
                CREATE_RESPONSE_OPERATION_ID,
                "provider returned a " + result.status + " response",
            )
            return MutationAttempt.branch_result(FAILED, result, receipt, failure)
        if result.status == "cancelled":
            return await _retry_create_response(
                heartbeat,
                openai_failure(
                    FailureKind.AVAILABILITY,
                    CREATE_RESPONSE_OPERATION_ID,
                    "response " + result.id + " was cancelled before it completed",
                ),
                0,
            )
        if result.status in ("queued", "in_progress"):
            return await self._read_back_response(call, heartbeat, result.id, headers, request_id)
        return MutationAttempt.uncertain(
            result,
            openai_failure(
                FailureKind.PROTOCOL,
                CREATE_RESPONSE_OPERATION_ID,
                "response " + result.id + " has no recognized status",
            ),
            receipt,
        )

    async def _read_back_response(
        self,
        call: Call,
        heartbeat: _CreateResponseHeartbeat,
        response_id: str,
        create_headers: httpx.Headers | None,
        create_request_id: str,
    ) -> MutationAttempt[Response]:
        poll_deadline = _read_back_deadline(call)
        interval = _READ_BACK_INITIAL_INTERVAL_SECONDS
        while True:
            read = await self._read_stored_response(call, response_id, poll_deadline)
            if read.retry_failure is not None:
                return MutationAttempt.retry(read.retry_failure)
            if read.unconfirmed_failure is not None:
                return MutationAttempt.uncertain(
                    Response(id=response_id),
                    read.unconfirmed_failure,
                    response_receipt(call, create_request_id, create_headers, response_id),
                )
            if read.response is not None and read.response.status not in ("queued", "in_progress"):
                return await self.resolve_known_response(
                    call, heartbeat, read.response, create_headers, create_request_id
                )
            wait = max(interval, read.retry_after_seconds)
            if time.monotonic() + wait > poll_deadline:
                return MutationAttempt.retry(
                    openai_failure(
                        FailureKind.AVAILABILITY,
                        CREATE_RESPONSE_OPERATION_ID,
                        "response " + response_id + " is not confirmed yet",
                    ),
                    read.retry_after_seconds,
                )
            await asyncio.sleep(wait)
            interval = min(2 * interval, _READ_BACK_MAX_INTERVAL_SECONDS)

    async def _read_stored_response(
        self, call: Call, response_id: str, poll_deadline: float
    ) -> _StoredResponseRead:
        request = await self.client.build_request(
            call, "GET", "/responses/" + quote(response_id, safe=""), "", None
        )
        if isinstance(request, Failure):
            return _StoredResponseRead(
                retry_failure=openai_failure(
                    request.kind,
                    CREATE_RESPONSE_OPERATION_ID,
                    "response " + response_id + " read-back request could not be built",
                )
            )
        timeout = max(0.0, min(_READ_BACK_REQUEST_TIMEOUT_SECONDS, poll_deadline - time.monotonic()))
        try:
            response, _ = await asyncio.wait_for(send_traced(self.client.http_client, request), timeout)
        except (ProviderTransportError, TimeoutError):
            return _StoredResponseRead()
        try:
            return await asyncio.wait_for(
                self._classify_stored_response(response, response_id),
                max(0.0, min(_READ_BACK_REQUEST_TIMEOUT_SECONDS, poll_deadline - time.monotonic())),
            )
        except TimeoutError:
            return _StoredResponseRead()
        finally:
            await response.aclose()

    async def _classify_stored_response(
        self, response: httpx.Response, response_id: str
    ) -> _StoredResponseRead:
        def unconfirmed(kind: FailureKind, message: str) -> _StoredResponseRead:
            return _StoredResponseRead(
                unconfirmed_failure=openai_failure(
                    kind, CREATE_RESPONSE_OPERATION_ID, "response " + response_id + " " + message
                )
            )

        status = response.status_code
        if 200 <= status < 300:
            document = await self.client.read_response(response, CREATE_RESPONSE_OPERATION_ID)
            if isinstance(document, Failure):
                if document.kind == FailureKind.TRANSPORT:
                    return _StoredResponseRead()
                return unconfirmed(document.kind, "read-back is unreadable: " + document.message)
            result = decode_wire_response(document)
            if result.id != response_id:
                return unconfirmed(FailureKind.PROTOCOL, "read-back returned another response ID")
            return _StoredResponseRead(response=result)
        if status == 404:
            return unconfirmed(
                FailureKind.NOT_FOUND, "was not found on read-back, so its outcome cannot be confirmed"
            )
        if status == 401:
            return unconfirmed(
                FailureKind.AUTHENTICATION,
                "read-back returned HTTP 401; the API key must be valid and allowed to read responses with "
                "GET /v1/responses/{id}",
            )
        if status == 403:
            return unconfirmed(
                FailureKind.AUTHORIZATION,
                "read-back returned HTTP 403; the API key needs read permission for responses "
                "(GET /v1/responses/{id})",
            )
        if status == 429:
            return _StoredResponseRead(retry_after_seconds=_retry_after_seconds(response.headers))
        if status >= 500 or status in (408, 409):
            return _StoredResponseRead()
        return unconfirmed(FailureKind.PROVIDER_REJECTION, f"read-back returned HTTP {status}")


class RetrieveResponseOperation:
    """Read one stored OpenAI response by ID."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("OpenAI client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return RETRIEVE_RESPONSE_DEFINITION

    async def invoke(self, call: Call, input: RetrieveRequest) -> QueryAttempt[Response]:
        """Read once, then select a branch or a safe Query retry."""
        operation = RETRIEVE_RESPONSE_OPERATION_ID
        if (
            not isinstance(input, RetrieveRequest)
            or not isinstance(input.response_id, str)
            or not input.response_id
        ):
            return QueryAttempt.branch_result(
                DEFECT,
                Response(),
                Receipt(),
                openai_failure(FailureKind.VALIDATION, operation, "response ID is required"),
            )
        request = await self.client.build_request(
            call, "GET", "/responses/" + quote(input.response_id, safe=""), "", None
        )
        if isinstance(request, Failure):
            return QueryAttempt.branch_result(DEFECT, Response(), Receipt(), request)
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return QueryAttempt.retry(
                openai_failure(FailureKind.AVAILABILITY, operation, "provider is unavailable")
            )
        try:
            request_id = response.headers.get("X-Request-Id", "")
            if not 200 <= response.status_code < 300:
                failure, retry_after, is_retryable = classify_openai_status(
                    operation, response.status_code, response.headers
                )
                if is_retryable:
                    return QueryAttempt.retry(failure, retry_after)
                branch = NOT_FOUND if response.status_code == 404 else PROVIDER_REJECTED
                return QueryAttempt.branch_result(
                    branch,
                    Response(),
                    response_receipt(call, request_id, response.headers, input.response_id),
                    failure,
                )
            document = await self.client.read_response(response, operation)
            if isinstance(document, Failure):
                if document.kind in (FailureKind.RESPONSE_TOO_LARGE, FailureKind.PROTOCOL):
                    return QueryAttempt.branch_result(
                        INVALID_RESPONSE,
                        Response(),
                        response_receipt(call, request_id, response.headers, input.response_id),
                        document,
                    )
                return QueryAttempt.retry(document)
            result = decode_wire_response(document)
            return QueryAttempt.branch_result(
                FOUND, result, response_receipt(call, request_id, response.headers, result.id)
            )
        finally:
            await response.aclose()


async def _retry_create_response(
    heartbeat: _CreateResponseHeartbeat, failure: Failure, retry_after_seconds: float
) -> MutationAttempt[Response]:
    await heartbeat.record_can_resend()
    return MutationAttempt.retry(failure, retry_after_seconds)


async def _create_status_attempt(
    call: Call, heartbeat: _CreateResponseHeartbeat, status: int, headers: httpx.Headers, request_id: str
) -> MutationAttempt[Response]:
    failure, retry_after, is_retryable = classify_openai_status(CREATE_RESPONSE_OPERATION_ID, status, headers)
    if is_retryable:
        return await _retry_create_response(heartbeat, failure, retry_after)
    receipt = response_receipt(call, request_id, headers, "")
    if status >= 500:
        return MutationAttempt.uncertain(Response(), failure, receipt)
    if status == 408:
        return MutationAttempt.uncertain(
            Response(),
            openai_failure(
                FailureKind.TRANSPORT,
                CREATE_RESPONSE_OPERATION_ID,
                "provider returned HTTP 408 without a response ID, so a response may exist",
            ),
            receipt,
        )
    if status == 409:
        return MutationAttempt.uncertain(
            Response(),
            openai_failure(
                FailureKind.CONFLICT,
                CREATE_RESPONSE_OPERATION_ID,
                "provider returned HTTP 409 without a response ID, so a response may exist",
            ),
            receipt,
        )
    return MutationAttempt.branch_result(PROVIDER_REJECTED, Response(), receipt, failure)


def classify_openai_status(
    operation: str, status: int, headers: httpx.Headers
) -> tuple[Failure, float, bool]:
    """Map a non-2xx status to its Failure, Retry-After delay, and retryability."""
    kind = FailureKind.PROVIDER_REJECTION
    is_retryable = False
    if status == 401:
        kind = FailureKind.AUTHENTICATION
    elif status == 403:
        kind = FailureKind.AUTHORIZATION
    elif status == 404:
        kind = FailureKind.NOT_FOUND
    elif status == 408:
        kind = FailureKind.AVAILABILITY
        is_retryable = operation == RETRIEVE_RESPONSE_OPERATION_ID
    elif status == 409:
        kind = FailureKind.CONFLICT
    elif status == 429:
        kind = FailureKind.RATE_LIMIT
        is_retryable = True
    elif status == 503:
        kind = FailureKind.AVAILABILITY
        is_retryable = True
    elif status >= 500:
        kind = FailureKind.AVAILABILITY
        is_retryable = operation == RETRIEVE_RESPONSE_OPERATION_ID
    retry_after = _retry_after_seconds(headers) if kind == FailureKind.RATE_LIMIT else 0
    return openai_failure(kind, operation, f"provider returned HTTP {status}"), retry_after, is_retryable


def openai_failure(kind: FailureKind, operation: str, message: str) -> Failure:
    """Build a provider-safe OpenAI Failure."""
    return Failure(kind, CONNECTOR_ID, operation, message)


def response_receipt(call: Call, request_id: str, headers: httpx.Headers | None, response_id: str) -> Receipt:
    """Build a receipt with the response ID, request ID, and rate-limit headers."""
    metadata: dict[str, str] = {}
    if headers is not None:
        for name in _RATE_LIMIT_HEADER_NAMES:
            value = headers.get(name, "")
            if value:
                metadata[name] = value
    return Receipt(
        call_id=call.id,
        provider=CONNECTOR_ID,
        observed_at=datetime.now(UTC),
        idempotency_key=call.idempotency_key,
        provider_object_id=response_id,
        provider_request_id=request_id,
        metadata=metadata,
    )


def is_printable_model_id(model: str) -> bool:
    """Accept 1 to 256 printable ASCII characters without spaces."""
    return 1 <= len(model) <= 256 and all(0x21 <= ord(character) <= 0x7E for character in model)


_FOREIGN_CHECKPOINT = CreateResponseCheckpoint(call_id="\x00foreign")


def _create_defect(kind: FailureKind, message: str) -> MutationAttempt[Response]:
    return MutationAttempt.branch_result(
        DEFECT, Response(), Receipt(), openai_failure(kind, CREATE_RESPONSE_OPERATION_ID, message)
    )


def _context(call: Call) -> Any:
    if call.context is None:
        raise ValueError("createResponse requires a Dex Step context")
    return call.context


def _attempt(call: Call) -> int:
    return int(_context(call).attempt)


def _read_back_deadline(call: Call) -> float:
    """Return a monotonic poll deadline five seconds before the Execute deadline."""
    deadline = _context(call).deadline
    if deadline is None:
        remaining = _READ_BACK_BUDGET_WITHOUT_DEADLINE_SECONDS
    else:
        remaining = (deadline - datetime.now(UTC)).total_seconds()
    return time.monotonic() + remaining - _READ_BACK_DEADLINE_MARGIN_SECONDS


def _retry_after_seconds(headers: httpx.Headers) -> float:
    return parse_retry_after(headers.get("Retry-After", ""), datetime.now(UTC))


Config.__doc__ = "OpenAI settings; blank or zero values use manifest defaults."
Credentials.__doc__ = "OpenAI project API key, resolved anew for every provider attempt."
