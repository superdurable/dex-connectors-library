# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Amazon Lambda function lookup Flow using getFunction."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.amazon_lambda import (
    Connection,
    FunctionDetails,
    GetFunctionInput,
    new_get_function_step,
)

FLOW_TYPE = "AmazonLambdaFunctionLookupPython"
CONNECTION_NAME = "amazon-lambda"
_CALL_STEP_TYPE = "CallGetFunction"
_RESULT = dex.Attribute("lambda-function-lookup", FunctionDetails)


def map_to_operation_input(request: GetFunctionInput) -> GetFunctionInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:lambda group-label:"Amazon Lambda"
# dex:explanation text:"Start the Amazon Lambda request."
class _StartRequest(dex.Step[GetFunctionInput]):
    def get_step_type(self) -> str:
        return "StartGetFunctionRequest"

    def execute(self, context: dex.Context, input: GetFunctionInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallGetFunction"), input)


# dex:group group-id:lambda group-label:"Amazon Lambda"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[FunctionDetails]]):
    def get_step_type(self) -> str:
        return "CompleteGetFunctionRequest"

    def execute(self, context: dex.Context, input: QueryResult[FunctionDetails]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class FunctionLookupFlow(dex.Flow[GetFunctionInput]):
    """Read one function metadata through the static amazon-lambda connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("function lookup requires the static amazon-lambda connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_get_function_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=GetFunctionInput,
            annotations={
                "groupId": "lambda",
                "groupLabel": "Amazon Lambda",
                "explanation": "Read one function metadata from the connected Amazon Lambda account.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "AmazonLambdaFunctionLookupPython"

    def get_steps(self) -> dex.StepList[GetFunctionInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
