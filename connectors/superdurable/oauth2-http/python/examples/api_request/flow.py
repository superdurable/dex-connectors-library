# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Send one request through the named OAuth HTTP connection."""

import dex
from dex_connectors.sdk import MutationResult

from dex_connectors.superdurable_oauth2_http import (
    Connection,
    SendRequestInput,
    SendRequestOutput,
    new_send_request_step,
)

CONNECTION_NAME = "oauth2-http-api"
FLOW_TYPE = "OAuth2HTTPPythonAPIRequest"


# dex:group group-id:oauth2-http-request group-label:"OAuth HTTP API request"
# dex:explanation text:"Complete with the API status and bounded response body."
class _Complete(dex.Step[MutationResult[SendRequestOutput]]):
    def get_step_type(self) -> str:
        return "OAuth2HTTPPythonComplete"

    def execute(self, context: dex.Context, input: MutationResult[SendRequestOutput]) -> dex.StepDecision:
        return dex.graceful_complete(input.value)


class APIRequestFlow(dex.Flow[SendRequestInput]):
    """Call one OAuth-protected API through a stable Dex connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("OAuth HTTP request Flow requires the static oauth2-http-api connection")
        self.complete = _Complete()
        self.send_request = new_send_request_step(
            step_type="OAuth2HTTPPythonSendRequest",
            step_input_type=SendRequestInput,
            annotations={
                "groupId": "oauth2-http-request",
                "groupLabel": "OAuth HTTP API request",
                "explanation": "Send one authorized request to the connection's API origin.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda input: input,
            completed=self.complete,
        )

    def get_flow_type(self) -> str:
        return "OAuth2HTTPPythonAPIRequest"

    def get_steps(self) -> dex.StepList[SendRequestInput]:
        return dex.StepList.start_step(self.send_request).other_steps(self.complete)
