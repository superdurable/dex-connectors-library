# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable WxPusher get subscriber count Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.wxpusher import (
    Connection,
    GetSubscriberCountInput,
    SubscriberCountResult,
    new_get_subscriber_count_step,
)

CONNECTION_NAME = "wxpusher"
_RESULT = dex.Attribute("wxpusher-get-subscriber-count-result", SubscriberCountResult)


def map_to_operation_input(request: GetSubscriberCountInput) -> GetSubscriberCountInput:
    """Forward the typed request to the provider Query."""
    return request


# dex:group group-id:wxpusher group-label:"WxPusher"
# dex:explanation text:"Start the WxPusher get subscriber count request."
class _StartQuery(dex.Step[GetSubscriberCountInput]):
    def get_step_type(self) -> str:
        return "StartWxPusherSubscriberCount"

    def execute(self, context: dex.Context, input: GetSubscriberCountInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallWxPusherSubscriberCount"), input)


# dex:group group-id:wxpusher group-label:"WxPusher"
# dex:explanation text:"Persist the provider result and complete the Flow."
class _CompleteQuery(dex.Step[QueryResult[SubscriberCountResult]]):
    def get_step_type(self) -> str:
        return "CompleteWxPusherSubscriberCount"

    def execute(self, context: dex.Context, input: QueryResult[SubscriberCountResult]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class WxPusherSubscriberCountFlow(dex.Flow[GetSubscriberCountInput]):
    """Run the get subscriber count Query with a static named connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("WxPusherSubscriberCountFlow requires the static WxPusher connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._call = new_get_subscriber_count_step(
            step_type="CallWxPusherSubscriberCount",
            step_input_type=GetSubscriberCountInput,
            annotations={
                "groupId": "wxpusher",
                "groupLabel": "WxPusher",
                "explanation": "Get subscriber count with the configured WxPusher connection.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            counted=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable get subscriber count Flow identity."""
        return "WxPusherSubscriberCount"

    def get_steps(self) -> dex.StepList[GetSubscriberCountInput]:
        """Register the application and connector Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the bounded get subscriber count result."""
        return dex.PersistenceSchema.of(_RESULT)
