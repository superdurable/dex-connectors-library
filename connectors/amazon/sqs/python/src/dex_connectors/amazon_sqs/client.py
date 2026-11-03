# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Amazon SQS long-polling receive, delete, and visibility changes over the JSON protocol.

Requests are SigV4-signed like the Go connector, and errors are classified from the
X-Amzn-Query-Error header and the JSON __type, never from provider message text, except
to recognize an invalid receipt handle.
"""

import base64
import binascii
import hashlib
import json
import re
import unicodedata
from collections.abc import Callable
from dataclasses import dataclass, field, replace
from datetime import UTC, datetime, timedelta
from enum import Enum
from typing import Any, TypeVar
from urllib.parse import urlsplit

import httpx
from dex_connectors.sdk import (
    Call,
    CallID,
    CredentialProvider,
    Failure,
    FailureKind,
    IdempotencyKey,
    MutationAttempt,
    MutationDefinition,
    Receipt,
)
from dex_connectors.sdk.providerhttp import (
    MAX_ERROR_BODY_BYTES,
    BodyTooLargeError,
    ProviderTransportError,
    is_header_safe_credential,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import (
    CHANGE_MESSAGE_VISIBILITY_DEFINITION,
    CHANGE_MESSAGE_VISIBILITY_OPERATION_ID,
    CHANGED,
    CONNECTOR_ID,
    DEFECT,
    DELETE_MESSAGE_DEFINITION,
    DELETE_MESSAGE_OPERATION_ID,
    DELETED,
    EMPTY,
    INVALID_RESPONSE,
    PROVIDER_REJECTED,
    RECEIPT_HANDLE_INVALID,
    RECEIVE_MESSAGES_DEFINITION,
    RECEIVE_MESSAGES_OPERATION_ID,
    RECEIVED,
)
from .models import (
    ChangedMessageVisibility,
    ChangeMessageVisibilityInput,
    DeletedMessage,
    DeleteMessageInput,
    Message,
    MessageAttributeValue,
    ReceivedMessages,
    ReceiveMessagesInput,
)
from .sigv4 import sign_headers

MAXIMUM_MESSAGES_PER_RECEIVE = 10
MAXIMUM_WAIT_TIME_SECONDS = 20
MAXIMUM_VISIBILITY_TIMEOUT_SECONDS = 43200
_SIGNING_SERVICE = "sqs"
_OPERATION_DEADLINE_SECONDS = 25.0
_CONTENT_TYPE = "application/x-amz-json-1.0"
_TARGET_PREFIX = "AmazonSQS."
_REQUEST_ID_HEADER = "X-Amzn-Requestid"
_QUERY_ERROR_HEADER = "X-Amzn-Query-Error"
_FIFO_SUFFIX = ".fifo"
_MAXIMUM_QUEUE_URL_BYTES = 2048
_MAXIMUM_RECEIPT_HANDLE_BYTES = 4096
_MAXIMUM_MESSAGE_ID_BYTES = 128
_MAXIMUM_RECEIVE_REQUEST_ATTEMPT_ID_BYTES = 128
_DEFAULT_REGION = "us-east-1"
_DEFAULT_MAX_RESPONSE_BYTES = 4_194_304
_ERROR_CODE_PATTERN = re.compile(r"^[A-Za-z0-9.]{1,96}$")
_REGION_PATTERN = re.compile(r"^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?$")
_REQUEST_ID_PATTERN = re.compile(r"^[A-Za-z0-9+/=._-]{1,128}$")
_ENDPOINT_PATH_PATTERN = re.compile(r"^(/[A-Za-z0-9._~-]+)*$")
_RECEIPT_HANDLE_MESSAGE_PATTERN = re.compile(r"receipt ?handle", re.IGNORECASE)
_QUEUE_NAME_PATTERN = re.compile(r"^[A-Za-z0-9_-]{1,80}$")
_QUEUE_OWNER_PATTERN = re.compile(r"^[A-Za-z0-9_-]{1,64}$")
_AMAZON_QUEUE_HOST_PATTERN = re.compile(r"^sqs\.([a-z0-9-]+)\.amazonaws\.(?:com|com\.cn|eu)$")
_MESSAGE_ATTRIBUTE_NAME_PATTERN = re.compile(r"^[A-Za-z0-9_.-]{1,256}$")
_INTEGER_PATTERN = re.compile(r"^[+-]?[0-9]+$")
_PARTITION_DNS_SUFFIXES = (
    (re.compile(r"^(us|eu|ap|sa|ca|me|af|il|mx)-\w+-\d+$"), "amazonaws.com"),
    (re.compile(r"^us-gov-\w+-\d+$"), "amazonaws.com"),
    (re.compile(r"^cn-\w+-\d+$"), "amazonaws.com.cn"),
    (re.compile(r"^eusc-de-\w+-\d+$"), "amazonaws.eu"),
)
_RECEIVED_SYSTEM_ATTRIBUTE_NAMES = (
    "ApproximateReceiveCount",
    "ApproximateFirstReceiveTimestamp",
    "SentTimestamp",
    "MessageGroupId",
    "MessageDeduplicationId",
    "SequenceNumber",
)
_THROTTLING_CODES = (
    "RequestThrottled",
    "ThrottlingException",
    "Throttling",
    "KmsThrottled",
    "AWS.SimpleQueueService.RequestThrottled",
)
_RECEIPT_HANDLE_CODES = (
    "ReceiptHandleIsInvalid",
    "InvalidIdFormat",
    "MessageNotInflight",
    "AWS.SimpleQueueService.MessageNotInflight",
)
_CREDENTIAL_CODES = (
    "InvalidClientTokenId",
    "UnrecognizedClientException",
    "SignatureDoesNotMatch",
    "IncompleteSignature",
    "ExpiredToken",
    "InvalidSecurity",
    "MissingAuthenticationToken",
    "InvalidAccessKeyId",
    "AWS.SimpleQueueService.InvalidSecurity",
)
_AUTHORIZATION_CODES = (
    "AccessDenied",
    "AccessDeniedException",
    "KmsAccessDenied",
    "KmsDisabled",
    "KmsInvalidState",
    "KmsNotFound",
    "KmsOptInRequired",
    "KmsInvalidKeyUsage",
)
Output = TypeVar("Output")
ZERO_TIME = datetime(1, 1, 1, tzinfo=UTC)
_MAXIMUM_ACCESS_KEY_ID_BYTES = 128
_MAXIMUM_SECRET_ACCESS_KEY_BYTES = 256
_MAXIMUM_SESSION_TOKEN_BYTES = 4096


def validate_credentials(credentials: Credentials) -> None:
    """Reject credentials that cannot be sent in a SigV4 Authorization header, like Go."""
    access_key_id = credentials.access_key_id.reveal()
    if (
        not is_header_safe_credential(access_key_id)
        or len(access_key_id) > _MAXIMUM_ACCESS_KEY_ID_BYTES
        or any(character in access_key_id for character in "/,=")
    ):
        raise ValueError(
            "SQS access key ID is required and must be printable ASCII without spaces, slashes, commas, "
            "or equals signs"
        )
    secret_access_key = credentials.secret_access_key.reveal()
    if (
        not secret_access_key
        or len(secret_access_key.encode("utf-8", "surrogatepass")) > _MAXIMUM_SECRET_ACCESS_KEY_BYTES
        or any(unicodedata.category(character) == "Cc" for character in secret_access_key)
    ):
        raise ValueError("SQS secret access key is required and cannot contain control characters")
    session_token = credentials.session_token.reveal()
    if session_token and (
        not is_header_safe_credential(session_token) or len(session_token) > _MAXIMUM_SESSION_TOKEN_BYTES
    ):
        raise ValueError("SQS session token must be printable ASCII without spaces")




@dataclass(frozen=True, slots=True)
class _Queue:
    url: str
    name: str
    is_fifo: bool
    amazon_region: str


class _Outcome(Enum):
    RETRY = "retry"
    RECEIPT_HANDLE_INVALID = "receiptHandleInvalid"
    REJECTED = "rejected"
    DEFECT = "defect"


@dataclass(frozen=True, slots=True)
class _Classified:
    outcome: _Outcome
    failure: Failure
    retry_after_seconds: float = 0
    request_id: str = ""


@dataclass(frozen=True, slots=True)
class _ErrorResponse:
    status: int
    code: str = ""
    request_id: str = ""
    retry_after_seconds: float = 0
    is_about_receipt_handle: bool = False


@dataclass(frozen=True, slots=True)
class _Session:
    call: Call
    credentials: Credentials = field(repr=False)


class _RequestNotBuiltError(ValueError):
    """The request could not be encoded or signed."""


class Client:
    """One signing Region and endpoint, redirect-safe HTTP client, and credential source."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        now: Callable[[], datetime] | None = None,
    ) -> None:
        if config is None:
            raise ValueError("SQS configuration is required")
        if credentials is None:
            raise ValueError("credential provider is required")
        region = config.region or _DEFAULT_REGION
        if not _REGION_PATTERN.fullmatch(region):
            raise ValueError("SQS region must be a Region code such as eu-west-1")
        self.endpoint, self.is_amazon_endpoint = _resolve_endpoint(config.endpoint, region)
        if config.default_queue_url:
            try:
                _parse_queue_url(config.default_queue_url)
            except ValueError as error:
                raise ValueError(f"SQS defaultQueueUrl: {error}") from None
        max_messages = config.max_messages_per_receive or MAXIMUM_MESSAGES_PER_RECEIVE
        wait_seconds = config.receive_wait_seconds or MAXIMUM_WAIT_TIME_SECONDS
        max_response_bytes = config.max_response_bytes or _DEFAULT_MAX_RESPONSE_BYTES
        if not 1 <= max_messages <= MAXIMUM_MESSAGES_PER_RECEIVE:
            raise ValueError(f"SQS maxMessagesPerReceive must be from 1 to {MAXIMUM_MESSAGES_PER_RECEIVE}")
        if not 1 <= wait_seconds <= MAXIMUM_WAIT_TIME_SECONDS:
            raise ValueError(f"SQS receiveWaitSeconds must be from 1 to {MAXIMUM_WAIT_TIME_SECONDS}")
        if max_response_bytes < 1:
            raise ValueError("SQS maxResponseBytes must be positive")
        self.region = region
        self.default_queue_url = config.default_queue_url
        self.max_messages_per_receive = max_messages
        self.receive_wait_seconds = wait_seconds
        self.max_response_bytes = max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(_OPERATION_DEADLINE_SECONDS)
        self.now = now or (lambda: datetime.now(UTC))

    def receive_messages(self) -> "ReceiveMessagesOperation":
        """Bind the receiveMessages Mutation to this client."""
        return ReceiveMessagesOperation(self)

    def delete_message(self) -> "DeleteMessageOperation":
        """Bind the deleteMessage Mutation to this client."""
        return DeleteMessageOperation(self)

    def change_message_visibility(self) -> "ChangeMessageVisibilityOperation":
        """Bind the changeMessageVisibility Mutation to this client."""
        return ChangeMessageVisibilityOperation(self)

    async def aclose(self) -> None:
        """Close the owned provider HTTP client after application shutdown."""
        await self.http_client.aclose()

    async def start_session(self, call: Call, operation: str) -> _Session | Failure:
        """Resolve and validate the access key once per operation."""
        try:
            credentials = await self.credentials.resolve(call)
            validate_credentials(credentials)
        except Exception:
            return _failure(
                operation,
                FailureKind.AUTHENTICATION,
                "SQS connection credentials are unavailable; save an access key ID and secret access key for "
                "the connection",
            )
        return _Session(call, credentials)

    def resolve_queue(self, queue_url: str) -> _Queue:
        """Use the input or default queue and require an Amazon queue to match the signing Region."""
        queue_url = queue_url or self.default_queue_url
        if not queue_url:
            raise ValueError("queueUrl is required because the connection has no defaultQueueUrl")
        queue = _parse_queue_url(queue_url)
        if self.is_amazon_endpoint and queue.amazon_region and queue.amazon_region != self.region:
            raise ValueError(
                f"queueUrl is in Region {queue.amazon_region} but the connection signs for {self.region}; "
                f"set region to {queue.amazon_region}"
            )
        return queue

    async def exchange(self, session: _Session, action: str, document: dict[str, Any]) -> httpx.Response:
        """Sign and send one JSON-protocol request; the caller closes the streaming response."""
        try:
            body = json.dumps(document, separators=(",", ":"), ensure_ascii=False).encode()
            headers = sign_headers(
                method="POST",
                url=self.endpoint,
                headers={"Content-Type": _CONTENT_TYPE, "X-Amz-Target": _TARGET_PREFIX + action},
                body=body,
                access_key_id=session.credentials.access_key_id.reveal(),
                secret_access_key=session.credentials.secret_access_key.reveal(),
                session_token=session.credentials.session_token.reveal(),
                region=self.region,
                service=_SIGNING_SERVICE,
                signing_time=self.now(),
            )
            request = self.http_client.build_request("POST", self.endpoint, content=body, headers=headers)
        except (ValueError, TypeError, httpx.HTTPError):
            raise _RequestNotBuiltError("SQS request could not be built") from None
        response, _ = await send_traced(self.http_client, request)
        return response

    async def read_error_response(self, response: httpx.Response, credentials: Credentials) -> _ErrorResponse:
        """Read the error code from the query-compatibility header and the JSON __type."""
        code = ""
        query_code = str(response.headers.get(_QUERY_ERROR_HEADER, "")).partition(";")[0]
        if _ERROR_CODE_PATTERN.fullmatch(query_code) and not _contains_credential(query_code, credentials):
            code = query_code
        error = _ErrorResponse(
            response.status_code,
            code,
            _safe_request_id(response.headers),
            parse_retry_after(str(response.headers.get("Retry-After", "")), self.now()),
        )
        try:
            contents = await read_bounded_body(response, MAX_ERROR_BODY_BYTES)
        except (BodyTooLargeError, ProviderTransportError):
            return error
        try:
            document = json.loads(contents)
            if not isinstance(document, dict):
                return error
            error_type = _optional_string(document, "__type")
            message = _optional_string(document, "message")
        except (ValueError, UnicodeDecodeError):
            return error
        type_code = error_type.partition("#")[2] if "#" in error_type else error_type
        type_code = type_code.partition(":")[0]
        if _ERROR_CODE_PATTERN.fullmatch(type_code) and not _contains_credential(type_code, credentials):
            error = replace(error, code=type_code)
        return replace(error, is_about_receipt_handle=bool(_RECEIPT_HANDLE_MESSAGE_PATTERN.search(message)))

    def receipt(self, call: Call, request_id: str, object_id: str) -> Receipt:
        """Correlate the call with the SQS request ID and the queue or message."""
        return Receipt(
            call_id=call.id,
            provider=CONNECTOR_ID,
            observed_at=self.now().astimezone(UTC),
            idempotency_key=call.idempotency_key,
            provider_object_id=object_id,
            provider_request_id=request_id,
        )

    async def send_acknowledgement(
        self, session: _Session, operation: str, subject: str, action: str, document: dict[str, Any]
    ) -> str | _Classified:
        """Send a delete or visibility change; return the request ID or a classified failure."""
        try:
            response = await self.exchange(session, action, document)
        except _RequestNotBuiltError as error:
            return _Classified(_Outcome.DEFECT, _failure(operation, FailureKind.LOCAL_DEFECT, str(error)))
        except ProviderTransportError:
            return _Classified(_Outcome.RETRY, _transport_failure(operation, subject))
        if not 200 <= response.status_code < 300:
            classified = _classify_error_response(
                operation, subject, await self.read_error_response(response, session.credentials)
            )
            return classified
        await response.aclose()
        return _safe_request_id(response.headers)


class ReceiveMessagesOperation:
    """Long-poll one queue for up to ten messages."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("SQS client is required")
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return RECEIVE_MESSAGES_DEFINITION

    def idempotency_key(self, call_id: CallID, input: ReceiveMessagesInput) -> IdempotencyKey:
        """Use the Call ID, which FIFO queues receive as ReceiveRequestAttemptId."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: ReceiveMessagesInput) -> MutationAttempt[ReceivedMessages]:
        """Receive once; a FIFO retry repeats the same attempt ID so SQS returns the same batch."""
        client = self.client
        operation = RECEIVE_MESSAGES_OPERATION_ID
        try:
            queue, document = self._build_request(input)
        except ValueError as error:
            failure = _failure(operation, FailureKind.VALIDATION, str(error))
            return MutationAttempt.branch_result(DEFECT, ReceivedMessages(), Receipt(), failure)
        if queue.is_fifo:
            attempt_id = str(call.idempotency_key)
            if (
                not attempt_id
                or len(attempt_id.encode("utf-8", "surrogatepass"))
                > _MAXIMUM_RECEIVE_REQUEST_ATTEMPT_ID_BYTES
                or not _is_printable_token(attempt_id)
            ):
                message = "idempotency key is missing or not a valid ReceiveRequestAttemptId"
                failure = _failure(operation, FailureKind.LOCAL_DEFECT, message)
                return MutationAttempt.branch_result(DEFECT, ReceivedMessages(), Receipt(), failure)
            document["ReceiveRequestAttemptId"] = attempt_id
        session = await client.start_session(call, operation)
        if isinstance(session, Failure):
            return MutationAttempt.branch_result(DEFECT, ReceivedMessages(), Receipt(), session)
        batch = ReceivedMessages(queue.url, queue.is_fifo)
        try:
            response = await client.exchange(session, "ReceiveMessage", document)
        except _RequestNotBuiltError as error:
            failure = _failure(operation, FailureKind.LOCAL_DEFECT, str(error))
            return MutationAttempt.branch_result(DEFECT, batch, Receipt(), failure)
        except ProviderTransportError:
            return MutationAttempt.retry(_transport_failure(operation, "receive"))
        if not 200 <= response.status_code < 300:
            classified = _classify_error_response(
                operation, "receive", await client.read_error_response(response, session.credentials)
            )
            receipt = client.receipt(call, classified.request_id, queue.url)
            return _mutation_for_failure(classified, receipt, "", batch)
        receipt = client.receipt(call, _safe_request_id(response.headers), queue.url)
        try:
            contents = await read_bounded_body(response, client.max_response_bytes)
        except BodyTooLargeError:
            message = "SQS receive batch exceeds maxResponseBytes; lower maxMessages"
            failure = _failure(operation, FailureKind.RESPONSE_TOO_LARGE, message)
            return MutationAttempt.branch_result(INVALID_RESPONSE, batch, receipt, failure)
        except ProviderTransportError:
            return MutationAttempt.retry(_transport_failure(operation, "receive"))
        if _contains_credential(contents.decode("utf-8", "replace"), session.credentials):
            failure = _failure(
                operation, FailureKind.PROTOCOL, "SQS receive batch contains the connection's credentials"
            )
            return MutationAttempt.branch_result(INVALID_RESPONSE, batch, receipt, failure)
        try:
            messages = _decode_receive_response(contents, document["MaxNumberOfMessages"])
        except ValueError as error:
            message = "SQS returned an invalid receive batch: " + str(error)
            failure = _failure(operation, FailureKind.PROTOCOL, message)
            return MutationAttempt.branch_result(INVALID_RESPONSE, batch, receipt, failure)
        batch = replace(batch, messages=messages)
        return MutationAttempt.branch_result(RECEIVED if messages else EMPTY, batch, receipt, None)

    def _build_request(self, input: ReceiveMessagesInput) -> tuple[_Queue, dict[str, Any]]:
        client = self.client
        queue = client.resolve_queue(input.queue_url)
        max_messages = input.max_messages or client.max_messages_per_receive
        if not 1 <= max_messages <= MAXIMUM_MESSAGES_PER_RECEIVE:
            raise ValueError(
                f"maxMessages must be from 1 to {MAXIMUM_MESSAGES_PER_RECEIVE}, or zero for the connection's "
                "maxMessagesPerReceive"
            )
        wait_seconds = (
            client.receive_wait_seconds if input.wait_time_seconds is None else input.wait_time_seconds
        )
        if not 0 <= wait_seconds <= MAXIMUM_WAIT_TIME_SECONDS:
            raise ValueError(f"waitTimeSeconds must be from 0 to {MAXIMUM_WAIT_TIME_SECONDS}")
        timeout = input.visibility_timeout_seconds
        if timeout is not None and not 0 <= timeout <= MAXIMUM_VISIBILITY_TIMEOUT_SECONDS:
            raise ValueError(
                f"visibilityTimeoutSeconds must be from 0 to {MAXIMUM_VISIBILITY_TIMEOUT_SECONDS}"
            )
        document: dict[str, Any] = {
            "QueueUrl": queue.url,
            "MaxNumberOfMessages": max_messages,
            "WaitTimeSeconds": wait_seconds,
        }
        if timeout is not None:
            document["VisibilityTimeout"] = timeout
        document["AttributeNames"] = list(_RECEIVED_SYSTEM_ATTRIBUTE_NAMES)
        document["MessageSystemAttributeNames"] = list(_RECEIVED_SYSTEM_ATTRIBUTE_NAMES)
        document["MessageAttributeNames"] = ["All"]
        return queue, document


class DeleteMessageOperation:
    """Acknowledge one received message by deleting it."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("SQS client is required")
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return DELETE_MESSAGE_DEFINITION

    def idempotency_key(self, call_id: CallID, input: DeleteMessageInput) -> IdempotencyKey:
        """Use the Call ID; deleting the same receipt handle again is safe."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: DeleteMessageInput) -> MutationAttempt[DeletedMessage]:
        """Delete by receipt handle."""
        client = self.client
        operation = DELETE_MESSAGE_OPERATION_ID
        try:
            queue = _validate_acknowledgement(client, input.queue_url, input.receipt_handle, input.message_id)
        except ValueError as error:
            failure = _failure(operation, FailureKind.VALIDATION, str(error))
            return MutationAttempt.branch_result(DEFECT, DeletedMessage(), Receipt(), failure)
        session = await client.start_session(call, operation)
        if isinstance(session, Failure):
            return MutationAttempt.branch_result(DEFECT, DeletedMessage(), Receipt(), session)
        deleted = DeletedMessage(queue.url, input.message_id)
        result = await client.send_acknowledgement(
            session,
            operation,
            "message deletion",
            "DeleteMessage",
            {"QueueUrl": queue.url, "ReceiptHandle": input.receipt_handle},
        )
        request_id = result.request_id if isinstance(result, _Classified) else result
        receipt = client.receipt(call, request_id, _message_object_id(queue, input.message_id))
        if isinstance(result, _Classified):
            return _mutation_for_failure(result, receipt, RECEIPT_HANDLE_INVALID, deleted)
        return MutationAttempt.branch_result(DELETED, deleted, receipt, None)


class ChangeMessageVisibilityOperation:
    """Release or extend one received message's visibility timeout."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("SQS client is required")
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return CHANGE_MESSAGE_VISIBILITY_DEFINITION

    def idempotency_key(self, call_id: CallID, input: ChangeMessageVisibilityInput) -> IdempotencyKey:
        """Use the Call ID; the bounded retry policy limits lease extensions after a lost response."""
        return IdempotencyKey(str(call_id))

    async def invoke(
        self, call: Call, input: ChangeMessageVisibilityInput
    ) -> MutationAttempt[ChangedMessageVisibility]:
        """Set the visibility timeout; zero releases the message for redelivery."""
        client = self.client
        operation = CHANGE_MESSAGE_VISIBILITY_OPERATION_ID
        try:
            queue = _validate_acknowledgement(client, input.queue_url, input.receipt_handle, input.message_id)
            if not 0 <= input.visibility_timeout_seconds <= MAXIMUM_VISIBILITY_TIMEOUT_SECONDS:
                raise ValueError(
                    f"visibilityTimeoutSeconds must be from 0 to {MAXIMUM_VISIBILITY_TIMEOUT_SECONDS}"
                )
        except ValueError as error:
            failure = _failure(operation, FailureKind.VALIDATION, str(error))
            return MutationAttempt.branch_result(DEFECT, ChangedMessageVisibility(), Receipt(), failure)
        session = await client.start_session(call, operation)
        if isinstance(session, Failure):
            return MutationAttempt.branch_result(DEFECT, ChangedMessageVisibility(), Receipt(), session)
        changed = ChangedMessageVisibility(queue.url, input.message_id, input.visibility_timeout_seconds)
        result = await client.send_acknowledgement(
            session,
            operation,
            "visibility change",
            "ChangeMessageVisibility",
            {
                "QueueUrl": queue.url,
                "ReceiptHandle": input.receipt_handle,
                "VisibilityTimeout": input.visibility_timeout_seconds,
            },
        )
        request_id = result.request_id if isinstance(result, _Classified) else result
        receipt = client.receipt(call, request_id, _message_object_id(queue, input.message_id))
        if isinstance(result, _Classified):
            return _mutation_for_failure(result, receipt, RECEIPT_HANDLE_INVALID, changed)
        observed_at = receipt.observed_at
        assert isinstance(observed_at, datetime)
        changed = replace(
            changed, visible_after=observed_at + timedelta(seconds=input.visibility_timeout_seconds)
        )
        return MutationAttempt.branch_result(CHANGED, changed, receipt, None)


def _mutation_for_failure(
    classified: _Classified, receipt: Receipt, receipt_handle_invalid: str, value: Output
) -> MutationAttempt[Output]:
    if classified.outcome is _Outcome.RETRY:
        return MutationAttempt.retry(classified.failure, classified.retry_after_seconds)
    if classified.outcome is _Outcome.RECEIPT_HANDLE_INVALID and receipt_handle_invalid:
        return MutationAttempt.branch_result(receipt_handle_invalid, value, receipt, classified.failure)
    if classified.outcome is _Outcome.DEFECT:
        return MutationAttempt.branch_result(DEFECT, value, receipt, classified.failure)
    return MutationAttempt.branch_result(PROVIDER_REJECTED, value, receipt, classified.failure)


def _classify_error_response(operation: str, subject: str, response: _ErrorResponse) -> _Classified:
    status, code = response.status, response.code

    def classified(outcome: _Outcome, kind: FailureKind, message: str) -> _Classified:
        return _Classified(
            outcome, _failure(operation, kind, message), response.retry_after_seconds, response.request_id
        )

    def status_message(prefix: str) -> str:
        return f"{prefix} with HTTP {status} {code}" if code else f"{prefix} with HTTP {status}"

    if status == 429 or code in _THROTTLING_CODES:
        return classified(_Outcome.RETRY, FailureKind.RATE_LIMIT, "SQS throttled the " + subject)
    if code == "RequestTimeout" or status == 408:
        return classified(_Outcome.RETRY, FailureKind.TRANSPORT, "SQS timed out the " + subject)
    if status >= 500 and status != 501:
        message = status_message("SQS is temporarily unable to serve the " + subject)
        return classified(_Outcome.RETRY, FailureKind.AVAILABILITY, message)
    if code in _RECEIPT_HANDLE_CODES or (
        code in ("InvalidParameterValue", "InvalidParameterValueException")
        and response.is_about_receipt_handle
    ):
        message = (
            status_message("SQS no longer accepts the receipt handle for the " + subject)
            + "; the message may be delivered again"
        )
        return classified(_Outcome.RECEIPT_HANDLE_INVALID, FailureKind.NOT_FOUND, message)
    if code in ("QueueDoesNotExist", "AWS.SimpleQueueService.NonExistentQueue"):
        message = (
            status_message("SQS rejected the " + subject)
            + " because the queue does not exist; check queueUrl and region"
        )
        return classified(_Outcome.REJECTED, FailureKind.NOT_FOUND, message)
    if code in ("OverLimit", "AWS.SimpleQueueService.OverLimit"):
        message = (
            status_message("SQS rejected the " + subject)
            + " because the queue holds too many in-flight messages; delete or release received messages"
        )
        return classified(_Outcome.REJECTED, FailureKind.PROVIDER_REJECTION, message)
    if code in ("RequestTimeTooSkewed", "SignatureExpired"):
        message = (
            "SQS rejected the "
            + subject
            + " because the Worker clock differs from AWS by more than 5 minutes"
        )
        return classified(_Outcome.REJECTED, FailureKind.AUTHENTICATION, message)
    if code in _CREDENTIAL_CODES or status == 401:
        message = (
            status_message("SQS rejected the connection credentials for the " + subject)
            + "; replace the access key or session token"
        )
        return classified(_Outcome.REJECTED, FailureKind.AUTHENTICATION, message)
    if code in _AUTHORIZATION_CODES or status == 403:
        return classified(
            _Outcome.REJECTED, FailureKind.AUTHORIZATION, status_message("SQS denied the " + subject)
        )
    message = status_message("SQS rejected the " + subject)
    return classified(_Outcome.REJECTED, FailureKind.PROVIDER_REJECTION, message)


def _decode_receive_response(contents: bytes, max_messages: int) -> tuple[Message, ...]:
    try:
        document = json.loads(contents)
        if not isinstance(document, dict):
            raise ValueError()
        raw_messages = document.get("Messages")
        items = [] if raw_messages is None else _list(raw_messages)
        received = [_structural_message(item) for item in items]
    except (ValueError, UnicodeDecodeError):
        raise ValueError("the document is not a ReceiveMessage response") from None
    if len(received) > max_messages:
        raise ValueError("the batch holds more messages than requested")
    return tuple(_decode_message(item) for item in received)


def _structural_message(value: Any) -> dict[str, Any]:
    """Check the JSON types Go's decoder requires before semantic validation."""
    document = {} if value is None else _object(value)
    for name in ("MessageId", "ReceiptHandle", "MD5OfBody", "Body"):
        _optional_string(document, name)
    attributes = document.get("Attributes")
    if attributes is not None and any(
        not isinstance(item, str) and item is not None for item in _object(attributes).values()
    ):
        raise ValueError("attributes must be strings")
    message_attributes = document.get("MessageAttributes")
    if message_attributes is not None:
        for attribute in _object(message_attributes).values():
            attribute_document = {} if attribute is None else _object(attribute)
            for name in ("DataType", "StringValue", "BinaryValue"):
                _optional_string(attribute_document, name)
            binary = attribute_document.get("BinaryValue")
            if binary:
                try:
                    base64.b64decode(binary, validate=True)
                except (binascii.Error, ValueError):
                    raise ValueError("binary value is not base64") from None
    return document


def _decode_message(received: dict[str, Any]) -> Message:
    message_id = _optional_string(received, "MessageId")
    if not message_id or not _is_valid_message_id(message_id):
        raise ValueError("a message ID is missing or not printable")
    receipt_handle = _optional_string(received, "ReceiptHandle")
    if not _is_valid_receipt_handle(receipt_handle):
        raise ValueError("a receipt handle is missing or not printable")
    body = received.get("Body")
    if not isinstance(body, str):
        raise ValueError("a message body is missing or not UTF-8")
    try:
        encoded_body = body.encode("utf-8")
    except UnicodeEncodeError:
        raise ValueError("a message body is missing or not UTF-8") from None
    md5_of_body = _optional_string(received, "MD5OfBody")
    if md5_of_body != hashlib.md5(encoded_body, usedforsecurity=False).hexdigest():
        raise ValueError("a message body does not match its MD5 digest")
    attributes = {name: value or "" for name, value in (received.get("Attributes") or {}).items()}
    count_text = attributes.get("ApproximateReceiveCount", "")
    if not _INTEGER_PATTERN.fullmatch(count_text) or int(count_text) < 1:
        raise ValueError("a receive count is not a positive integer")
    try:
        sent_at = _epoch_milliseconds(attributes.get("SentTimestamp", ""))
    except ValueError:
        raise ValueError("a sent timestamp is not epoch milliseconds") from None
    try:
        first_received_at = _epoch_milliseconds(attributes.get("ApproximateFirstReceiveTimestamp", ""))
    except ValueError:
        raise ValueError("a first-receive timestamp is not epoch milliseconds") from None
    message_attributes: dict[str, MessageAttributeValue] = {}
    for name, value in (received.get("MessageAttributes") or {}).items():
        attribute = value or {}
        data_type = attribute.get("DataType") or ""
        if not _MESSAGE_ATTRIBUTE_NAME_PATTERN.fullmatch(name) or not data_type:
            raise ValueError("a message attribute has an invalid name, data type, or value")
        binary = attribute.get("BinaryValue") or ""
        canonical_binary = (
            base64.b64encode(base64.b64decode(binary, validate=True)).decode() if binary else ""
        )
        message_attributes[name] = MessageAttributeValue(
            data_type, attribute.get("StringValue") or "", canonical_binary
        )
    return Message(
        message_id=message_id,
        receipt_handle=receipt_handle,
        body=body,
        md5_of_body=md5_of_body,
        sent_at=sent_at,
        first_received_at=first_received_at,
        receive_count=int(count_text),
        message_group_id=attributes.get("MessageGroupId", ""),
        message_deduplication_id=attributes.get("MessageDeduplicationId", ""),
        sequence_number=attributes.get("SequenceNumber", ""),
        message_attributes=message_attributes,
    )


def _epoch_milliseconds(value: str) -> datetime:
    if not value:
        return ZERO_TIME
    if not _INTEGER_PATTERN.fullmatch(value) or int(value) < 0:
        raise ValueError("timestamp is not epoch milliseconds")
    return datetime(1970, 1, 1, tzinfo=UTC) + timedelta(milliseconds=int(value))


def _resolve_endpoint(configured: str, region: str) -> tuple[str, bool]:
    is_amazon_endpoint = not configured
    if is_amazon_endpoint:
        suffix = next(
            (suffix for pattern, suffix in _PARTITION_DNS_SUFFIXES if pattern.fullmatch(region)), ""
        )
        if not suffix:
            raise ValueError(
                "SQS region is not an Amazon SQS Region; set endpoint for an SQS-compatible server"
            )
        configured = f"https://sqs.{region}.{suffix}"
    try:
        validated = validate_base_url(configured)
    except ValueError as error:
        raise ValueError(f"SQS endpoint: {error}") from None
    parsed = urlsplit(validated)
    if not _ENDPOINT_PATH_PATTERN.fullmatch(parsed.path) or "%" in parsed.path:
        raise ValueError("SQS endpoint path may contain only letters, digits, and - . _ ~ segments")
    if not parsed.path:
        validated += "/"
    return validated, is_amazon_endpoint


def _parse_queue_url(queue_url: str) -> _Queue:
    invalid = ValueError(
        "queueUrl must be a queue URL such as https://sqs.eu-west-1.amazonaws.com/123456789012/orders"
    )
    if len(queue_url.encode("utf-8", "surrogatepass")) > _MAXIMUM_QUEUE_URL_BYTES or any(
        ord(character) < 0x21 or ord(character) > 0x7E for character in queue_url
    ):
        raise invalid
    try:
        parsed = urlsplit(queue_url)
        hostname = parsed.hostname
    except ValueError:
        raise invalid from None
    if (
        parsed.scheme not in ("https", "http")
        or not hostname
        or "@" in parsed.netloc
        or "?" in queue_url
        or "#" in queue_url
        or "%" in parsed.path
    ):
        raise invalid
    owner, separator, name = parsed.path.removeprefix("/").partition("/")
    if not separator or not _QUEUE_OWNER_PATTERN.fullmatch(owner):
        raise invalid
    is_fifo = name.endswith(_FIFO_SUFFIX)
    base_name = name.removesuffix(_FIFO_SUFFIX)
    if not _QUEUE_NAME_PATTERN.fullmatch(base_name) or len(name) > 80:
        raise ValueError(
            "queueUrl must end with a queue name of 1 to 80 letters, digits, hyphens, and underscores, "
            "including any .fifo suffix"
        )
    match = _AMAZON_QUEUE_HOST_PATTERN.fullmatch(hostname)
    return _Queue(queue_url, name, is_fifo, match.group(1) if match else "")


def _validate_acknowledgement(client: Client, queue_url: str, receipt_handle: str, message_id: str) -> _Queue:
    queue = client.resolve_queue(queue_url)
    if not _is_valid_receipt_handle(receipt_handle):
        raise ValueError("receiptHandle must be the handle receiveMessages returned for the message")
    if not _is_valid_message_id(message_id):
        raise ValueError("messageId must be blank or the message ID receiveMessages returned")
    return queue


def _is_valid_receipt_handle(receipt_handle: str) -> bool:
    return (
        bool(receipt_handle)
        and len(receipt_handle.encode("utf-8", "surrogatepass")) <= _MAXIMUM_RECEIPT_HANDLE_BYTES
        and _is_printable_token(receipt_handle)
    )


def _is_valid_message_id(message_id: str) -> bool:
    return len(message_id.encode("utf-8", "surrogatepass")) <= _MAXIMUM_MESSAGE_ID_BYTES and (
        not message_id or _is_printable_token(message_id)
    )


def _is_printable_token(value: str) -> bool:
    return all("!" <= character <= "~" for character in value)


def _message_object_id(queue: _Queue, message_id: str) -> str:
    return f"{queue.url}#{message_id}" if message_id else queue.url


def _contains_credential(value: str, credentials: Credentials) -> bool:
    secrets = (credentials.secret_access_key.reveal(), credentials.session_token.reveal())
    return any(secret and secret in value for secret in secrets)


def _safe_request_id(headers: httpx.Headers) -> str:
    request_id = str(headers.get(_REQUEST_ID_HEADER, ""))
    return request_id if _REQUEST_ID_PATTERN.fullmatch(request_id) else ""


def _transport_failure(operation: str, subject: str) -> Failure:
    return _failure(
        operation, FailureKind.TRANSPORT, "SQS could not be reached or did not finish the " + subject
    )


def _failure(operation: str, kind: FailureKind, message: str) -> Failure:
    return Failure(kind, CONNECTOR_ID, operation, message)


def _object(value: Any) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise ValueError("expected a JSON object")
    return value


def _list(value: Any) -> list[Any]:
    if not isinstance(value, list):
        raise ValueError("expected a JSON array")
    return value


def _optional_string(document: dict[str, Any], name: str) -> str:
    value = document.get(name)
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ValueError(f"{name} must be a string")
    return value
