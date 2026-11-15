# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed Dex Flow and RPC Trigger targets for one application Flow.

`new_dex_rpc_trigger_target` needs `map_to_rpc_input` exactly when the RPC handler takes an input, and
mypy checks the mapper's result against the handler's input type.
"""

from dataclasses import dataclass

import dex

from dex_connectors.sdk import (
    TriggerEvent,
    TriggerTarget,
    new_dex_flow_trigger_target,
    new_dex_rpc_trigger_target,
)


@dataclass(frozen=True)
class ApprovalEvent:
    """A provider event about one approval."""

    approval_id: str
    decision: str


@dataclass(frozen=True)
class ApprovalRequest:
    """The Flow input that opens one approval."""

    approval_id: str


class AwaitDecision(dex.Step[ApprovalRequest]):
    """Keeps the approval open for decision RPCs."""

    def get_step_type(self) -> str:
        """Name the waiting Step."""
        return "AwaitDecision"

    def execute(self, context: dex.Context, input: ApprovalRequest) -> dex.StepDecision:
        """Wait for RPCs."""
        return dex.dead_end()


class ApprovalFlow(dex.Flow[ApprovalRequest]):
    """One Flow per approval, decided by RPCs."""

    def get_flow_type(self) -> str:
        """Return the stable Flow type."""
        return "Approval"

    def get_steps(self) -> dex.StepList[ApprovalRequest]:
        """Register the waiting Step."""
        return dex.StepList.start_step(AwaitDecision())

    @dex.rpc
    def record_decision(self, context: dex.Context, decision: str) -> None:
        """Record one decision; a repeated event ID must not apply twice."""

    @dex.rpc
    def send_reminder(self, context: dex.Context) -> None:
        """Remind the approver."""


def approval_flow_id(event: TriggerEvent[ApprovalEvent]) -> str:
    """Route an event to its approval's Flow."""
    return f"approval-{event.payload.approval_id}"


def new_approval_targets(
    client: dex.AsyncClient, flow: ApprovalFlow
) -> tuple[TriggerTarget[ApprovalEvent], TriggerTarget[ApprovalEvent], TriggerTarget[ApprovalEvent]]:
    """Start approvals, record decisions, and send reminders from provider events."""
    start: TriggerTarget[ApprovalEvent] = new_dex_flow_trigger_target(
        client,
        flow,
        accepts_event=lambda event: event.payload.decision == "requested",
        resolve_flow_id=approval_flow_id,
        map_to_flow_input=lambda event: ApprovalRequest(event.payload.approval_id),
    )
    decide: TriggerTarget[ApprovalEvent] = new_dex_rpc_trigger_target(
        client,
        flow.record_decision,
        accepts_event=lambda event: event.payload.decision in ("approved", "rejected"),
        resolve_flow_id=approval_flow_id,
        map_to_rpc_input=lambda event: event.payload.decision,
    )
    remind: TriggerTarget[ApprovalEvent] = new_dex_rpc_trigger_target(
        client,
        flow.send_reminder,
        accepts_event=lambda event: event.payload.decision == "overdue",
        resolve_flow_id=approval_flow_id,
    )
    return start, decide, remind
