# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable PubSub page Flow using the listTopics Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.google_pubsub import (
    Connection,
    ListTopicsInput,
    TopicsPage,
    new_list_topics_step,
)

FLOW_TYPE = "PubSubTopicsListPython"
CONNECTION_NAME = "google-pubsub"
_CALL_STEP_TYPE = "CallListTopics"
_RESULT = dex.Attribute("google_pubsub-python-page", TopicsPage)


def map_to_operation_input(request: ListTopicsInput) -> ListTopicsInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:google-pubsub group-label:"PubSub"
# dex:explanation text:"Start the PubSub request."
class _StartRequest(dex.Step[ListTopicsInput]):
    def get_step_type(self) -> str:
        return "StartListTopics"

    def execute(self, context: dex.Context, input: ListTopicsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallListTopics"), input)


# dex:group group-id:google-pubsub group-label:"PubSub"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[TopicsPage]]):
    def get_step_type(self) -> str:
        return "CompleteListTopics"

    def execute(self, context: dex.Context, input: QueryResult[TopicsPage]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class TopicsFlow(dex.Flow[ListTopicsInput]):
    """Run one listTopics read through the static google_pubsub connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("the topics lookup Flow requires the static google_pubsub connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_list_topics_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=ListTopicsInput,
            annotations={
                "groupId": "google-pubsub",
                "groupLabel": "PubSub",
                "explanation": "Read one PubSub page.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "PubSubTopicsListPython"

    def get_steps(self) -> dex.StepList[ListTopicsInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
