# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Classification, cursor documents, and provider signals of the shared changes.list feed."""

import json
from datetime import UTC, datetime, timedelta

import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider, TriggerContext
from dex_connectors.sdk.pollingtrigger import Cursor, RateLimitedError, is_cursor_invalid

import dex_connectors.google_drive as drive
from dex_connectors.google_drive.file_changes import (
    FILE_CHANGES_FEED_NAME,
    FileChangeFetcher,
    _decode_file_change_cursor,
    _encode_file_change_cursor,
    classify_file_change,
)
from dex_connectors.google_drive.operations import _timestamp

CHECKPOINT = datetime(2026, 11, 1, 12, 0, tzinfo=UTC)
CONNECTION = ConnectionRef("google", "drive-files")


def millis(value: datetime) -> str:
    return str(int(value.timestamp() * 1000))


def event_id(event: object) -> str:
    return "" if event is None else event.id  # type: ignore[attr-defined]


OLD = CHECKPOINT - timedelta(hours=1)
RECENT = CHECKPOINT + timedelta(minutes=1)
WITHIN_LOOKBACK = CHECKPOINT - timedelta(minutes=1)


@pytest.mark.parametrize(
    ("file", "created", "updated", "trashed"),
    [
        (drive.FileEvent(file_id="f1", created_time=RECENT, modified_time=RECENT), "f1@created", "", ""),
        (drive.FileEvent(file_id="f1", created_time=RECENT, modified_time=OLD), "f1@created", "", ""),
        (
            drive.FileEvent(file_id="f1", created_time=RECENT, modified_time=RECENT + timedelta(minutes=1)),
            "f1@created",
            "f1@modified@" + millis(RECENT + timedelta(minutes=1)),
            "",
        ),
        (
            drive.FileEvent(file_id="f1", created_time=OLD, modified_time=RECENT),
            "",
            "f1@modified@" + millis(RECENT),
            "",
        ),
        (
            drive.FileEvent(file_id="f1", created_time=WITHIN_LOOKBACK, modified_time=WITHIN_LOOKBACK),
            "f1@created",
            "",
            "",
        ),
        (drive.FileEvent(file_id="f1", created_time=OLD, modified_time=OLD), "", "", ""),
        (
            drive.FileEvent(
                file_id="f1", created_time=OLD, modified_time=RECENT, is_trashed=True, trashed_time=RECENT
            ),
            "",
            "",
            "f1@trashed@" + millis(RECENT),
        ),
        (
            drive.FileEvent(
                file_id="f1", created_time=OLD, modified_time=OLD, is_trashed=True, change_time=RECENT
            ),
            "",
            "",
            "f1@trashed@" + millis(RECENT),
        ),
        (
            drive.FileEvent(
                file_id="f1",
                created_time=OLD,
                modified_time=OLD,
                is_trashed=True,
                trashed_time=OLD,
                change_time=RECENT,
            ),
            "",
            "",
            "",
        ),
    ],
    ids=[
        "new upload",
        "upload keeps older local mtime",
        "created then edited",
        "edit of an old file",
        "late change record inside the lookback",
        "permission or view change",
        "trashed with trashed time",
        "trashed without trashed time uses the change time",
        "change to a long-trashed file",
    ],
)
def test_classify_file_change_derives_kind_from_file_times(
    file: drive.FileEvent, created: str, updated: str, trashed: str
) -> None:
    change = classify_file_change(file, CHECKPOINT)
    assert (event_id(change.created), event_id(change.updated), event_id(change.trashed)) == (
        created,
        updated,
        trashed,
    )


def test_configuration_validation_matches_go() -> None:
    drive.validate_file_trigger_configuration(
        drive.FileTriggerConfiguration(
            folder_id="folder_a", folder_name="Invoices", mime_type="application/vnd.google-apps.document"
        )
    )
    for invalid in (
        drive.FileTriggerConfiguration(folder_id="https://drive.google.com/drive/folders/x"),
        drive.FileTriggerConfiguration(mime_type="Application/PDF"),
        drive.FileTriggerConfiguration(mime_type="text/plain; charset=utf-8"),
    ):
        with pytest.raises(ValueError):
            drive.validate_file_trigger_configuration(invalid)


def test_cursor_documents_are_the_go_connectors_bytes() -> None:
    # Go writes the checkpoint from time.Now().UTC() with nanoseconds, and provider times keep their offset.
    stored = (
        '{"pageToken":"101","checkpoint":"2026-11-01T12:00:00.123456789Z",'
        '"roundLatest":"2026-11-01T13:01:00.5+01:00"}'
    )
    position = _decode_file_change_cursor(stored)
    assert _encode_file_change_cursor(position).token == stored
    assert (
        _encode_file_change_cursor(position.__class__("1<&> ", CHECKPOINT)).token
        == '{"pageToken":"1<&>\\u2028","checkpoint":"2026-11-01T12:00:00Z"}'
    )
    for invalid in (
        '{"pageToken":"100"}',
        '{"checkpoint":"2026-11-01T12:00:00Z"}',
        '{"pageToken":"100","checkpoint":"2026-11-01T12:00:00Z","extra":1}',
        '{"pageToken":100,"checkpoint":"2026-11-01T12:00:00Z"}',
        "",
    ):
        with pytest.raises(ValueError):
            _decode_file_change_cursor(invalid)
    # Go matches field names case-insensitively and ignores data after the first JSON value.
    assert (
        _decode_file_change_cursor('{"PageToken":"7","checkpoint":"2026-11-01T12:00:00Z"} x').page_token
        == "7"
    )


def test_file_event_payload_decodes_go_inbox_json() -> None:
    payload = (
        '{"fileId":"f1","name":"Invoice.pdf","mimeType":"application/pdf","parents":["folder_a"],'
        '"createdTime":"2026-11-01T12:00:00.123456789Z","modifiedTime":"2026-11-01T12:00:00Z",'
        '"changeTime":"2026-11-01T12:00:01Z","isTrashed":false,"sizeBytes":42,"future":true}'
    )
    decoded = drive.decode_file_event_payload(payload, payload.encode())
    assert decoded.file_id == "f1" and decoded.parents == ("folder_a",) and decoded.size_bytes == 42
    assert decoded.created_time == _timestamp("2026-11-01T12:00:00.123456789Z")
    with pytest.raises(ValueError):
        drive.decode_file_event_payload('{"sizeBytes":1.5}', b'{"sizeBytes":1.5}')


def change_json(file_id: str, created: datetime, changed: datetime) -> str:
    created_text = created.isoformat().replace("+00:00", "Z")
    return json.dumps(
        {
            "changeType": "file",
            "fileId": file_id,
            "time": changed.isoformat().replace("+00:00", "Z"),
            "file": {
                "id": file_id,
                "name": "Invoice.pdf",
                "mimeType": "application/pdf",
                "parents": ["folder_a"],
                "createdTime": created_text,
                "modifiedTime": created_text,
                "size": "42",
            },
        }
    )


def new_fetcher(handler: httpx.MockTransport) -> FileChangeFetcher:
    provider = StaticCredentialProvider(
        {CONNECTION: drive.Credentials(access_token=SecretString("token-SENTINEL"))}
    )
    client = drive.Client(
        drive.Config(endpoint="https://drive.example"),
        provider,
        http_client=httpx.AsyncClient(transport=handler),
        now=lambda: CHECKPOINT,
    )
    return FileChangeFetcher(client, CONNECTION, FILE_CHANGES_FEED_NAME)


async def test_fetcher_seeds_and_keeps_checkpoint_across_pages_of_one_pass() -> None:
    first_change, second_change = CHECKPOINT + timedelta(minutes=1), CHECKPOINT + timedelta(minutes=2)
    requests: list[httpx.Request] = []

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        assert request.headers["Authorization"] == "Bearer token-SENTINEL"
        page_token = request.url.params.get("pageToken", "")
        if page_token == "":
            assert request.url.path == "/drive/v3/changes/startPageToken"
            return httpx.Response(200, content=b'{"startPageToken":"100"}')
        if page_token == "100":
            body = (
                '{"nextPageToken":"101","changes":['
                + change_json("f1", first_change, first_change)
                + ',{"changeType":"drive","driveId":"d1"},'
                + '{"changeType":"file","fileId":"gone","removed":true}]}'
            )
            return httpx.Response(200, content=body.encode())
        body = (
            '{"newStartPageToken":"102","changes":[' + change_json("f2", second_change, second_change) + "]}"
        )
        return httpx.Response(200, content=body.encode())

    fetcher = new_fetcher(httpx.MockTransport(respond))
    seeded = await fetcher.seed_cursor(TriggerContext())
    assert seeded.token == '{"pageToken":"100","checkpoint":"2026-11-01T12:00:00Z"}'
    first = await fetcher.fetch_changes(TriggerContext(), seeded)
    assert first.has_more_pages and [event_id(change.created) for change in first.changes] == ["f1@created"]
    assert first.next_cursor.token == (
        '{"pageToken":"101","checkpoint":"2026-11-01T12:00:00Z","roundLatest":"2026-11-01T12:01:00Z"}'
    )
    second = await fetcher.fetch_changes(TriggerContext(), first.next_cursor)
    assert not second.has_more_pages and event_id(second.changes[0].created) == "f2@created"
    assert second.next_cursor.token == '{"pageToken":"102","checkpoint":"2026-11-01T12:02:00Z"}'
    listed = requests[1].url.params
    for name, value in {
        "pageSize": "100",
        "spaces": "drive",
        "includeRemoved": "false",
        "includeItemsFromAllDrives": "true",
        "supportsAllDrives": "true",
    }.items():
        assert listed[name] == value, name
    assert "newStartPageToken" in listed["fields"]
    assert requests[0].url.params["supportsAllDrives"] == "true"
    # Go's url.Values.Encode sorts the query, so both languages send byte-identical requests.
    assert str(requests[1].url.query, "ascii").startswith("fields=nextPageToken%2CnewStartPageToken")


async def test_fetcher_maps_provider_failures_to_poll_signals() -> None:
    response = {"status": 200, "body": b"", "retry_after": ""}

    def respond(request: httpx.Request) -> httpx.Response:
        headers = {"Retry-After": str(response["retry_after"])} if response["retry_after"] else {}
        return httpx.Response(int(str(response["status"])), headers=headers, content=bytes(response["body"]))  # type: ignore[call-overload]

    fetcher = new_fetcher(httpx.MockTransport(respond))
    cursor = Cursor('{"pageToken":"100","checkpoint":"2026-11-01T12:00:00Z"}')
    context = TriggerContext()

    async def failure(status: int, body: bytes = b"", retry_after: str = "") -> BaseException:
        response.update(status=status, body=body, retry_after=retry_after)
        with pytest.raises(Exception) as raised:
            await fetcher.fetch_changes(context, cursor)
        return raised.value

    rate_limited = await failure(429, retry_after="7")
    assert isinstance(rate_limited, RateLimitedError) and rate_limited.retry_after == timedelta(seconds=7)
    assert isinstance(
        await failure(403, b'{"error":{"errors":[{"reason":"userRateLimitExceeded"}]}}'), RateLimitedError
    )
    assert is_cursor_invalid(await failure(400, b'{"error":{"errors":[{"reason":"invalid"}]}}'))
    pagetoken = (
        b'{"error":{"errors":[{"reason":"invalid","locationType":"parameter","location":"pageToken"}]}}'
    )
    assert is_cursor_invalid(await failure(400, pagetoken))
    for rejected in (404, 410):
        assert is_cursor_invalid(await failure(rejected)), rejected
    fields = (
        b'{"error":{"errors":[{"reason":"invalidParameter","locationType":"parameter","location":"fields"}]}}'
    )
    assert not is_cursor_invalid(await failure(400, fields)), (
        "a 400 for another parameter recurs after reseeds"
    )
    with pytest.raises(Exception) as seed_failure:
        await fetcher.seed_cursor(context)
    assert not is_cursor_invalid(seed_failure.value), "a rejected seed is retried, not reseeded"
    unavailable = await failure(503)
    assert not is_cursor_invalid(unavailable) and not isinstance(unavailable, RateLimitedError)
    assert "neither a next page token nor a new start page token" in str(
        await failure(200, b'{"changes":[]}')
    )
    assert "invalid change response" in str(await failure(200, b'{"changes":[{"time":5}]}'))
    with pytest.raises(Exception) as invalid_cursor:
        await fetcher.fetch_changes(context, Cursor('{"pageToken":"100"}'))
    assert is_cursor_invalid(invalid_cursor.value)


def test_client_validates_poll_settings() -> None:
    provider = StaticCredentialProvider({CONNECTION: drive.Credentials(access_token=SecretString("inert"))})
    for invalid in (
        drive.Config(poll_interval_seconds=0.5),
        drive.Config(poll_page_size=501),
        drive.Config(poll_page_size=-1),
    ):
        with pytest.raises(ValueError):
            drive.Client(invalid, provider)
