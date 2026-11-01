# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Mailgun demo Flow using the getEvents Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.mailgun import Connection, EventsOutput, GetEventsInput, new_get_events_step

FLOW_TYPE = "MailgunDemo"
CONNECTION_NAME = "mailgun"
_OPERATION_STEP_TYPE = "CallGetEvents"
_OUTCOME = dex.Attribute("mailgun-demo-outcome", EventsOutput)


def map_to_operation_input(input: GetEventsInput) -> GetEventsInput:
    """Read the requested domain events unchanged."""
    return input


# dex:group group-id:demo group-label:"Demo"
# dex:explanation text:"Start the event lookup with the requested domain."
class _StartRequest(dex.Step[GetEventsInput]):
    def get_step_type(self) -> str:
        return "StartGetEvents"

    def execute(self, context: dex.Context, input: GetEventsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef(_OPERATION_STEP_TYPE), input)


# dex:group group-id:demo group-label:"Demo"
# dex:explanation text:"Record the events and complete the Flow."
class _CompleteResult(dex.Step[QueryResult[EventsOutput]]):
    def get_step_type(self) -> str:
        return "CompleteGetEvents"

    def execute(self, context: dex.Context, input: QueryResult[EventsOutput]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class DemoFlow(dex.Flow[GetEventsInput]):
    """Read domain events through the static mailgun connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("demo requires the static mailgun connection")
        self._start = _StartRequest()
        self._complete = _CompleteResult()
        self._read = new_get_events_step(
            step_type=_OPERATION_STEP_TYPE,
            step_input_type=GetEventsInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "Mailgun",
                "explanation": "Call the provider's getEvents operation.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[GetEventsInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._read, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the event page outcome."""
        return dex.PersistenceSchema.of(_OUTCOME)
