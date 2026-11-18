# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded resend of uploadFile: accepted uploads are never resent."""

import asyncio
import json
from collections.abc import AsyncIterator, Callable
from datetime import UTC, datetime, timedelta
from typing import get_origin

import dex_connectors.sdk as sdk
import httpx
import pytest
from dex_connectors.sdk.projectconfig import (
    ConnectionNotSelectedError,
    CredentialBrokerRejectedError,
    CredentialBrokerUnavailableError,
    ObjectNotFoundError,
)
from dex_connectors.sdk.providerhttp import MAX_ERROR_BODY_BYTES

import dex_connectors.google_drive as drive
from dex_connectors.google_drive._generated.connection import CredentialMaterialError, decode_credentials

CONNECTION = sdk.ConnectionRef("google", "drive-files")
CALL_ID = sdk.CallID("6072a0ce-9ddb-5937-8998-675e48cfa409")
CREATED_FILE = {
    "id": "created-1",
    "name": "report.txt",
    "mimeType": "text/plain",
    "createdTime": "2026-09-30T12:00:00Z",
    "size": "6",
    "appProperties": {"dexIdempotencyKey": str(CALL_ID)},
}


class AttemptContext:
    """Keep the last heartbeat across Execute attempts like Dex does."""

    def __init__(self, attempt: int = 1) -> None:
        self.attempt = attempt
        self.run_id = "run"
        self.deadline = datetime.now(UTC) + timedelta(seconds=30)
        self.heartbeat_value: object = None
        self.heartbeat_count = 0

    def has_last_heartbeat_value(self) -> bool:
        """Report whether an earlier attempt recorded a heartbeat."""
        return self.heartbeat_value is not None

    def get_last_heartbeat_value(self, value_type: type[object]) -> object:
        """Return the last heartbeat, failing like Dex when it cannot decode into value_type."""
        value = self.heartbeat_value
        decoded_type = get_origin(value_type) or value_type
        if decoded_type is not object and not isinstance(value, decoded_type):
            raise TypeError("the heartbeat does not decode into the requested type")
        return value

    async def heartbeat(self, value: object = None) -> None:
        """Record the heartbeat exactly as the SDK wrote it."""
        self.heartbeat_count += 1
        self.heartbeat_value = value


class BrokenStream(httpx.AsyncByteStream):
    """A response body that fails after its first bytes."""

    async def __aiter__(self) -> AsyncIterator[bytes]:
        yield b'{"id":'
        raise httpx.ReadError("connection reset")


class FailingCredentials:
    """A credential provider that always fails with one error."""

    def __init__(self, error: Exception) -> None:
        self.error = error

    async def resolve(self, call: sdk.Call) -> drive.Credentials:
        """Raise the configured error."""
        raise self.error


class FakeDrive:
    """Answer lookups from stored files and uploads with queued outcomes."""

    def __init__(self, *upload_outcomes: Callable[[], httpx.Response]) -> None:
        self.upload_outcomes = list(upload_outcomes)
        self.files: list[dict[str, object]] = []
        self.is_lookup_hidden = False
        self.uploads = 0

    def respond(self, request: httpx.Request) -> httpx.Response:
        """Serve one Drive request."""
        if request.method == "POST":
            self.uploads += 1
            outcome = self.upload_outcomes.pop(0)
            return outcome()
        if request.url.path == "/drive/v3/files":
            files = [] if self.is_lookup_hidden else self.files
            return httpx.Response(200, json={"files": files})
        file_id = request.url.path.rsplit("/", 1)[-1]
        for file in self.files:
            if file["id"] == file_id:
                return httpx.Response(200, json=file)
        return httpx.Response(404, json={"error": {"code": 404}})

    def stores(self, response: httpx.Response) -> Callable[[], httpx.Response]:
        """Store the created file, then answer with response."""

        def outcome() -> httpx.Response:
            self.files.append(CREATED_FILE)
            return response

        return outcome


async def _run(fake: FakeDrive, context: AttemptContext, credentials: object | None = None) -> str:
    """Run one uploadFile attempt and return its branch or `retry`."""
    async with httpx.AsyncClient(transport=httpx.MockTransport(fake.respond)) as http_client:
        provider = credentials or sdk.StaticCredentialProvider(
            {CONNECTION: drive.Credentials(access_token=sdk.SecretString("inert-token"))}
        )
        client = drive.Client(drive.Config(endpoint="http://127.0.0.1:9"), provider, http_client)  # type: ignore[arg-type]
        call = sdk.Call(
            CALL_ID, CONNECTION, sdk.OperationRef("google-drive", "uploadFile"), context=context, max_sends=3
        )
        upload = drive.UploadFileInput(name="report.txt", mime_type="text/plain", text_content="report")
        try:
            return (await sdk.run_mutation(client.upload_file(), call, upload)).branch
        except sdk.RetryError:
            return "retry"


@pytest.mark.asyncio
async def test_a_broken_success_body_is_looked_up_on_later_attempts_and_never_resent() -> None:
    """A 2xx status proves the upload; later attempts only look it up."""
    fake = FakeDrive()
    fake.upload_outcomes.append(fake.stores(httpx.Response(200, stream=BrokenStream())))
    fake.is_lookup_hidden = True
    context = AttemptContext()
    assert await _run(fake, context) == "retry"
    context.attempt += 1
    assert await _run(fake, context) == "retry"
    fake.is_lookup_hidden = False
    context.attempt += 1
    assert await _run(fake, context) == "uploaded"
    assert fake.uploads == 1


class StalledStream(httpx.AsyncByteStream):
    """A response body that stalls after the 2xx headers arrived."""

    async def __aiter__(self) -> AsyncIterator[bytes]:
        await asyncio.sleep(5)
        yield b"{}"


@pytest.mark.asyncio
async def test_a_write_deadline_that_cuts_an_accepted_body_is_looked_up_and_never_resent() -> None:
    """2xx headers arrived, so the Fence write-window timeout proves the upload."""
    fake = FakeDrive()
    fake.upload_outcomes.append(fake.stores(httpx.Response(200, stream=StalledStream())))
    fake.is_lookup_hidden = True
    context = AttemptContext()
    context.deadline = datetime.now(UTC) + timedelta(seconds=1.2)
    assert await _run(fake, context) == "retry"
    context.attempt += 1
    context.deadline = datetime.now(UTC) + timedelta(seconds=30)
    assert await _run(fake, context) == "retry"
    fake.is_lookup_hidden = False
    context.attempt += 1
    assert await _run(fake, context) == "uploaded"
    assert fake.uploads == 1


@pytest.mark.asyncio
async def test_a_refusal_whose_body_is_oversized_is_an_unknown_outcome() -> None:
    """An oversized refusal body might hide a file ID, so it is looked up and resent, never rejected."""
    oversized = b"x" * (drive.Config().max_response_bytes + MAX_ERROR_BODY_BYTES + 1)
    fake = FakeDrive(
        lambda: httpx.Response(400, content=oversized), lambda: httpx.Response(200, json=CREATED_FILE)
    )
    context = AttemptContext()
    assert await _run(fake, context) == "retry"
    context.attempt += 1
    assert await _run(fake, context) == "uploaded"
    assert fake.uploads == 2


@pytest.mark.asyncio
async def test_an_accepted_upload_that_stays_invisible_is_uncertain_at_the_cap() -> None:
    """Lookups stop at the send cap and select uncertain without any resend."""
    fake = FakeDrive()
    fake.upload_outcomes.append(fake.stores(httpx.Response(200, content=b'{"name":"report.txt"}')))
    fake.is_lookup_hidden = True
    context = AttemptContext()
    branches = []
    for _ in range(3):
        branches.append(await _run(fake, context))
        context.attempt += 1
    assert branches == ["retry", "retry", "uncertain"]
    assert fake.uploads == 1


@pytest.mark.asyncio
async def test_a_refusal_carrying_a_file_id_is_read_back_and_never_resent() -> None:
    """A file ID contradicts a 4xx refusal, so the file is read back by ID."""
    fake = FakeDrive()
    fake.upload_outcomes.append(fake.stores(httpx.Response(422, content=json.dumps(CREATED_FILE).encode())))
    assert await _run(fake, AttemptContext()) == "uploaded"
    assert fake.uploads == 1


@pytest.mark.asyncio
async def test_the_lookup_runs_before_a_resend_after_a_throttled_resend() -> None:
    """After unknown, then 429, the third attempt finds the late first upload."""
    fake = FakeDrive()
    fake.upload_outcomes.append(fake.stores(httpx.Response(500, json={"error": {"code": 500}})))
    fake.upload_outcomes.append(lambda: httpx.Response(429, json={"error": {"code": 429}}))
    fake.is_lookup_hidden = True
    context = AttemptContext()
    assert await _run(fake, context) == "retry"
    context.attempt += 1
    assert await _run(fake, context) == "retry"
    fake.is_lookup_hidden = False
    context.attempt += 1
    assert await _run(fake, context) == "uploaded"
    assert fake.uploads == 2


@pytest.mark.asyncio
async def test_credential_failures_never_record_not_sent_after_a_possible_send() -> None:
    """A credential outage retries; reauthorization selects defect, or uncertain after a possible send."""
    outage = FailingCredentials(RuntimeError("credential store offline"))
    after_possible_send = AttemptContext(attempt=2)
    assert await _run(FakeDrive(), after_possible_send, outage) == "retry"
    assert after_possible_send.heartbeat_count == 0
    revoked = FailingCredentials(sdk.ReauthorizationRequiredError())
    assert await _run(FakeDrive(), AttemptContext(), revoked) == "defect"
    assert await _run(FakeDrive(), AttemptContext(attempt=2), revoked) == "uncertain"


def sdk_wrapped_material_error() -> ValueError:
    """Build the ValueError the SDK project provider raises after the generated decoder refuses material."""
    try:
        try:
            decode_credentials({"unknown": "value"})
        except CredentialMaterialError:
            raise ValueError("project credential decoding failed") from None
    except ValueError as error:
        return error
    raise AssertionError("malformed material decoded")


CREDENTIAL_FAILURES: list[tuple[Exception, bool]] = [
    (sdk_wrapped_material_error(), True),
    (sdk.ReauthorizationRequiredError(), True),
    (ObjectNotFoundError(), True),
    (ConnectionNotSelectedError(), True),
    (CredentialBrokerRejectedError(), True),
    (CredentialBrokerUnavailableError(), False),
    (ValueError("connection is not configured"), False),
    (RuntimeError("unlisted"), False),
]


@pytest.mark.asyncio
@pytest.mark.parametrize(("error", "is_terminal"), CREDENTIAL_FAILURES)
async def test_resolve_failures_follow_one_classification(error: Exception, is_terminal: bool) -> None:
    """Only reconnection-only failures are terminal; every other resolve failure retries."""
    credentials = FailingCredentials(error)
    first = await _run(FakeDrive(), AttemptContext(), credentials)
    later = await _run(FakeDrive(), AttemptContext(attempt=2), credentials)
    assert (first, later) == (("defect", "uncertain") if is_terminal else ("retry", "retry"))


class RefreshFailingCredentials:
    """Resolve a token Google rejects, then fail the refresh after rejection."""

    def __init__(self, error: Exception) -> None:
        self.error = error

    async def resolve(self, call: sdk.Call) -> drive.Credentials:
        """Return the rejected token."""
        return drive.Credentials(access_token=sdk.SecretString("rejected-token"))

    async def resolve_after_rejection(self, call: sdk.Call) -> drive.Credentials:
        """Raise the configured refresh error."""
        raise self.error


@pytest.mark.asyncio
@pytest.mark.parametrize(("error", "is_terminal"), CREDENTIAL_FAILURES)
async def test_upload_401_refresh_failure_retries_unless_terminal(
    error: Exception, is_terminal: bool
) -> None:
    """A 401 proves Drive did not process the upload, so a transient refresh failure retries."""
    fake = FakeDrive(lambda: httpx.Response(401, json={"error": {"code": 401}}))
    outcome = await _run(fake, AttemptContext(), RefreshFailingCredentials(error))
    assert outcome == ("providerRejected" if is_terminal else "retry")
    assert fake.uploads == 1
