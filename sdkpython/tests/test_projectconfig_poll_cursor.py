# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Poll cursor keys, documents, and compare-and-swap, checked against Go-generated golden fixtures.

tests/fixtures/go-poll-cursor-cases.json is written by the Go poll Trigger kit (sdkgo v0.38.0):
`projectconfig.PollCursorStore` head keys, the bytes it writes for each state, the bytes it rewrites
after reading each stored document (or that it rejects it), and `pollingtrigger.NewTriggerCredentialCall`.
"""

import base64
import json
from pathlib import Path
from typing import Any

import pytest

from dex_connectors.sdk import ConnectionRef, OperationRef, TriggerRef
from dex_connectors.sdk.pollingtrigger import new_memory_cursor_store, new_trigger_credential_call
from dex_connectors.sdk.projectconfig import (
    MAXIMUM_KEYED_SNAPSHOT_ENTRIES,
    MAXIMUM_POLL_CURSOR_TOKEN_BYTES,
    InMemoryObjectStore,
    KeyedSnapshot,
    KeyedSnapshotEntry,
    ObjectConflictError,
    PollCursorDocumentInvalidError,
    PollCursorKey,
    PollCursorState,
    PollCursorStore,
    PollCursorTooLargeError,
    PollFeedKey,
    Scope,
    TriggerInboxKey,
    decode_poll_cursor_document,
    encode_poll_cursor_document,
)
from dex_connectors.sdk.projectconfig.storage import Object, ObjectNotFoundError, OutcomeUnknownError

FIXTURE = json.loads((Path(__file__).parent / "fixtures" / "go-poll-cursor-cases.json").read_text("utf-8"))
SCOPE = Scope("sample-project", "preview")
FIXTURE_KEY_PARTS = ("sample", "main-é-流程 😀", "itemCreated", "main / west")
KEY = PollCursorKey(*FIXTURE_KEY_PARTS)
GOLDEN_HEAD_KEY = FIXTURE["keys"][0]["key"]
UNSEEDED = b'{"schemaVersion":"connectors.dex.dev/project-poll-cursor/v1alpha1","state":null}'


def state_from_fixture(spec: dict[str, Any]) -> PollCursorState:
    """Build the state a fixture's Go stateSpec describes."""
    snapshot = None
    if spec["snapshot"] is not None:
        snapshot = KeyedSnapshot(
            tuple(
                KeyedSnapshotEntry(
                    entry["key"],
                    entry["hash"],
                    entry["seen"],
                    None if entry["value"] is None else entry["value"].encode(),
                )
                for entry in spec["snapshot"]
            )
        )
    return PollCursorState(
        spec["token"],
        snapshot,
        spec["seededAtUnixMs"],
        spec["committedAtUnixMs"],
        spec["commitCount"],
        spec["resetCount"],
        spec["lastResetAtUnixMs"],
    )


class KeyRecordingStore(InMemoryObjectStore):
    """Records created keys and refuses reads of missing keys as S3 does."""

    def __init__(self) -> None:
        super().__init__()
        self.created_keys: list[str] = []
        self.missing_key_reads = 0

    async def create_object(self, key: str, contents: bytes) -> Object:
        self.created_keys.append(key)
        return await super().create_object(key, contents)

    async def read_object(self, key: str, version: str = "") -> Object:
        try:
            return await super().read_object(key, version)
        except ObjectNotFoundError:
            self.missing_key_reads += 1
            raise PermissionError("access denied") from None


@pytest.mark.parametrize("case", FIXTURE["keys"], ids=lambda case: case["key"])
def test_head_keys_match_go(case: dict[str, str]) -> None:
    key: PollCursorKey | PollFeedKey = (
        PollFeedKey(case["connectorId"], case["connectionName"], case["feedName"])
        if "feedName" in case
        else PollCursorKey(
            case["connectorId"], case["connectionName"], case["triggerName"], case["bindingName"]
        )
    )
    assert key.path(SCOPE) == case["key"]
    assert PollCursorStore(InMemoryObjectStore(), SCOPE, key).key == case["key"]


@pytest.mark.parametrize("case", FIXTURE["documents"], ids=lambda case: case["name"])
def test_documents_match_go_bytes(case: dict[str, Any]) -> None:
    state = state_from_fixture(case["state"])
    document = base64.b64decode(case["document"])
    assert encode_poll_cursor_document(state) == document
    assert encode_poll_cursor_document(decode_poll_cursor_document(document)) == document


@pytest.mark.parametrize("case", FIXTURE["decoding"], ids=lambda case: case["name"])
async def test_stored_documents_decode_as_go_decodes_them(case: dict[str, Any]) -> None:
    objects = InMemoryObjectStore()
    await objects.create_object(GOLDEN_HEAD_KEY, base64.b64decode(case["input"]))
    store = PollCursorStore(objects, SCOPE, KEY)
    if case["error"]:
        with pytest.raises(PollCursorDocumentInvalidError):
            await store.read_poll_cursor()
        assert (await objects.read_object(GOLDEN_HEAD_KEY)).contents == base64.b64decode(case["input"])
        return
    record = await store.read_poll_cursor()
    assert encode_poll_cursor_document(record.state) == base64.b64decode(case["reencoded"])


@pytest.mark.parametrize("case", FIXTURE["credentialCalls"], ids=lambda case: case["triggerName"])
def test_trigger_credential_call_matches_go(case: dict[str, str]) -> None:
    connection = ConnectionRef(case["provider"], case["connection"])
    trigger = TriggerRef(case["connectorId"], case["triggerName"])
    call = new_trigger_credential_call(connection, trigger)
    assert call.id == case["callId"]
    assert call.connection == connection
    assert call.operation == OperationRef(case["connectorId"], case["triggerName"])
    assert new_trigger_credential_call(connection, trigger) == call


async def test_store_creates_the_head_first_and_writes_golden_bytes() -> None:
    objects = KeyRecordingStore()
    store = PollCursorStore(objects, SCOPE, KEY)
    record = await store.read_poll_cursor()
    assert record.state is None
    assert objects.created_keys == [GOLDEN_HEAD_KEY]
    assert (await objects.read_object(GOLDEN_HEAD_KEY)).contents == UNSEEDED
    golden = next(case for case in FIXTURE["documents"] if case["name"] == "golden")
    written = await store.replace_poll_cursor(record.revision, state_from_fixture(golden["state"]))
    assert (await objects.read_object(GOLDEN_HEAD_KEY)).contents == base64.b64decode(golden["document"])
    reread = await store.read_poll_cursor()
    assert reread.revision == written.revision
    assert reread.state is not None and reread.state.token == "row <7> &  end"
    assert reread.state.snapshot is not None and reread.state.snapshot.entries[0].value == b'{"Name":"Ada"}'
    assert objects.missing_key_reads == 0
    feed = PollCursorStore(objects, SCOPE, PollFeedKey("sample", KEY.connection_name, "itemChanges"))
    await feed.read_poll_cursor()
    assert objects.created_keys[-1] == FIXTURE["keys"][3]["key"]
    assert PollCursorKey.from_trigger_inbox_key(TriggerInboxKey(*FIXTURE_KEY_PARTS)) == KEY


async def test_replicas_share_one_head_and_detect_conflicts() -> None:
    objects = InMemoryObjectStore()
    first = PollCursorStore(objects, SCOPE, KEY)
    second = PollCursorStore(objects, SCOPE, KEY)
    first_read = await first.read_poll_cursor()
    second_read = await second.read_poll_cursor()
    assert first_read.revision == second_read.revision
    await first.replace_poll_cursor(first_read.revision, PollCursorState("a", commit_count=1))
    with pytest.raises(ObjectConflictError):
        await second.replace_poll_cursor(second_read.revision, PollCursorState("b", commit_count=1))
    current = await second.read_poll_cursor()
    assert current.state is not None and current.state.token == "a"


class OutcomeUnknownStore(InMemoryObjectStore):
    """Applies or drops one compare-and-swap, then reports an unknown outcome."""

    def __init__(self, should_apply: bool) -> None:
        super().__init__()
        self.should_apply = should_apply

    async def compare_and_swap_object(self, key: str, expected_etag: str, contents: bytes) -> Object:
        if self.should_apply:
            await super().compare_and_swap_object(key, expected_etag, contents)
        raise OutcomeUnknownError("write outcome unknown")


async def test_unknown_write_outcomes_are_reconciled_by_bytes() -> None:
    applied = PollCursorStore(OutcomeUnknownStore(True), SCOPE, KEY)
    record = await applied.read_poll_cursor()
    written = await applied.replace_poll_cursor(record.revision, PollCursorState("x", commit_count=1))
    assert (await applied.read_poll_cursor()).revision == written.revision
    lost = PollCursorStore(OutcomeUnknownStore(False), SCOPE, KEY)
    record = await lost.read_poll_cursor()
    with pytest.raises(ObjectConflictError):
        await lost.replace_poll_cursor(record.revision, PollCursorState("x", commit_count=1))


def entries(*keys: str) -> KeyedSnapshot:
    return KeyedSnapshot(tuple(KeyedSnapshotEntry(key, "0123456789abcdef") for key in keys))


async def test_oversize_and_malformed_states_are_refused() -> None:
    store = new_memory_cursor_store()
    record = await store.read_poll_cursor()
    too_large = [
        PollCursorState("t" * (MAXIMUM_POLL_CURSOR_TOKEN_BYTES + 1)),
        PollCursorState("é" * (MAXIMUM_POLL_CURSOR_TOKEN_BYTES // 2 + 1)),
        PollCursorState(
            snapshot=entries(*(f"k{index:06}" for index in range(MAXIMUM_KEYED_SNAPSHOT_ENTRIES + 1)))
        ),
        PollCursorState(
            snapshot=KeyedSnapshot(
                (KeyedSnapshotEntry("a", "0123456789abcdef", 0, b'"' + b"v" * (9 << 20) + b'"'),)
            )
        ),
    ]
    for state in too_large:
        with pytest.raises(PollCursorTooLargeError):
            await store.replace_poll_cursor(record.revision, state)
    malformed = [
        PollCursorState(snapshot=entries("b", "a")),
        PollCursorState(snapshot=entries("a", "a")),
        PollCursorState(snapshot=entries("")),
        PollCursorState(snapshot=KeyedSnapshot((KeyedSnapshotEntry("a", "ABCDEF0123456789"),))),
        PollCursorState(snapshot=KeyedSnapshot((KeyedSnapshotEntry("a", "0123456789abcdef", 0, b"null"),))),
        PollCursorState(snapshot=KeyedSnapshot((KeyedSnapshotEntry("a", "0123456789abcdef", 0, b"{"),))),
        PollCursorState(snapshot=KeyedSnapshot((KeyedSnapshotEntry("a", "0123456789abcdef", -1),))),
        PollCursorState(commit_count=-1),
        PollCursorState(reset_count=-1),
    ]
    for state in malformed:
        with pytest.raises(ValueError) as raised:
            await store.replace_poll_cursor(record.revision, state)
        assert not isinstance(raised.value, PollCursorTooLargeError)
    written = await store.replace_poll_cursor(record.revision, PollCursorState(snapshot=KeyedSnapshot()))
    assert written.revision


def test_invalid_identities_are_refused() -> None:
    for key in [
        PollCursorKey("Sample", "main", "itemCreated", "main"),
        PollCursorKey("sample", " main", "itemCreated", "main"),
        PollCursorKey("sample", "main", "item-created", "main"),
        PollCursorKey("sample", "main", "itemCreated", "line\nbreak"),
        PollCursorKey("sample", "main", "itemCreated", "x" * 257),
    ]:
        with pytest.raises(ValueError):
            PollCursorStore(InMemoryObjectStore(), SCOPE, key)
    with pytest.raises(ValueError):
        PollCursorStore(InMemoryObjectStore(), SCOPE, PollFeedKey("sample", "main", "Feed"))
    with pytest.raises(ValueError):
        PollCursorStore(None, SCOPE, KEY)  # type: ignore[arg-type]
