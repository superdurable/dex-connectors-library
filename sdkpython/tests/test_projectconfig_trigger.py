# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Project Trigger inboxes, the durable target, local persistence, and relayed verification."""

import base64
import json
import os
import re
from collections.abc import Mapping
from dataclasses import dataclass, field
from datetime import UTC, datetime
from pathlib import Path
from typing import Any

import pytest

from dex_connectors.sdk import (
    TriggerCancellation,
    TriggerContext,
    TriggerDeliveryAbortedError,
    TriggerEvent,
    TriggerLogLevel,
    TriggerLogValue,
    mark_trigger_undeliverable,
)
from dex_connectors.sdk.projectconfig import (
    MAXIMUM_PENDING_TRIGGER_EVENTS,
    Configuration,
    InMemoryObjectStore,
    ObjectConflictError,
    ObjectNotFoundError,
    PendingTriggerEvent,
    Scope,
    TriggerInbox,
    TriggerInboxFullError,
    TriggerInboxKey,
    decode_trigger_configuration,
    decode_trigger_event,
    encode_trigger_event,
    new_durable_trigger_target,
    parse_trigger_payload_json,
)
from dex_connectors.sdk.projectconfig.dexweb_store import DexWebObjectStore
from dex_connectors.sdk.projectconfig.models import go_canonical_json_bytes
from dex_connectors.sdk.projectconfig.storage import (
    Object,
    OutcomeUnknownError,
    S3ObjectStore,
)


@dataclass(frozen=True, slots=True)
class RecordedLog:
    """One captured Trigger record."""

    level: TriggerLogLevel
    message: str
    attributes: dict[str, TriggerLogValue]


@dataclass(slots=True)
class RecordingTriggerLogger:
    """A logger that keeps every record for assertions."""

    records: list[RecordedLog] = field(default_factory=list)

    def is_enabled_for(self, level: TriggerLogLevel) -> bool:
        """Enable every level."""
        return True

    def log(self, level: TriggerLogLevel, message: str, attributes: Mapping[str, TriggerLogValue]) -> None:
        """Keep the record."""
        self.records.append(RecordedLog(level, message, dict(attributes)))

    def messages(self) -> list[str]:
        """Return the record messages in order."""
        return [record.message for record in self.records]


def text_event(event_id: str, text: str = "hello <world> & co") -> TriggerEvent[dict[str, Any]]:
    """Return the shared test event, as the TypeScript tests define it."""
    return TriggerEvent(event_id, datetime(2025, 10, 12, 0, 0, 0, 120000, tzinfo=UTC), {"text": text})


SCOPE = Scope("test", "live")
KEY = TriggerInboxKey("slack", "support", "threadReplyCreated", "reply")
FIXTURES = Path(__file__).parent / "fixtures"
GO_SCOPE = Scope("fixture", "live")


def _fixture(name: str) -> bytes:
    return (FIXTURES / name).read_bytes()


async def test_stores_events_under_the_go_key_and_document_once_per_id_in_order() -> None:
    objects = InMemoryObjectStore()
    inbox = TriggerInbox(objects, SCOPE, KEY)
    assert inbox.key == "projects/test/live/trigger-inboxes/slack/c3VwcG9ydA/threadReplyCreated/cmVwbHk/head"
    assert inbox.identity == KEY
    for event_id in ("Ev1", "Ev1", "Ev2"):
        await inbox.add(PendingTriggerEvent(event_id, encode_trigger_event(text_event(event_id))))
    assert [event.id for event in await inbox.pending()] == ["Ev1", "Ev2"]
    stored = (await objects.read_object(inbox.key)).contents.decode()
    assert stored.startswith(
        '{"schemaVersion":"connectors.dex.dev/project-trigger-inbox/v1alpha1","events":[{"id":"Ev1","event":'
        '{"eventId":"Ev1","occurredAt":"2025-10-12T00:00:00.12Z","payload":{"text":"hello \\u003cworld\\u003e'
        ' \\u0026 co"}}}'
    )
    await inbox.remove("Ev1")
    await inbox.remove("absent")
    assert [event.id for event in await inbox.pending()] == ["Ev2"]


def test_inbox_keys_match_conformance_and_go_identity_rules(conformance_dir: Path) -> None:
    storage = json.loads((conformance_dir / "project-storage.json").read_bytes())
    scope = Scope(storage["scope"]["projectId"], storage["scope"]["kind"])
    key = TriggerInboxKey("sample", storage["connectionKey"]["connectionName"], "itemCreated", "main / west")
    assert key.path(scope) == storage["triggerInboxHeadKey"]
    empty = json.loads(go_canonical_json_bytes(storage["emptyTriggerInboxDocument"]))
    assert empty == {"schemaVersion": "connectors.dex.dev/project-trigger-inbox/v1alpha1", "events": []}
    assert TriggerInboxKey("slack", "a" * 256, "itemCreated", "b").path(SCOPE)
    for invalid in (
        TriggerInboxKey("slack", "a" * 257, "itemCreated", "b"),
        TriggerInboxKey("slack", "é" * 129, "itemCreated", "b"),
        TriggerInboxKey("slack\n", "a", "itemCreated", "b"),
        TriggerInboxKey("slack", "a", "itemCreated\n", "b"),
        TriggerInboxKey("slack", " padded", "itemCreated", "b"),
        TriggerInboxKey("slack", "a", "itemCreated", "b "),
        TriggerInboxKey("slack", "a", "itemCreated", "b　"),
        TriggerInboxKey("slack", "line\nbreak", "itemCreated", "b"),
        TriggerInboxKey("slack", "", "itemCreated", "b"),
    ):
        with pytest.raises(ValueError):
            TriggerInbox(InMemoryObjectStore(), SCOPE, invalid)


async def test_empty_head_matches_the_go_conformance_document(conformance_dir: Path) -> None:
    objects = InMemoryObjectStore()
    inbox = TriggerInbox(objects, SCOPE, KEY)
    assert await inbox.pending() == ()
    storage = json.loads((conformance_dir / "project-storage.json").read_bytes())
    assert (await objects.read_object(inbox.key)).contents == go_canonical_json_bytes(
        storage["emptyTriggerInboxDocument"]
    )


async def test_bounds_the_backlog_and_size() -> None:
    inbox = TriggerInbox(InMemoryObjectStore(), SCOPE, KEY)
    for index in range(MAXIMUM_PENDING_TRIGGER_EVENTS):
        await inbox.add(PendingTriggerEvent(f"Ev{index}", b"{}"))
    with pytest.raises(TriggerInboxFullError):
        await inbox.add(PendingTriggerEvent("overflow", b"{}"))
    for invalid in (PendingTriggerEvent(" ", b"{}"), PendingTriggerEvent("Ev", b"{")):
        with pytest.raises(ValueError):
            await TriggerInbox(InMemoryObjectStore(), SCOPE, KEY).add(invalid)


class FlakyStore:
    """Delegates to memory; selected writes fail after or instead of applying."""

    def __init__(self) -> None:
        self.backing = InMemoryObjectStore()
        self.unknown_after_cas = 0
        self.conflicts_before_cas = 0
        self.is_cas_failing = False

    async def read_object(self, key: str, version: str = "") -> Object:
        return await self.backing.read_object(key, version)

    async def create_object(self, key: str, contents: bytes) -> Object:
        return await self.backing.create_object(key, contents)

    async def compare_and_swap_object(self, key: str, expected_etag: str, contents: bytes) -> Object:
        if self.is_cas_failing:
            raise OSError("storage unavailable")
        if self.conflicts_before_cas > 0:
            self.conflicts_before_cas -= 1
            current = await self.backing.read_object(key)
            await self.backing.compare_and_swap_object(key, current.etag, current.contents + b" ")
            raise ObjectConflictError("lost")
        result = await self.backing.compare_and_swap_object(key, expected_etag, contents)
        if self.unknown_after_cas > 0:
            self.unknown_after_cas -= 1
            raise OutcomeUnknownError("maybe")
        return result


async def test_retries_conditional_writes_after_conflicts_and_unknown_outcomes() -> None:
    store = FlakyStore()
    store.unknown_after_cas = 1
    inbox = TriggerInbox(store, SCOPE, KEY)
    await inbox.add(PendingTriggerEvent("EvA", b"{}"))
    assert [event.id for event in await inbox.pending()] == ["EvA"]
    store.conflicts_before_cas = 2
    await inbox.add(PendingTriggerEvent("EvB", b"{}"))
    assert [event.id for event in await inbox.pending()] == ["EvA", "EvB"]
    store.conflicts_before_cas = 9
    with pytest.raises(ObjectConflictError):
        await inbox.add(PendingTriggerEvent("EvC", b"{}"))


async def test_creates_the_head_before_the_first_read() -> None:
    class CreateFirstStore(InMemoryObjectStore):
        async def read_object(self, key: str, version: str = "") -> Object:
            self.reads = getattr(self, "reads", 0) + 1
            return await super().read_object(key, version)

    store = CreateFirstStore()
    inbox = TriggerInbox(store, SCOPE, KEY)
    assert await inbox.pending() == ()
    assert (await InMemoryObjectStore.read_object(store, inbox.key)).version == "1"


async def test_six_mebibyte_inbox_is_local_only_and_nine_mebibyte_events_are_full() -> None:
    objects = InMemoryObjectStore()
    inbox = TriggerInbox(objects, GO_SCOPE, KEY)
    large = "x" * (6 << 20)
    await inbox.add(
        PendingTriggerEvent(
            "EvLarge",
            f'{{"eventId":"EvLarge","occurredAt":"2025-10-12T00:00:00Z","payload":{{"text":"{large}"}}}}'.encode(),
        )
    )
    await inbox.add(
        PendingTriggerEvent(
            "EvSmall", b'{"eventId":"EvSmall","occurredAt":"2025-10-12T00:00:00Z","payload":{}}'
        )
    )
    assert len((await objects.read_object(inbox.key)).contents) > 6 << 20
    await inbox.remove("EvLarge")
    assert [event.id for event in await inbox.pending()] == ["EvSmall"]
    with pytest.raises(TriggerInboxFullError):
        await inbox.add(PendingTriggerEvent("EvHuge", f'{{"text":"{"y" * (9 << 20)}"}}'.encode()))
    with pytest.raises(ValueError, match="oversized"):
        await objects.create_object("projects/test/live/other/head", b"x" * (5 << 20))
    s3 = S3ObjectStore(object(), "bucket", allow_unencrypted=True)
    with pytest.raises(ValueError, match="oversized"):
        await s3.create_object(inbox.key, b"x" * (5 << 20))


async def _inbox_with(contents: bytes) -> tuple[InMemoryObjectStore, TriggerInbox]:
    objects = InMemoryObjectStore()
    inbox = TriggerInbox(objects, GO_SCOPE, KEY)
    assert (
        inbox.key == "projects/fixture/live/trigger-inboxes/slack/c3VwcG9ydA/threadReplyCreated/cmVwbHk/head"
    )
    await objects.create_object(inbox.key, contents)
    return objects, inbox


async def test_keeps_go_events_byte_for_byte_and_rewrites_as_go_does() -> None:
    objects, inbox = await _inbox_with(_fixture("go-trigger-inbox-three-events.json"))
    pending = await inbox.pending()
    assert [event.id for event in pending] == ["Ev1<&>", "Ev2", "Ev3"]
    assert (
        b'"alpha":9007199254740993,"list":[1.50,"x"],"nested":{"a":null,"b":true},"zeta":1'
        in pending[1].event
    )
    four = _fixture("go-trigger-inbox-four-events.json")
    _, four_inbox = await _inbox_with(four)
    await inbox.add((await four_inbox.pending())[3])
    assert (await objects.read_object(inbox.key)).contents == four
    await inbox.remove("Ev2")
    assert (await objects.read_object(inbox.key)).contents == _fixture("go-trigger-inbox-after-remove.json")


async def test_decodes_and_rewrites_every_go_decoding_case() -> None:
    cases = json.loads(_fixture("go-trigger-inbox-decoding-cases.json"))
    assert len(cases) > 8
    for case in cases:
        objects, inbox = await _inbox_with(base64.b64decode(case["input"]))
        if case["error"]:
            with pytest.raises(ValueError, match="trigger inbox document is invalid"):
                await inbox.pending()
            continue
        assert [event.id for event in await inbox.pending()] == case["ids"], case["name"]
        await inbox.add(PendingTriggerEvent("EvAdded", b'{"added":true}'))
        encoded = base64.b64encode((await objects.read_object(inbox.key)).contents).decode()
        assert encoded == case["output"], case["name"]


async def test_refuses_documents_go_refuses() -> None:
    schema = '"schemaVersion":"connectors.dex.dev/project-trigger-inbox/v1alpha1"'
    for invalid in (
        '{"schemaVersion":"other","events":[]}',
        "{" + schema + ',"events":[],"extra":1}',
        "{" + schema + ',"events":[{"id":"Ev1","event":{},"extra":1}]}',
        "{" + schema + ',"events":[]} {}',
    ):
        _, inbox = await _inbox_with(invalid.encode())
        with pytest.raises(ValueError, match="trigger inbox document is invalid"):
            await inbox.pending()
    _, empty = await _inbox_with(("{" + schema + ',"events":null}').encode())
    assert await empty.pending() == ()


async def test_replays_go_payloads_with_exact_integers_and_custom_decoders() -> None:
    _, inbox = await _inbox_with(_fixture("go-trigger-inbox-three-events.json"))
    delivered: list[TriggerEvent[Any]] = []

    class Recorder:
        async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[Any]) -> None:
            delivered.append(event)

    durable = new_durable_trigger_target(inbox, Recorder(), logger=RecordingTriggerLogger())
    await durable.replay_trigger_deliveries(TriggerContext())  # type: ignore[attr-defined]
    assert [event.id for event in delivered] == ["Ev1<&>", "Ev2", "Ev3"]
    assert delivered[0].occurred_at.isoformat() == "2025-10-12T00:00:00.123456+00:00"
    assert delivered[0].payload["text"] == '<b>approve</b> & ship   ünïcode "quoted"\n\b\f\u0001'
    assert delivered[2].payload == {"count": 9007199254740993, "label": "big"}
    assert await inbox.pending() == ()

    _, exact_inbox = await _inbox_with(_fixture("go-trigger-inbox-three-events.json"))
    texts: list[str] = []

    def keep_text(payload_json: str, payload_bytes: bytes) -> str:
        texts.append(payload_json)
        return payload_json

    await new_durable_trigger_target(
        exact_inbox, Recorder(), decode_payload=keep_text, logger=RecordingTriggerLogger()
    ).replay_trigger_deliveries(TriggerContext())  # type: ignore[attr-defined]
    assert texts[1:] == [
        '{"alpha":9007199254740993,"list":[1.50,"x"],"nested":{"a":null,"b":true},"zeta":1}',
        '{"count":9007199254740993,"label":"big"}',
    ]


@dataclass(frozen=True, slots=True)
class GeneratedItem:
    """Shaped like a generated connector model, without a hand-written to_wire."""

    item_id: str = field(metadata={"dex_json_name": "itemId"})


async def test_durable_targets_store_dataclass_payloads_and_apply_encode_payload() -> None:
    inbox = TriggerInbox(InMemoryObjectStore(), SCOPE, KEY)
    delivered: list[GeneratedItem] = []

    class ItemConsumer:
        async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[GeneratedItem]) -> None:
            delivered.append(event.payload)

    def decode_item(payload_json: str, payload_bytes: bytes) -> GeneratedItem:
        return GeneratedItem(json.loads(payload_json)["itemId"])

    durable: Any = new_durable_trigger_target(inbox, ItemConsumer(), decode_payload=decode_item)
    when = datetime(2025, 10, 12, tzinfo=UTC)
    await durable.prepare_trigger(TriggerContext(), TriggerEvent("Ev1", when, GeneratedItem("a")))
    assert (await inbox.pending())[0].event == (
        b'{"eventId":"Ev1","occurredAt":"2025-10-12T00:00:00Z","payload":{"itemId":"a"}}'
    )
    await durable.replay_trigger_deliveries(TriggerContext())
    assert delivered == [GeneratedItem("a")]
    wrapped: Any = new_durable_trigger_target(
        inbox,
        ItemConsumer(),
        decode_payload=decode_item,
        encode_payload=lambda item: {"itemId": item.item_id.upper()},
    )
    await wrapped.prepare_trigger(TriggerContext(), TriggerEvent("Ev2", when, GeneratedItem("b")))
    assert (await inbox.pending())[0].event.endswith(b'"payload":{"itemId":"B"}}')


async def test_decoder_receives_go_text_and_a_private_copy_of_the_bytes() -> None:
    inbox = TriggerInbox(InMemoryObjectStore(), SCOPE, KEY)
    event = b'{"eventId":"Ev1","occurredAt":"2025-10-12T00:00:00Z","payload":{"t":"a\xffb"}}'
    await inbox.add(PendingTriggerEvent("Ev1", event))
    received: list[tuple[str, bytes]] = []

    def capture(payload_json: str, payload_bytes: bytes) -> Any:
        received.append((payload_json, payload_bytes))
        return parse_trigger_payload_json(payload_bytes)

    decoded = decode_trigger_event((await inbox.pending())[0].event, capture)
    assert received == [('{"t":"a�b"}', b'{"t":"a\xffb"}')]
    assert decoded.payload == {"t": "a�b"}
    assert (
        decode_trigger_event(
            b'{"EVENTID":"x","occurredAt":"2025-10-12T00:00:00.123456789+02:00","extra":1}'
        ).id
        == "x"
    )
    for invalid in (b"[]", b'{"eventId":"x","occurredAt":"yesterday"}', b'{"eventId":"x"}'):
        with pytest.raises(ValueError):
            decode_trigger_event(invalid)


class ScriptedConsumer:
    """Records consumed events; listed IDs fail retryably, Ev3 is undeliverable."""

    def __init__(self, failing: set[str]) -> None:
        self.failing = failing
        self.delivered: list[str] = []

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[Any]) -> None:
        if event.id in self.failing:
            raise OSError("Dex unavailable")
        if event.id == "Ev3":
            raise mark_trigger_undeliverable(LookupError("Flow closed"))
        self.delivered.append(f"{event.id}:{event.occurred_at.isoformat()}:{event.payload['text']}")


async def test_durable_target_records_consumes_and_replays_after_restart() -> None:
    objects = InMemoryObjectStore()
    consumer = ScriptedConsumer({"Ev2"})
    logger = RecordingTriggerLogger()
    durable: Any = new_durable_trigger_target(TriggerInbox(objects, SCOPE, KEY), consumer, logger=logger)
    for event_id in ("Ev1", "Ev2", "Ev3"):
        await durable.prepare_trigger(TriggerContext(), text_event(event_id))
    await durable.handle_trigger(TriggerContext(), text_event("Ev1"))
    with pytest.raises(OSError, match="Dex unavailable"):
        await durable.handle_trigger(TriggerContext(), text_event("Ev2"))
    assert [event.id for event in await TriggerInbox(objects, SCOPE, KEY).pending()] == ["Ev2", "Ev3"]
    consumer.failing.clear()
    restarted: Any = new_durable_trigger_target(TriggerInbox(objects, SCOPE, KEY), consumer, logger=logger)
    await restarted.replay_trigger_deliveries(TriggerContext())
    assert consumer.delivered == [
        "Ev1:2025-10-12T00:00:00.120000+00:00:hello <world> & co",
        "Ev2:2025-10-12T00:00:00.120000+00:00:hello <world> & co",
    ]
    assert await TriggerInbox(objects, SCOPE, KEY).pending() == ()
    finished = [
        record for record in logger.records if record.message == "finished replaying pending trigger events"
    ]
    assert finished[-1].attributes == {
        "connector": "slack",
        "connection": "support",
        "trigger": "threadReplyCreated",
        "binding": "reply",
        "delivered": 1,
        "skipped": 1,
        "remaining": 0,
    }


async def test_durable_target_removes_without_reinvoking_after_a_failed_removal() -> None:
    store = FlakyStore()
    invocations: list[str] = []

    class Counter:
        async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[Any]) -> None:
            invocations.append(event.id)

    logger = RecordingTriggerLogger()
    durable: Any = new_durable_trigger_target(TriggerInbox(store, SCOPE, KEY), Counter(), logger=logger)
    await durable.prepare_trigger(TriggerContext(), text_event("Ev1"))
    store.is_cas_failing = True
    with pytest.raises(OSError, match="storage unavailable"):
        await durable.handle_trigger(TriggerContext(), text_event("Ev1"))
    assert "trigger inbox remove failed" in logger.messages()
    store.is_cas_failing = False
    await durable.handle_trigger(TriggerContext(), text_event("Ev1"))
    assert invocations == ["Ev1"]
    assert await TriggerInbox(store, SCOPE, KEY).pending() == ()


async def test_durable_prepare_refuses_writes_after_cancellation() -> None:
    inbox = TriggerInbox(InMemoryObjectStore(), SCOPE, KEY)
    durable: Any = new_durable_trigger_target(inbox, ScriptedConsumer(set()), logger=RecordingTriggerLogger())
    cancellation = TriggerCancellation()
    cancellation.cancel()
    with pytest.raises(TriggerDeliveryAbortedError):
        await durable.prepare_trigger(TriggerContext(cancellation), text_event("Ev1"))
    assert await inbox.pending() == ()


async def test_failed_decoder_keeps_the_stored_event_unchanged() -> None:
    inbox = TriggerInbox(InMemoryObjectStore(), SCOPE, KEY)
    await inbox.add(
        PendingTriggerEvent(
            "EvMutated", b'{"eventId":"EvMutated","occurredAt":"2025-10-12T00:00:00Z","payload":{"n":1}}'
        )
    )
    stored = (await inbox.pending())[0].event

    def failing_decoder(payload_json: str, payload_bytes: bytes) -> Any:
        raise ValueError("decoder failed")

    logger = RecordingTriggerLogger()
    durable: Any = new_durable_trigger_target(
        inbox, ScriptedConsumer(set()), decode_payload=failing_decoder, logger=logger
    )
    with pytest.raises(RuntimeError, match="decode pending Trigger event"):
        await durable.replay_trigger_deliveries(TriggerContext())
    assert (await inbox.pending())[0].event == stored
    assert "trigger inbox event decode failed" in logger.messages()


def test_decodes_binding_configuration_by_wire_identity() -> None:
    entry = {
        **{
            "connectorId": "slack",
            "connectionName": "support",
            "triggerName": "threadReplyCreated",
            "bindingName": "reply",
        },
        "configuration": {"channelId": "C1"},
    }
    configuration = Configuration(SCOPE, 1, (), trigger_bindings=(entry,))

    def strict(value: dict[str, Any]) -> str:
        if set(value) - {"channelId"}:
            raise ValueError("threadReplyCreated configuration has unknown fields")
        return str(value["channelId"])

    assert decode_trigger_configuration(configuration, KEY, strict) == "C1"
    other = TriggerInboxKey("slack", "support", "threadReplyCreated", "other")
    with pytest.raises(ObjectNotFoundError):
        decode_trigger_configuration(configuration, other, strict)
    unknown = Configuration(
        SCOPE, 1, (), trigger_bindings=({**entry, "configuration": {"channelId": "C1", "x": 1}},)
    )
    with pytest.raises(ValueError, match="unknown fields"):
        decode_trigger_configuration(unknown, KEY, strict)
    for invalid in (
        {k: v for k, v in entry.items() if k != "configuration"},
        {**entry, "configuration": [1]},
    ):
        with pytest.raises(ValueError, match="must be an object"):
            decode_trigger_configuration(
                Configuration(SCOPE, 1, (), trigger_bindings=(invalid,)), KEY, strict
            )


def _write_connections_file(directory: Path) -> Path:
    path = directory / "connections.json"
    path.write_text(
        json.dumps({"schemaVersion": "connectors.dex.dev/local-connections/v1alpha1", "connections": []})
    )
    path.chmod(0o600)
    return path


async def test_dex_web_inboxes_persist_beside_connections_in_the_go_file_layout(tmp_path: Path) -> None:
    path = _write_connections_file(tmp_path)
    local_scope = Scope("local", "preview")
    first = DexWebObjectStore(path, local_scope)
    await first.initialize()
    inbox = TriggerInbox(first, local_scope, KEY)
    await inbox.add(PendingTriggerEvent("Ev1", b'{"eventId":"Ev1"}'))
    await inbox.add(PendingTriggerEvent("Ev2", b'{"eventId":"Ev2"}'))
    directory = tmp_path / "trigger-inboxes"
    files = sorted(item.name for item in directory.iterdir())
    assert len(files) == 1 and re.fullmatch(r"[0-9a-f]{64}-3\.json", files[0])
    assert (directory.stat().st_mode & 0o777) == 0o700
    assert ((directory / files[0]).stat().st_mode & 0o777) == 0o600
    record = json.loads((directory / files[0]).read_bytes())
    assert list(record) == ["key", "version", "etag", "contents"]
    contents = base64.b64decode(record["contents"])
    assert record["etag"].startswith("sha256:") and record["etag"].endswith("-3")
    restarted = DexWebObjectStore(path, local_scope)
    await restarted.initialize()
    assert [event.id for event in await TriggerInbox(restarted, local_scope, KEY).pending()] == ["Ev1", "Ev2"]
    assert (await restarted.read_object(inbox.key)).contents == contents


async def test_dex_web_store_prunes_crash_leftovers_and_refuses_invalid_files(tmp_path: Path) -> None:
    path = _write_connections_file(tmp_path)
    local_scope = Scope("local", "preview")
    store = DexWebObjectStore(path, local_scope)
    await store.initialize()
    inbox = TriggerInbox(store, local_scope, KEY)
    await inbox.add(PendingTriggerEvent("Ev1", b"{}"))
    directory = tmp_path / "trigger-inboxes"
    (current,) = list(directory.iterdir())
    leftover = directory / current.name.replace("-2.json", "-1.json")
    leftover.write_bytes(
        go_canonical_json_bytes(
            {
                "key": inbox.key,
                "version": "1",
                "etag": "sha256:x-1",
                "contents": base64.b64encode(b"{}").decode(),
            }
        )
    )
    restarted = DexWebObjectStore(path, local_scope)
    await restarted.initialize()
    assert not leftover.exists()
    assert (await restarted.read_object(inbox.key)).version == "2"
    (directory / "bad.json").write_text('{"key":"k","version":"01","etag":"e","contents":""}')
    with pytest.raises(ValueError, match="local project storage file is invalid"):
        await DexWebObjectStore(path, local_scope).initialize()
    os.remove(directory / "bad.json")


def test_local_envelope_bytes_are_pinned() -> None:
    envelope = go_canonical_json_bytes(
        {
            "key": "projects/local/preview/trigger-inboxes/x/head",
            "version": "1",
            "etag": "sha256:ab-1",
            "contents": "e30=",
        }
    )
    assert envelope == (
        b'{"key":"projects/local/preview/trigger-inboxes/x/head","version":"1","etag":"sha256:ab-1","contents":"e30="}'
    )
