# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real dexcli dev Flow verification of async connector Steps."""

import asyncio
import os
import tempfile
from collections.abc import Awaitable, Callable
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import pytest

from dex_connectors.sdk import (
    BranchDefinition,
    Call,
    CallID,
    ConnectionRef,
    Failure,
    FailureKind,
    IdempotencyKey,
    MutationAttempt,
    MutationDefinition,
    MutationResult,
    MutationStep,
    MutationStepConfig,
    OperationRef,
    Progress,
    ProgressUpdate,
    QueryAttempt,
    QueryDefinition,
    QueryResult,
    QueryStep,
    QueryStepConfig,
    Receipt,
    StepAnnotations,
    StepDefaults,
)
from dex_connectors.sdk.connectormock import Case
from dex_connectors.sdk.connectormock import Mutation as MockMutation
from dex_connectors.sdk.singledispatch import DecisionKind, Fence

SERVER_ADDRESS = os.environ.get("DEX_SERVER_ADDRESS", "127.0.0.1:19881")
OPERATION = OperationRef("sample", "getItem")
CONNECTION = ConnectionRef("sample", "main")
BRANCHES = (
    BranchDefinition("success", "Found"),
    BranchDefinition("alternate", "Different outcome", True),
    BranchDefinition("defect", "Local defect", True),
)


class ScriptedQuery:
    """Classify one inert response per real Dex Execute attempt."""

    def __init__(self, outcomes: list[str], *, emit_progress: bool = False) -> None:
        self.outcomes = outcomes
        self.calls = 0
        self.emit_progress = emit_progress

    def definition(self) -> QueryDefinition:
        """Declare connector branches and async Execute defaults."""
        return QueryDefinition(OPERATION, BRANCHES, StepDefaults(timedelta(seconds=10), 2))

    async def invoke(self, call: Call, input: str) -> QueryAttempt[str]:
        """Return the next scripted provider classification."""
        outcome = self.outcomes[min(self.calls, len(self.outcomes) - 1)]
        self.calls += 1
        if self.emit_progress:
            assert call.has_progress_stream() and call.has_text_stream()
            call.report_progress(Progress("request", "Fetching an item", 0.5, 7))
            call.write_text("response ")
            call.write_text("ready")
        if outcome == "retry":
            return QueryAttempt.retry(
                Failure(FailureKind.AVAILABILITY, "sample", "getItem", "provider unavailable")
            )
        return QueryAttempt.branch_result(outcome, input, Receipt())


class QueryTerminal(dex.Step[QueryResult[str]]):
    """Record one typed terminal result before completing the Flow."""

    def __init__(
        self, step_type: str,
        result_attribute: dex.Attribute[QueryResult[str]] | None = None,
    ) -> None:
        self.step_type = step_type
        self.observed: list[QueryResult[str]] = []
        self.result_attribute = result_attribute

    def get_step_type(self) -> str:
        """Name this registered terminal instance."""
        return self.step_type

    async def execute(  # type: ignore[override]
        self, context: dex.AsyncContext, input: QueryResult[str]
    ) -> dex.StepDecision:
        """Finish after retaining the routed result for verification."""
        if self.result_attribute is not None:
            assert self.result_attribute.get(context) == input
        self.observed.append(input)
        return dex.graceful_complete()


class QueryFlow(dex.Flow[str]):
    """Minimal consuming application for a provider Query Step."""

    def __init__(
        self, name: str, step: QueryStep[str, str, str], terminals: list[QueryTerminal],
        result_attribute: dex.Attribute[QueryResult[str]] | None = None,
        progress_stream: dex.Stream[ProgressUpdate] | None = None,
        text_stream: dex.Stream[str] | None = None,
    ) -> None:
        self.name = name
        self.step = step
        self.terminals = terminals
        self.result_attribute = result_attribute
        self.progress_stream = progress_stream
        self.text_stream = text_stream

    def get_flow_type(self) -> str:
        """Return one stable example Flow type."""
        return self.name

    def get_steps(self) -> dex.StepList[str]:
        """Register the operation and its declared terminal targets."""
        return dex.StepList.start_step(self.step).other_steps(*self.terminals)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register each Attribute and Stream consumed by the connector Step."""
        return dex.PersistenceSchema.of(*(
            definition for definition in
            (self.result_attribute, self.progress_stream, self.text_stream)
            if definition is not None
        ))


def query_flow(
    name: str, operation: ScriptedQuery, *, wire_alternate: bool = False,
    retry: dex.RetryPolicy | None = None, with_outputs: bool = False,
) -> tuple[QueryFlow, QueryTerminal]:
    """Construct one real Flow around an operation-specific Step instance."""
    result_attribute = (
        dex.Attribute("result", QueryResult[str]) if with_outputs else None
    )
    progress_stream = (
        dex.Stream("progress", ProgressUpdate, 64 * 1024) if with_outputs else None
    )
    text_stream = dex.Stream("text", str, 64 * 1024) if with_outputs else None
    success = QueryTerminal("Success", result_attribute)
    alternate = QueryTerminal("Alternate")
    targets: dict[str, dex.StepTarget[QueryResult[str]]] = {"success": success}
    if wire_alternate:
        # The alternate target records the distinct business outcome before completion.
        targets["alternate"] = alternate
    options = dex.StepOptions(execute_retry=retry) if retry is not None else None
    step = QueryStep(QueryStepConfig(
        "GetItem", str, StepAnnotations("sample", "Sample", "Get one sample item."),
        operation, CONNECTION, lambda value: value, targets,
        result_attribute=result_attribute,
        progress_stream=progress_stream, text_stream=text_stream,
        step_options_override=options,
    ))
    return QueryFlow(
        name, step, [success, alternate], result_attribute, progress_stream, text_stream
    ), success


async def run_flow(
    flow: dex.Flow[str],
    inspect: Callable[[dex.AsyncClient, str], Awaitable[None]] | None = None,
) -> dex.FlowResult:
    """Run a real AsyncWorker and AsyncClient against the local Dex stack."""
    cache_root = Path(__file__).parents[1] / ".dexcli-data"
    cache_root.mkdir(exist_ok=True)
    with tempfile.TemporaryDirectory(dir=cache_root) as directory:
        cache = dex.open_blob_cache(dex.BlobCacheConfig(directory, 4 * 1024 * 1024))
        registry = dex.Registry([flow], allow_async_handlers=True)
        async with dex.AsyncWorker(
            registry, cache, dex.WorkerOptions(server_address=SERVER_ADDRESS)
        ) as worker:
            worker_task = asyncio.create_task(worker.start())
            try:
                await worker.wait_pull_registered(timeout=15)
                async with dex.AsyncClient(
                    registry, cache, dex.ClientOptions(server_address=SERVER_ADDRESS)
                ) as client:
                    flow_id = "sdkpython-integration-" + uuid4().hex
                    await client.start_flow(flow, flow_id, "input")
                    result = await asyncio.wait_for(
                        client.wait_for_flow(flow_id, timedelta(seconds=30)), 40
                    )
                    if inspect is not None:
                        await inspect(client, flow_id)
                    return result
            finally:
                await worker.stop()
                await worker_task
                cache.close()


@pytest.mark.asyncio
async def test_query_happy_path_and_declared_branch() -> None:
    """Real Worker routes success and an explicitly wired optional branch."""
    operation = ScriptedQuery(["success"])
    flow, sink = query_flow("QueryHappy", operation)
    result = await run_flow(flow)
    assert result.status == dex.FlowStatus.COMPLETED
    assert operation.calls == 1
    assert sink.observed[0].branch == "success"

    alternate_operation = ScriptedQuery(["alternate"])
    alternate_flow, _ = query_flow("QueryAlternate", alternate_operation, wire_alternate=True)
    alternate_result = await run_flow(alternate_flow)
    assert alternate_result.status == dex.FlowStatus.COMPLETED
    assert alternate_flow.terminals[1].observed[0].branch == "alternate"


@pytest.mark.asyncio
async def test_retry_then_success_and_retry_exhaustion() -> None:
    """Dex reruns a retryable Execute and fails the Flow after the attempt limit."""
    retry = dex.RetryPolicy(
        initial_interval=timedelta(seconds=1),
        backoff_coefficient=1,
        maximum_interval=timedelta(seconds=1),
        maximum_attempts=2,
        total_duration=timedelta(seconds=10),
    )
    eventual = ScriptedQuery(["retry", "success"])
    flow, sink = query_flow("QueryRetrySuccess", eventual, retry=retry)
    result = await run_flow(flow)
    assert result.status == dex.FlowStatus.COMPLETED
    assert eventual.calls == 2
    assert sink.observed[0].branch == "success"

    exhausted = ScriptedQuery(["retry"])
    failing_flow, _ = query_flow("QueryRetryExhausted", exhausted, retry=retry)
    failed = await run_flow(failing_flow)
    assert failed.status == dex.FlowStatus.FAILED
    assert exhausted.calls == 2


@pytest.mark.asyncio
async def test_unwired_optional_branch_fails_flow() -> None:
    """A selected business branch without application wiring fails explicitly."""
    operation = ScriptedQuery(["alternate"])
    flow, _ = query_flow("QueryUnwiredAlternate", operation)
    result = await run_flow(flow)
    assert result.status == dex.FlowStatus.FAILED
    assert "selected branch alternate with no target" in (result.error_message or "")


@pytest.mark.asyncio
async def test_result_attribute_and_progress_streams() -> None:
    """Real Dex persists the typed result and both configured Stream outputs."""
    operation = ScriptedQuery(["success"], emit_progress=True)
    flow, sink = query_flow("QueryOutputs", operation, with_outputs=True)
    assert flow.progress_stream is not None and flow.text_stream is not None
    progress_stream = flow.progress_stream
    text_stream = flow.text_stream

    async def inspect(client: dex.AsyncClient, flow_id: str) -> None:
        progress = await client.read_stream(
            flow_id, progress_stream, timeout=timedelta(seconds=5)
        )
        text = await client.read_stream(flow_id, text_stream, timeout=timedelta(seconds=5))
        assert progress.value.phase == "request"
        assert progress.value.sequence == 1
        assert progress.value.provider_sequence == 7
        assert text.value == "response ready"

    result = await run_flow(flow, inspect)
    assert result.status == dex.FlowStatus.COMPLETED
    assert sink.observed[0].branch == "success"


MUTATION_OPERATION = OperationRef("sample", "createItem")
MUTATION_BRANCHES = (
    BranchDefinition("success", "Created"),
    BranchDefinition("uncertain", "Provider outcome cannot be confirmed", True),
    BranchDefinition("defect", "Local defect", True),
)
UNCERTAIN_FAILURE = Failure(
    FailureKind.TRANSPORT, "sample", "createItem", "provider response was lost"
)


class ScriptedMutation:
    """Inert provider write classifications for real Dex Flow tests."""

    def __init__(self, outcome: str) -> None:
        self.outcome = outcome
        self.calls = 0

    def definition(self) -> MutationDefinition:
        """Declare sync Execute for non-idempotent write cases."""
        return MutationDefinition(
            MUTATION_OPERATION, MUTATION_BRANCHES,
            StepDefaults(timedelta(seconds=30), 1, timedelta(seconds=10)),
        )

    def idempotency_key(self, call_id: CallID, input: str) -> IdempotencyKey:
        """Return logical call identity; the inert provider does not honor it."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: str) -> MutationAttempt[str]:
        """Return a definite or unconfirmable provider outcome."""
        self.calls += 1
        if self.outcome == "uncertain":
            return MutationAttempt.uncertain(input, UNCERTAIN_FAILURE, Receipt())
        return MutationAttempt.branch_result("success", input, Receipt())


class FencedMutation(ScriptedMutation):
    """Model a possibly accepted write with read-back unavailable on retry."""

    def __init__(self, *, request_seconds: float = 0, lose_response: bool = False) -> None:
        super().__init__("success")
        self.request_seconds = request_seconds
        self.lose_response = lose_response
        self.provider_sends = 0
        self.read_backs = 0

    async def invoke(self, call: Call, input: str) -> MutationAttempt[str]:
        """Fence the request before credentials and keep an ambiguous send closed."""
        self.calls += 1
        fence = Fence(call, timedelta(seconds=30))
        decision = await fence.decide()
        if decision.kind == DecisionKind.READ_BACK_REQUIRED:
            self.read_backs += 1
            return MutationAttempt.uncertain(input, UNCERTAIN_FAILURE, Receipt())
        if decision.kind != DecisionKind.SEND_ALLOWED:
            raise RuntimeError("single-dispatch checkpoint is untrusted")

        async def send(deadline: object) -> str:
            self.provider_sends += 1
            if self.request_seconds:
                await asyncio.sleep(self.request_seconds)
            if self.lose_response:
                raise OSError("provider response was lost")
            return "provider-item"

        provider_id = await fence.dispatch(send)
        await fence.record_provider_id(provider_id)
        return MutationAttempt.branch_result("success", input, Receipt())


class MockReplayMutation(ScriptedMutation):
    """Drop one accepted mock response so Dex retries the same logical call."""

    def __init__(self) -> None:
        super().__init__("success")
        self.mock = MockMutation[str, str](self.definition()).respond(
            Case.branch_result("success", "mock-created")
        )

    async def invoke(self, call: Call, input: str) -> MutationAttempt[str]:
        """Replay a keyed terminal mock outcome on the second Execute attempt."""
        self.calls += 1
        answer = await self.mock.invoke(call, input)
        if self.calls == 1:
            raise OSError("inert response lost after mock acceptance")
        return answer


class MutationTerminal(dex.Step[MutationResult[str]]):
    """Consume a definite or uncertain write result."""

    def __init__(self, step_type: str) -> None:
        self.step_type = step_type
        self.observed: list[MutationResult[str]] = []

    def get_step_type(self) -> str:
        """Return the registered terminal instance identity."""
        return self.step_type

    async def execute(  # type: ignore[override]
        self, context: dex.AsyncContext, input: MutationResult[str]
    ) -> dex.StepDecision:
        """Finish after capturing the routed write classification."""
        self.observed.append(input)
        return dex.graceful_complete()


class MutationFlow(dex.Flow[str]):
    """Minimal consuming application for a provider Mutation Step."""

    def __init__(
        self, name: str, step: MutationStep[str, str, str], terminals: list[MutationTerminal]
    ) -> None:
        self.name = name
        self.step = step
        self.terminals = terminals

    def get_flow_type(self) -> str:
        """Return this example's stable Flow type."""
        return self.name

    def get_steps(self) -> dex.StepList[str]:
        """Register the operation and terminal result consumers."""
        return dex.StepList.start_step(self.step).other_steps(*self.terminals)


def mutation_flow(
    name: str, operation: ScriptedMutation,
    *, wire_uncertain: bool = False, max_attempts: int = 3,
) -> tuple[MutationFlow, MutationTerminal]:
    """Construct an instance-addressed write Step with explicit retry budget."""
    success = MutationTerminal("Success")
    uncertain = MutationTerminal("Uncertain")
    targets: dict[str, dex.StepTarget[MutationResult[str]]] = {"success": success}
    if wire_uncertain:
        # The uncertain target records a write requiring manual reconciliation.
        targets["uncertain"] = uncertain
    retry = dex.RetryPolicy(
        initial_interval=timedelta(seconds=1), backoff_coefficient=1,
        maximum_interval=timedelta(seconds=1), maximum_attempts=max_attempts,
        total_duration=timedelta(minutes=1),
    )
    step = MutationStep(MutationStepConfig(
        "CreateItem", str, StepAnnotations("sample", "Sample", "Create one sample item."),
        operation, CONNECTION, lambda value: value, targets,
        step_options_override=dex.StepOptions(execute_retry=retry),
    ))
    return MutationFlow(name, step, [success, uncertain]), uncertain


@pytest.mark.asyncio
async def test_uncertain_route_and_single_dispatch_after_lost_response() -> None:
    """A retry reads back an ambiguous write and never dispatches it twice."""
    direct = ScriptedMutation("uncertain")
    direct_flow, uncertain_sink = mutation_flow("MutationUncertain", direct, wire_uncertain=True)
    direct_result = await run_flow(direct_flow)
    assert direct_result.status == dex.FlowStatus.COMPLETED
    assert uncertain_sink.observed[0].branch == "uncertain"

    lost = FencedMutation(lose_response=True)
    lost_flow, lost_sink = mutation_flow("MutationLostResponse", lost, wire_uncertain=True)
    lost_result = await run_flow(lost_flow)
    assert lost_result.status == dex.FlowStatus.COMPLETED
    assert lost.provider_sends == 1
    assert lost.read_backs == 1
    assert lost_sink.observed[0].branch == "uncertain"


@pytest.mark.asyncio
async def test_heartbeat_cadence_keeps_slow_fenced_request_alive() -> None:
    """Five-second claim heartbeats keep a 12-second sync Execute alive."""
    slow = FencedMutation(request_seconds=12)
    flow, _ = mutation_flow("MutationSlowHeartbeat", slow)
    result = await run_flow(flow)
    assert result.status == dex.FlowStatus.COMPLETED
    assert slow.provider_sends == 1


@pytest.mark.asyncio
async def test_mock_terminal_result_replays_on_step_retry() -> None:
    """A real Dex retry reuses the mock's stable keyed terminal result."""
    operation = MockReplayMutation()
    flow, _ = mutation_flow("MutationMockReplay", operation)
    result = await run_flow(flow)
    assert result.status == dex.FlowStatus.COMPLETED
    assert operation.calls == 2
    assert [call.replay for call in operation.mock.calls()] == [False, True]
    operation.mock.assert_no_failures()
