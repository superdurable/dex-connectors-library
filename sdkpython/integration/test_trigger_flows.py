# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real dexcli dev verification of Trigger delivery into Flows and RPCs.

The RPC request ID scenarios port the TypeScript suite's (sdkts 0.10.1). They adapt to the Dex Server:
0.1.37 and later answer a changed repeat with the first reply, and earlier servers report a conflict.
"""

import asyncio
import dataclasses
import json
import os
import subprocess
import sys
import tempfile
from collections.abc import AsyncIterator, Mapping
from dataclasses import dataclass, field
from datetime import UTC, datetime, timedelta
from pathlib import Path
from typing import Any
from uuid import uuid4

import dex
import httpx
import pytest

from dex_connectors.sdk import (
    ConnectionRef,
    StaticCredentialProvider,
    TriggerBindingRef,
    TriggerCancellation,
    TriggerConfig,
    TriggerContext,
    TriggerDefinition,
    TriggerDeliveryAbortedError,
    TriggerEvent,
    TriggerLogLevel,
    TriggerLogValue,
    TriggerRef,
    TriggerRPCRequestConflictError,
    TriggerSource,
    TriggerTarget,
    UndeliverableTriggerError,
    deliver_trigger,
    is_trigger_undeliverable,
    new_dex_flow_trigger_target,
    new_dex_rpc_trigger_target,
    new_trigger,
    prepare_trigger_delivery,
    trigger_rpc_request_id,
)
from dex_connectors.sdk.projectconfig import (
    InMemoryObjectStore,
    LoadedProject,
    Scope,
    TriggerInbox,
    TriggerInboxKey,
    load_from_environment,
    new_durable_trigger_target,
)
from dex_connectors.sdk.projectconfig.broker import CredentialBrokerClient
from dex_connectors.sdk.projectconfig.models import ApplicationSecretRef, CredentialBrokerConfiguration
from dex_connectors.sdk.relaysignature import (
    RELAY_SIGNATURE_HEADER,
    RELAY_TIMESTAMP_HEADER,
    sign_relayed_request,
)
from dex_connectors.sdk.webhooktrigger import (
    WebhookEndpoint,
    WebhookEndpointConfig,
    WebhookRequest,
    new_relayed_request_verification,
)

SERVER_ADDRESS = os.environ.get("DEX_SERVER_ADDRESS", "127.0.0.1:19881")
SUFFIX = uuid4().hex
CONNECTION = ConnectionRef("sample", "workspace")
DEFINITION = TriggerDefinition(TriggerRef("sample", "itemCreated"), "Receive a new item.")
KEY = TriggerInboxKey("sample", "workspace", "itemCreated", "start")
FIXTURES = Path(__file__).parents[1] / "tests" / "fixtures"


@dataclass(frozen=True)
class ItemInput:
    """The thread an item event belongs to."""

    channel: str
    thread: str


@dataclass(frozen=True)
class Reply:
    """One approval reply event routed to an RPC."""

    event_id: str
    kind: str


@dataclass(slots=True)
class RecordingLogger:
    """Keeps Trigger records for assertions."""

    records: list[tuple[TriggerLogLevel, str, dict[str, TriggerLogValue]]] = field(default_factory=list)

    def is_enabled_for(self, level: TriggerLogLevel) -> bool:
        """Enable every level."""
        return True

    def log(self, level: TriggerLogLevel, message: str, attributes: Mapping[str, TriggerLogValue]) -> None:
        """Keep one record."""
        self.records.append((level, message, dict(attributes)))


class StartItem(dex.Step[ItemInput]):
    """Completes the item Flow with its input."""

    def get_step_type(self) -> str:
        """Name the start Step."""
        return "StartItem"

    def execute(self, context: dex.Context, input: ItemInput) -> dex.StepDecision:
        """Finish immediately."""
        return dex.force_complete(input)


class ItemFlow(dex.Flow[ItemInput]):
    """One Flow per provider thread."""

    def get_flow_type(self) -> str:
        """Return a run-unique Flow type."""
        return f"SdkpythonTriggerItem{SUFFIX}"

    def get_steps(self) -> dex.StepList[ItemInput]:
        """Register the start Step."""
        return dex.StepList.start_step(StartItem())


class StartRawPayload(dex.Step[str]):
    """Completes with the exact payload JSON text it received."""

    def get_step_type(self) -> str:
        """Name the start Step."""
        return "StartRawPayload"

    def execute(self, context: dex.Context, input: str) -> dex.StepDecision:
        """Finish immediately."""
        return dex.force_complete(input)


class RawPayloadFlow(dex.Flow[str]):
    """Receives Go-written payloads as exact JSON text."""

    def get_flow_type(self) -> str:
        """Return a run-unique Flow type."""
        return f"SdkpythonTriggerRawPayload{SUFFIX}"

    def get_steps(self) -> dex.StepList[str]:
        """Register the start Step."""
        return dex.StepList.start_step(StartRawPayload())


class AwaitReply(dex.Step[None]):
    """Keeps the approval Flow open for RPCs."""

    def get_step_type(self) -> str:
        """Name the waiting Step."""
        return "AwaitReply"

    def execute(self, context: dex.Context, input: None) -> dex.StepDecision:
        """Wait for RPCs."""
        return dex.dead_end()


RECEIVED_REPLIES: dex.Attribute[list[str]] = dex.Attribute("receivedReplies", list[str])
HANDLER_CALLS: dict[str, int] = {"not_ready": 0}


class ApprovalFlow(dex.Flow[None]):
    """Receives reply events through an RPC that deduplicates by event ID in Flow state."""

    def get_flow_type(self) -> str:
        """Return a run-unique Flow type."""
        return f"SdkpythonTriggerApproval{SUFFIX}"

    def get_steps(self) -> dex.StepList[None]:
        """Register the waiting Step."""
        return dex.StepList.start_step(AwaitReply())

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the event ID record."""
        return dex.PersistenceSchema.of(RECEIVED_REPLIES)

    @dex.rpc(is_transactional=True)
    def receive_reply(self, context: dex.Context, reply: Reply) -> dex.RPCResult[bool]:
        """Record a reply once per event ID; reject a decided approval for good."""
        if reply.kind == "reject":
            raise UndeliverableTriggerError(ValueError("approval already decided"))
        if reply.kind == "notReady":
            HANDLER_CALLS["not_ready"] += 1
            if HANDLER_CALLS["not_ready"] == 1:
                raise RuntimeError("approval is still loading")
        received = RECEIVED_REPLIES.get(context) or []
        is_duplicate = reply.event_id in received
        if not is_duplicate:
            RECEIVED_REPLIES.set(context, [*received, reply.event_id])
        return dex.RPCResult(is_duplicate)

    @dex.rpc
    def list_replies(self, context: dex.Context) -> dex.RPCResult[list[str]]:
        """Return the recorded reply event IDs."""
        return dex.RPCResult(RECEIVED_REPLIES.get(context) or [])


@dataclass(frozen=True)
class LedgerEntry:
    """One amount an event adds to the ledger; `mapped_at` lets a mapper change the request."""

    amount: int
    mapped_at: int = 0


@dataclass
class LedgerGate:
    """Holds one `record_gated` run in flight until the test settles it."""

    entered: asyncio.Event = field(default_factory=asyncio.Event)
    settled: asyncio.Event = field(default_factory=asyncio.Event)
    is_failing: bool = False


LEDGER_TOTAL: dex.Attribute[int] = dex.Attribute("ledgerTotal", int)
# Counts handler runs per RPC; the Worker runs in this process.
LEDGER_HANDLER_CALLS: dict[str, int] = {
    "record_transactional": 0,
    "record_non_transactional": 0,
    "increment_total": 0,
    "record_wired": 0,
}
LEDGER_GATES: list[LedgerGate] = []


class AwaitLedgerEntries(dex.Step[None]):
    """Keeps the ledger Flow open for RPCs."""

    def get_step_type(self) -> str:
        """Name the waiting Step."""
        return "AwaitLedgerEntries"

    def execute(self, context: dex.Context, input: None) -> dex.StepDecision:
        """Wait for RPCs."""
        return dex.dead_end()


class LedgerFlow(dex.Flow[None]):
    """Adds event amounts to a total through RPCs that rely on the Trigger request ID alone."""

    def get_flow_type(self) -> str:
        """Return a run-unique Flow type."""
        return f"SdkpythonTriggerLedger{SUFFIX}"

    def get_steps(self) -> dex.StepList[None]:
        """Register the waiting Step."""
        return dex.StepList.start_step(AwaitLedgerEntries())

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the total."""
        return dex.PersistenceSchema.of(LEDGER_TOTAL)

    @dex.rpc(is_transactional=True)
    def record_transactional(self, context: dex.Context, entry: LedgerEntry) -> dex.RPCResult[int]:
        """Add one entry; a repeat is answered from the recorded reply."""
        LEDGER_HANDLER_CALLS["record_transactional"] += 1
        return _add_to_ledger(context, entry.amount)

    @dex.rpc
    def record_non_transactional(self, context: dex.Context, entry: LedgerEntry) -> dex.RPCResult[int]:
        """Add one entry; a repeat runs again and Dex discards its changes."""
        LEDGER_HANDLER_CALLS["record_non_transactional"] += 1
        return _add_to_ledger(context, entry.amount)

    @dex.rpc(is_transactional=True)
    def increment_total(self, context: dex.Context) -> dex.RPCResult[int]:
        """Add one without input."""
        LEDGER_HANDLER_CALLS["increment_total"] += 1
        return _add_to_ledger(context, 1)

    @dex.rpc(name="record_ledger_entry", is_transactional=True)
    def record_wired(self, context: dex.Context, entry: LedgerEntry) -> dex.RPCResult[int]:
        """Add one entry under a wire name that differs from the method name."""
        LEDGER_HANDLER_CALLS["record_wired"] += 1
        return _add_to_ledger(context, entry.amount)

    @dex.rpc(is_transactional=True, timeout=timedelta(seconds=30))
    async def record_gated(self, context: dex.Context, entry: LedgerEntry) -> dex.RPCResult[int]:
        """Add one entry once the queued gate, if any, settles."""
        if LEDGER_GATES:
            gate = LEDGER_GATES.pop(0)
            gate.entered.set()
            await gate.settled.wait()
            if gate.is_failing:
                raise RuntimeError("earlier attempt failed")
        return _add_to_ledger(context, entry.amount)

    @dex.rpc
    def get_total(self, context: dex.Context) -> dex.RPCResult[int]:
        """Return the total."""
        return dex.RPCResult(LEDGER_TOTAL.get(context) or 0)


def _add_to_ledger(context: dex.Context, amount: int) -> dex.RPCResult[int]:
    total = (LEDGER_TOTAL.get(context) or 0) + amount
    LEDGER_TOTAL.set(context, total)
    return dex.RPCResult(total)


ITEM_FLOW = ItemFlow()
RAW_FLOW = RawPayloadFlow()
APPROVAL_FLOW = ApprovalFlow()
LEDGER_FLOW = LedgerFlow()


@dataclass(frozen=True)
class DexStack:
    """A running Worker and Client for this module's Flows."""

    client: dex.AsyncClient


@pytest.fixture
async def stack() -> AsyncIterator[DexStack]:
    """Run a real AsyncWorker and AsyncClient against the local Dex stack."""
    cache_root = Path(__file__).parents[1] / ".dexcli-data"
    cache_root.mkdir(exist_ok=True)
    with tempfile.TemporaryDirectory(dir=cache_root) as directory:
        cache = dex.open_blob_cache(dex.BlobCacheConfig(directory, 16 * 1024 * 1024))
        registry = dex.Registry([ITEM_FLOW, RAW_FLOW, APPROVAL_FLOW, LEDGER_FLOW], allow_async_handlers=True)
        async with dex.AsyncWorker(
            registry, cache, dex.WorkerOptions(server_address=SERVER_ADDRESS)
        ) as worker:
            worker_task = asyncio.create_task(worker.start())
            try:
                await worker.wait_pull_registered(timeout=30)
                async with dex.AsyncClient(
                    registry, cache, dex.ClientOptions(server_address=SERVER_ADDRESS)
                ) as client:
                    yield DexStack(client)
            finally:
                await worker.stop()
                await worker_task
                cache.close()


def _event(event_id: str, payload: Any) -> TriggerEvent[Any]:
    return TriggerEvent(event_id, datetime.now(UTC), payload)


def _item_target(client: dex.AsyncClient, logger: RecordingLogger | None = None) -> TriggerTarget[ItemInput]:
    return new_dex_flow_trigger_target(
        client,
        ITEM_FLOW,
        accepts_event=lambda event: event.payload.channel == "C1",
        resolve_flow_id=lambda event: f"item-{SUFFIX}-{event.payload.thread}",
        map_to_flow_input=lambda event: event.payload,
        logger=logger or RecordingLogger(),
    )


async def _wait_until_started(client: dex.AsyncClient, flow_id: str) -> None:
    async with asyncio.timeout(30):
        while True:
            try:
                await client.describe_flow(flow_id)
                return
            except dex.FlowNotFoundError:
                await asyncio.sleep(0.05)


async def test_flow_target_starts_one_run_per_flow_id(stack: DexStack) -> None:
    client = stack.client
    target = _item_target(client)
    first = _event(f"Ev-{SUFFIX}-1", ItemInput("C1", "1.0"))
    await deliver_trigger(TriggerContext(), target, first)
    flow_id = f"item-{SUFFIX}-1.0"
    started_run = (await client.describe_flow(flow_id)).run_id
    await deliver_trigger(TriggerContext(), target, first)
    logger = RecordingLogger()
    await deliver_trigger(
        TriggerContext(), _item_target(client, logger), _event(f"Ev-{SUFFIX}-other", ItemInput("C1", "1.0"))
    )
    assert logger.records[-1][2]["duplicate"] is True
    completed = await asyncio.wait_for(client.wait_for_flow(flow_id, timedelta(seconds=60)), 70)
    assert completed.status == dex.FlowStatus.COMPLETED
    assert (await client.describe_flow(flow_id)).run_id == started_run
    await target.handle_trigger(TriggerContext(), _event(f"Ev-{SUFFIX}-filtered", ItemInput("C9", "2.0")))
    with pytest.raises(dex.FlowNotFoundError):
        await client.describe_flow(f"item-{SUFFIX}-2.0")


async def test_rpc_target_routes_replies_and_classifies_handler_failures(
    stack: DexStack, monkeypatch: pytest.MonkeyPatch
) -> None:
    from dex_connectors.sdk import trigger_delivery

    async def short_wait(delay: timedelta) -> None:
        await asyncio.sleep(0.01)

    monkeypatch.setattr(trigger_delivery, "_sleep", short_wait)
    client = stack.client
    approval_id = f"approval-{SUFFIX}"
    await client.start_flow(APPROVAL_FLOW, approval_id, None)
    reply_target: TriggerTarget[dict[str, str]] = new_dex_rpc_trigger_target(
        client,
        APPROVAL_FLOW.receive_reply,
        accepts_event=lambda event: True,
        resolve_flow_id=lambda event: event.payload["flow_id"],
        map_to_rpc_input=lambda event: Reply(event.id, event.payload["kind"]),
        logger=RecordingLogger(),
    )
    reply = _event(f"Ev-{SUFFIX}-reply", {"flow_id": approval_id, "kind": "approve"})
    await reply_target.handle_trigger(TriggerContext(), reply)
    # The redelivery repeats the request ID, so Dex answers it from the recorded reply.
    await reply_target.handle_trigger(TriggerContext(), reply)
    assert await client.invoke_rpc(APPROVAL_FLOW.list_replies, approval_id) == [reply.id]

    rejected = _event(f"Ev-{SUFFIX}-rejected", {"flow_id": approval_id, "kind": "reject"})
    with pytest.raises(UndeliverableTriggerError) as raised:
        await reply_target.handle_trigger(TriggerContext(), rejected)
    assert isinstance(raised.value.__cause__, dex.WorkerInvocationError)
    assert raised.value.__cause__.worker_error_type == "ConnectorsSDK.UndeliverableTriggerError"
    logger = RecordingLogger()
    await deliver_trigger(TriggerContext(), reply_target, rejected, logger=logger)
    assert [(record[1], record[2]) for record in logger.records] == [
        (
            "trigger event skipped: undeliverable",
            {
                "event_id": rejected.id,
                "error_type": "UndeliverableTriggerError",
                "error_cause_type": "WorkerInvocationError",
            },
        )
    ]

    not_ready = _event(f"Ev-{SUFFIX}-early", {"flow_id": approval_id, "kind": "notReady"})
    retry_logger = RecordingLogger()
    await deliver_trigger(TriggerContext(), reply_target, not_ready, logger=retry_logger)
    assert [record[1] for record in retry_logger.records] == [
        "trigger delivery failed; retrying",
        "trigger delivered after retry",
    ]
    assert HANDLER_CALLS["not_ready"] == 2

    orphan = _event(f"Ev-{SUFFIX}-orphan", {"flow_id": f"missing-{SUFFIX}", "kind": "approve"})
    with pytest.raises(Exception) as missing:
        await reply_target.handle_trigger(TriggerContext(), orphan)
    assert is_trigger_undeliverable(missing.value)
    await client.stop_flow(approval_id)


def _ledger_target(
    client: dex.AsyncClient,
    rpc_method: Any,
    ledger_id: str,
    logger: RecordingLogger | None = None,
    rpc_name: str | None = None,
    map_to_entry: Any = None,
) -> TriggerTarget[dict[str, int]]:
    return new_dex_rpc_trigger_target(
        client,
        rpc_method,
        accepts_event=lambda event: True,
        resolve_flow_id=lambda event: ledger_id,
        map_to_rpc_input=map_to_entry or (lambda event: LedgerEntry(event.payload["amount"])),
        rpc_name=rpc_name,
        logger=logger or RecordingLogger(),
    )


async def test_rpc_target_sends_one_request_id_for_every_delivery_of_an_event(stack: DexStack) -> None:
    client = stack.client
    ledger_id = f"ledger-{SUFFIX}"
    await client.start_flow(LEDGER_FLOW, ledger_id, None)
    quiet = RecordingLogger()

    transactional_target = _ledger_target(client, LEDGER_FLOW.record_transactional, ledger_id)
    payment = _event(f"Ev-{SUFFIX}-payment", {"amount": 5})
    await deliver_trigger(TriggerContext(), transactional_target, payment, logger=quiet)
    await deliver_trigger(TriggerContext(), transactional_target, payment, logger=quiet)
    assert LEDGER_HANDLER_CALLS["record_transactional"] == 1, "a transactional repeat gets the recorded reply"
    assert await client.invoke_rpc(LEDGER_FLOW.get_total, ledger_id) == 5

    # A mapper whose input changes on every call applies its first delivery once. Its redelivery maps the
    # event to a different request under the one request ID: Dex Server 0.1.37 and later answer it with the
    # first reply, and earlier servers refuse it, which the target logs at ERROR before it drops the event.
    # Either way the durable inbox consumes the redelivery and the event applies once.
    bonus = _event(f"Ev-{SUFFIX}-bonus", {"amount": 3})
    mappings = iter(range(1, 100))
    conflict_logger = RecordingLogger()
    changing_target = _ledger_target(
        client,
        LEDGER_FLOW.record_transactional,
        ledger_id,
        conflict_logger,
        map_to_entry=lambda event: LedgerEntry(event.payload["amount"], next(mappings)),
    )
    bonus_inbox = TriggerInbox(
        InMemoryObjectStore(),
        Scope("bonus", "live"),
        TriggerInboxKey("sample", "workspace", "bonusCreated", "bonus"),
    )
    durable_changing_target = new_durable_trigger_target(
        bonus_inbox, changing_target, decode_payload=lambda text, raw: json.loads(text), logger=quiet
    )
    for _ in range(2):
        await prepare_trigger_delivery(TriggerContext(), durable_changing_target, bonus)
        await durable_changing_target.handle_trigger(TriggerContext(), bonus)
        assert await bonus_inbox.pending() == ()
    assert LEDGER_HANDLER_CALLS["record_transactional"] == 2, "only the first changing delivery ran"
    assert await client.invoke_rpc(LEDGER_FLOW.get_total, ledger_id) == 8
    bonus_request_id = trigger_rpc_request_id("record_transactional", bonus.id)
    drops = [
        record
        for record in conflict_logger.records
        if record[1] == "trigger rpc request conflict; event dropped"
    ]
    # Only a server before 0.1.37 compares the whole request and reports the conflict.
    is_request_conflict_reported = bool(drops)
    try:
        await changing_target.handle_trigger(TriggerContext(), bonus)
        bonus_redelivery: BaseException | None = None
    except Exception as error:
        bonus_redelivery = error
    if is_request_conflict_reported:
        assert drops == [
            (
                TriggerLogLevel.ERROR,
                "trigger rpc request conflict; event dropped",
                {
                    "target": "rpc",
                    "event_id": bonus.id,
                    "request_id": bonus_request_id,
                    "rpc_name": "record_transactional",
                    "flow_id": ledger_id,
                },
            )
        ]
        assert isinstance(bonus_redelivery, TriggerRPCRequestConflictError)
        assert is_trigger_undeliverable(bonus_redelivery)
        assert isinstance(bonus_redelivery.__cause__, dex.DexServiceError)
        assert "was already used for a different request" in bonus_redelivery.__cause__.detail
    else:
        assert bonus_redelivery is None, "a changed redelivery is answered with the first reply"
    assert LEDGER_HANDLER_CALLS["record_transactional"] == 2
    assert await client.invoke_rpc(LEDGER_FLOW.get_total, ledger_id) == 8

    # An input-free RPC without a mapper also applies a redelivery once.
    tick = _event(f"Ev-{SUFFIX}-tick", {})
    input_free_target: TriggerTarget[dict[str, int]] = new_dex_rpc_trigger_target(
        client,
        LEDGER_FLOW.increment_total,
        accepts_event=lambda event: True,
        resolve_flow_id=lambda event: ledger_id,
        logger=quiet,
    )
    await deliver_trigger(TriggerContext(), input_free_target, tick, logger=quiet)
    await deliver_trigger(TriggerContext(), input_free_target, tick, logger=quiet)
    assert LEDGER_HANDLER_CALLS["increment_total"] == 1
    assert await client.invoke_rpc(LEDGER_FLOW.get_total, ledger_id) == 9

    # rpc_name replaces the method name in the request ID, so the wire name keeps the IDs stable.
    wired = _event(f"Ev-{SUFFIX}-wired", {"amount": 4})
    wired_target = _ledger_target(client, LEDGER_FLOW.record_wired, ledger_id, rpc_name="record_ledger_entry")
    await deliver_trigger(TriggerContext(), wired_target, wired, logger=quiet)
    wired_options = dex.RPCInvokeOptions(request_id=trigger_rpc_request_id("record_ledger_entry", wired.id))
    assert (
        await client.invoke_rpc(LEDGER_FLOW.record_wired, ledger_id, LedgerEntry(4), options=wired_options)
        == 13
    )
    assert LEDGER_HANDLER_CALLS["record_wired"] == 1, "the wire-name request ID matched the target delivery"

    # The same event to another RPC on the same Flow is a different request.
    non_transactional_target = _ledger_target(client, LEDGER_FLOW.record_non_transactional, ledger_id)
    await deliver_trigger(TriggerContext(), non_transactional_target, payment, logger=quiet)
    await deliver_trigger(TriggerContext(), non_transactional_target, payment, logger=quiet)
    assert LEDGER_HANDLER_CALLS["record_non_transactional"] == 2, "a non-transactional repeat reruns"
    assert await client.invoke_rpc(LEDGER_FLOW.get_total, ledger_id) == 18, "each RPC applied the event once"

    # An earlier attempt with another input, such as one a crashed process sent, is still in flight. A server
    # before 0.1.37 reports the same conflict, so the target drops the event, and when that attempt then fails
    # without committing, this delivery is lost. A later server reports no conflict, so this delivery is never
    # dropped: it either fails for a retry or applies the event.
    deposit = _event(f"Ev-{SUFFIX}-deposit", {"amount": 7})
    gate = LedgerGate(is_failing=True)
    LEDGER_GATES.append(gate)
    deposit_options = dex.RPCInvokeOptions(request_id=trigger_rpc_request_id("record_gated", deposit.id))
    # The AsyncClient overloads do not type an async handler.
    record_gated: Any = LEDGER_FLOW.record_gated
    earlier_attempt: asyncio.Task[int] = asyncio.create_task(
        client.invoke_rpc(record_gated, ledger_id, LedgerEntry(100), options=deposit_options)
    )
    async with asyncio.timeout(30):
        await gate.entered.wait()
    gated_target = _ledger_target(client, LEDGER_FLOW.record_gated, ledger_id)
    in_flight_delivery = asyncio.create_task(gated_target.handle_trigger(TriggerContext(), deposit))
    if is_request_conflict_reported:
        with pytest.raises(TriggerRPCRequestConflictError) as in_flight_rejection:
            await in_flight_delivery
        assert is_trigger_undeliverable(in_flight_rejection.value)
        gate.settled.set()
        with pytest.raises(dex.WorkerInvocationError):
            await earlier_attempt
        assert await client.invoke_rpc(LEDGER_FLOW.get_total, ledger_id) == 18, "the dropped deposit was lost"
    else:
        gate.settled.set()
        with pytest.raises(dex.WorkerInvocationError):
            await earlier_attempt
        try:
            await in_flight_delivery
        except Exception as error:
            assert not is_trigger_undeliverable(error), f"an in-flight repeat is never dropped: {error!r}"
    # A later redelivery of the same event, such as a provider retry, applies the deposit exactly once.
    await deliver_trigger(TriggerContext(), gated_target, deposit, logger=quiet)
    assert await client.invoke_rpc(LEDGER_FLOW.get_total, ledger_id) == 25
    await client.stop_flow(ledger_id)


class NoEventSource:
    """A source that ends at once, so a runner only replays its inbox."""

    async def run(self, context: TriggerContext, target: TriggerTarget[Any]) -> None:
        return None


def _write_connections_file(directory: Path) -> Path:
    path = directory / "connections.json"
    path.write_text(
        json.dumps({"schemaVersion": "connectors.dex.dev/local-connections/v1alpha1", "connections": []})
    )
    path.chmod(0o600)
    return path


async def test_a_restarted_process_replays_its_local_inbox_into_dex(stack: DexStack, tmp_path: Path) -> None:
    client = stack.client
    configuration_file = _write_connections_file(tmp_path)
    pending_id = f"Ev-{SUFFIX}-restart"
    subprocess.run(
        [
            sys.executable,
            str(Path(__file__).with_name("trigger_restart_child.py")),
            str(configuration_file),
            pending_id,
            f"restart-{SUFFIX}",
        ],
        check=True,
        timeout=60,
    )
    assert len(list((tmp_path / "trigger-inboxes").iterdir())) == 1
    project = await load_from_environment({"DEX_CONNECTOR_CONFIG_FILE": str(configuration_file)})
    assert [event.id for event in await project.trigger_inbox(KEY).pending()] == [pending_id]
    source: TriggerSource[ItemInput] = NoEventSource()
    restarted = new_trigger(
        TriggerConfig(
            DEFINITION,
            TriggerBindingRef(CONNECTION, DEFINITION.trigger, "start"),
            source,
            new_durable_trigger_target(
                project.trigger_inbox(KEY),
                _item_target(client),
                decode_payload=lambda text, raw: ItemInput(**json.loads(text)),
                logger=RecordingLogger(),
            ),
        )
    )
    await restarted.run()
    result = await asyncio.wait_for(
        client.wait_for_flow(f"item-{SUFFIX}-restart-{SUFFIX}", timedelta(seconds=60)), 70
    )
    assert result.status == dex.FlowStatus.COMPLETED
    assert await project.trigger_inbox(KEY).pending() == ()


def _relay_broker(scope_token: str) -> CredentialBrokerClient:
    reference = ApplicationSecretRef(
        f"projects/local/preview/app-secrets/DEX_CREDENTIAL_BROKER_TOKEN/{'a' * 32}",
        "v1",
        "sha256:" + "b" * 64,
    )

    async def read_token(token_reference: ApplicationSecretRef) -> str:
        return scope_token

    async def current_reference() -> ApplicationSecretRef:
        return reference

    # Verification reads only the scope token, so the broker is never called over HTTP.
    transport = httpx.MockTransport(lambda request: httpx.Response(500))
    return CredentialBrokerClient(
        CredentialBrokerConfiguration(
            "https://broker.example.test/internal/connection-credentials/read", reference
        ),
        read_token,
        current_reference,
        httpx.AsyncClient(transport=transport),
    )


def _decode_relayed_item(request: WebhookRequest) -> TriggerEvent[ItemInput] | None:
    body = json.loads(request.body)
    return TriggerEvent(body["eventId"], request.received_at, ItemInput(body["channel"], body["thread"]))


async def _asgi_post(application: Any, body: bytes, headers: list[tuple[str, str]]) -> int:
    messages: list[Any] = [{"type": "http.request", "body": body, "more_body": False}]
    sent: list[Any] = []
    disconnected = asyncio.Event()

    async def receive() -> Any:
        if messages:
            return messages.pop(0)
        await disconnected.wait()
        return {"type": "http.disconnect"}

    async def send(message: Any) -> None:
        sent.append(message)

    scope = {
        "type": "http",
        "method": "POST",
        "path": "/triggers/sample",
        "raw_path": b"/triggers/sample",
        "query_string": b"",
        "headers": [(name.lower().encode(), value.encode()) for name, value in headers],
    }
    await application(scope, receive, send)
    return int(next(message["status"] for message in sent if message["type"] == "http.response.start"))


async def test_a_relayed_webhook_is_verified_recorded_and_delivered(stack: DexStack, tmp_path: Path) -> None:
    client = stack.client
    scope_token = f"scope-token-{SUFFIX}"
    loaded = await load_from_environment(
        {"DEX_CONNECTOR_CONFIG_FILE": str(_write_connections_file(tmp_path))}
    )
    project: LoadedProject = dataclasses.replace(loaded, broker=_relay_broker(scope_token))
    endpoint: WebhookEndpoint[dict[str, str], ItemInput] = WebhookEndpoint(
        WebhookEndpointConfig(
            connector_id="sample",
            trigger_name="itemCreated",
            connection=CONNECTION,
            credentials=StaticCredentialProvider({CONNECTION: {}}),
            verify_request=new_relayed_request_verification(project),
            decode_event=_decode_relayed_item,
            maximum_body_bytes=1 << 20,
            logger=RecordingLogger(),
        )
    )
    relay_key = dataclasses.replace(KEY, binding_name="relayed")
    runner = new_trigger(
        TriggerConfig(
            DEFINITION,
            TriggerBindingRef(CONNECTION, DEFINITION.trigger, "relayed"),
            endpoint.new_source(),
            new_durable_trigger_target(
                project.trigger_inbox(relay_key),
                _item_target(client),
                decode_payload=lambda text, raw: ItemInput(**json.loads(text)),
                logger=RecordingLogger(),
            ),
        )
    )
    cancellation = TriggerCancellation()
    running = asyncio.create_task(runner.run(TriggerContext(cancellation)))
    try:
        async with asyncio.timeout(10):
            while endpoint.running_source_count() == 0:
                await asyncio.sleep(0.01)

        async def post(token: str, thread: str) -> int:
            body = json.dumps(
                {"eventId": f"Ev-{SUFFIX}-{thread}", "channel": "C1", "thread": thread}
            ).encode()
            signed_at = datetime.now(UTC)
            return await _asgi_post(
                endpoint,
                body,
                [
                    (RELAY_TIMESTAMP_HEADER, str(int(signed_at.timestamp()))),
                    (RELAY_SIGNATURE_HEADER, sign_relayed_request(token, signed_at, body)),
                ],
            )

        assert await post("forged-token", f"forged-{SUFFIX}") == 400
        assert await post(scope_token, f"relayed-{SUFFIX}") == 200
        relayed_flow = f"item-{SUFFIX}-relayed-{SUFFIX}"
        await _wait_until_started(client, relayed_flow)
        result = await asyncio.wait_for(client.wait_for_flow(relayed_flow, timedelta(seconds=60)), 70)
        assert result.status == dex.FlowStatus.COMPLETED
        with pytest.raises(dex.FlowNotFoundError):
            await client.describe_flow(f"item-{SUFFIX}-forged-{SUFFIX}")
        async with asyncio.timeout(10):
            while await project.trigger_inbox(relay_key).pending():
                await asyncio.sleep(0.05)
    finally:
        cancellation.cancel()
        with pytest.raises(TriggerDeliveryAbortedError):
            await running


async def test_a_go_written_inbox_replays_into_dex_in_order(stack: DexStack) -> None:
    client = stack.client
    go_key = TriggerInboxKey("slack", "support", "threadReplyCreated", "reply")
    objects = InMemoryObjectStore()
    inbox = TriggerInbox(objects, Scope("fixture", "live"), go_key)
    await objects.create_object(inbox.key, (FIXTURES / "go-trigger-inbox-three-events.json").read_bytes())

    def go_flow_id(event_id: str) -> str:
        return f"go-inbox-{SUFFIX}-{''.join(character for character in event_id if character.isalnum())}"

    started: list[str] = []

    def resolve_go_flow_id(event: TriggerEvent[str]) -> str:
        started.append(event.id)
        return go_flow_id(event.id)

    target: TriggerTarget[str] = new_dex_flow_trigger_target(
        client,
        RAW_FLOW,
        accepts_event=lambda event: True,
        resolve_flow_id=resolve_go_flow_id,
        map_to_flow_input=lambda event: event.payload,
        logger=RecordingLogger(),
    )
    replay: Any = new_durable_trigger_target(
        inbox, target, decode_payload=lambda text, raw: text, logger=RecordingLogger()
    )
    await replay.replay_trigger_deliveries(TriggerContext())
    assert started == ["Ev1<&>", "Ev2", "Ev3"]
    outputs = []
    for event_id in started:
        result = await asyncio.wait_for(client.wait_for_flow(go_flow_id(event_id), timedelta(seconds=60)), 70)
        assert result.status == dex.FlowStatus.COMPLETED
        outputs.append(result.single_output(str))
    assert outputs[2] == '{"count":9007199254740993,"label":"big"}'
    assert json.loads(outputs[2])["count"] == 9007199254740993
    assert await inbox.pending() == ()
