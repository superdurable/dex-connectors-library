# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Amazon SNS topic listing Flow using listTopics."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.amazon_sns import (
    Connection,
    ListTopicsInput,
    TopicPage,
    new_list_topics_step,
)

FLOW_TYPE = "AmazonSNSTopicListingPython"
CONNECTION_NAME = "amazon-sns"
_CALL_STEP_TYPE = "CallListTopics"
_RESULT = dex.Attribute("sns-topic-listing", TopicPage)


def map_to_operation_input(request: ListTopicsInput) -> ListTopicsInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:sns group-label:"Amazon SNS"
# dex:explanation text:"Start the Amazon SNS request."
class _StartRequest(dex.Step[ListTopicsInput]):
    def get_step_type(self) -> str:
        return "StartListTopicsRequest"

    def execute(self, context: dex.Context, input: ListTopicsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallListTopics"), input)


# dex:group group-id:sns group-label:"Amazon SNS"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[TopicPage]]):
    def get_step_type(self) -> str:
        return "CompleteListTopicsRequest"

    def execute(self, context: dex.Context, input: QueryResult[TopicPage]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class TopicListingFlow(dex.Flow[ListTopicsInput]):
    """Read one page of topic ARNs through the static amazon-sns connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("topic listing requires the static amazon-sns connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_list_topics_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=ListTopicsInput,
            annotations={
                "groupId": "sns",
                "groupLabel": "Amazon SNS",
                "explanation": "Read one page of topic ARNs from the connected Amazon SNS account.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            page=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "AmazonSNSTopicListingPython"

    def get_steps(self) -> dex.StepList[ListTopicsInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
