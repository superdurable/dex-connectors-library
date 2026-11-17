# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Send one order POST that must never be sent twice through the named OAuth HTTP connection."""

import dex
from dex_connectors.sdk import MutationResult

from dex_connectors.superdurable_oauth2_http import (
    Connection,
    PostNonIdempotentInput,
    SendRequestOutput,
    new_post_non_idempotent_step,
)

CONNECTION_NAME = "oauth2-http-api"
ORDER_FLOW_TYPE = "OAuth2HTTPPythonOrder"


# dex:group group-id:oauth2-http-order group-label:"OAuth HTTP order"
# dex:explanation text:"Complete with the API status and bounded response body of the order."
class _CompleteOrder(dex.Step[MutationResult[SendRequestOutput]]):
    def get_step_type(self) -> str:
        return "OAuth2HTTPPythonCompleteOrder"

    def execute(self, context: dex.Context, input: MutationResult[SendRequestOutput]) -> dex.StepDecision:
        return dex.graceful_complete(input.value)


class OrderFlow(dex.Flow[PostNonIdempotentInput]):
    """Send one order POST that is never repeated after it may have reached the API."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("OAuth HTTP order Flow requires the static oauth2-http-api connection")
        self.complete = _CompleteOrder()
        self.post_order = new_post_non_idempotent_step(
            step_type="OAuth2HTTPPythonPostOrder",
            step_input_type=PostNonIdempotentInput,
            annotations={
                "groupId": "oauth2-http-order",
                "groupLabel": "OAuth HTTP order",
                "explanation": "Send the order once as a POST that is never repeated after a possible send.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda input: input,
            completed=self.complete,
        )

    def get_flow_type(self) -> str:
        return "OAuth2HTTPPythonOrder"

    def get_steps(self) -> dex.StepList[PostNonIdempotentInput]:
        return dex.StepList.start_step(self.post_order).other_steps(self.complete)
