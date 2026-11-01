# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable outbound lookup Flow using the Postmark listOutboundMessages Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.postmark import (
    Connection,
    ListOutboundMessagesInput,
    MessagePage,
    new_list_outbound_messages_step,
)

FLOW_TYPE = "PostmarkOutboundLookup"
CONNECTION_NAME = "postmark"
_LIST_STEP_TYPE = "ListOutboundMessages"
_OUTCOME = dex.Attribute("postmark-outbound-lookup-outcome", MessagePage)


def map_to_list_outbound_messages_input(request: ListOutboundMessagesInput) -> ListOutboundMessagesInput:
    """Search with the requested page window unchanged."""
    return request


# dex:group group-id:outbound-lookup group-label:"Outbound lookup"
# dex:explanation text:"Check the requested page window before calling Postmark."
class _PrepareRequest(dex.Step[ListOutboundMessagesInput]):
    def get_step_type(self) -> str:
        return "PrepareRequest"

    def execute(self, context: dex.Context, input: ListOutboundMessagesInput) -> dex.StepDecision:
        if input.count < 0 or input.offset < 0:
            return dex.force_fail("count and offset must not be negative")
        return dex.go_to(dex.StepRef(_LIST_STEP_TYPE), input)


# dex:group group-id:outbound-lookup group-label:"Outbound lookup"
# dex:explanation text:"Record the outbound message page and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[MessagePage]]):
    def get_step_type(self) -> str:
        return "CompleteRequest"

    def execute(self, context: dex.Context, input: QueryResult[MessagePage]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class OutboundLookupFlow(dex.Flow[ListOutboundMessagesInput]):
    """List one page of outbound messages through the static postmark connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("outbound lookup requires the static postmark connection")
        self._prepare = _PrepareRequest()
        self._complete = _CompleteRequest()
        self._list = new_list_outbound_messages_step(
            step_type=_LIST_STEP_TYPE,
            step_input_type=ListOutboundMessagesInput,
            annotations={
                "groupId": "postmark",
                "groupLabel": "Postmark",
                "explanation": "List recent outbound messages from Postmark.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_list_outbound_messages_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[ListOutboundMessagesInput]:
        """Register request check, provider search, and completion Steps."""
        return dex.StepList.start_step(self._prepare).other_steps(self._list, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the outbound message page."""
        return dex.PersistenceSchema.of(_OUTCOME)
