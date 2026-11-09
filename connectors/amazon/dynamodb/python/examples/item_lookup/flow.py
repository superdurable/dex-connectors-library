# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Amazon DynamoDB item lookup Flow using getItem."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.amazon_dynamodb import (
    Connection,
    GetItemInput,
    ItemResult,
    new_get_item_step,
)

FLOW_TYPE = "AmazonDynamoDBItemLookupPython"
CONNECTION_NAME = "amazon-dynamodb"
_CALL_STEP_TYPE = "CallGetItem"
_RESULT = dex.Attribute("dynamodb-item-lookup", ItemResult)


def map_to_operation_input(request: GetItemInput) -> GetItemInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:dynamodb group-label:"Amazon DynamoDB"
# dex:explanation text:"Start the Amazon DynamoDB request."
class _StartRequest(dex.Step[GetItemInput]):
    def get_step_type(self) -> str:
        return "StartGetItemRequest"

    def execute(self, context: dex.Context, input: GetItemInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallGetItem"), input)


# dex:group group-id:dynamodb group-label:"Amazon DynamoDB"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[ItemResult]]):
    def get_step_type(self) -> str:
        return "CompleteGetItemRequest"

    def execute(self, context: dex.Context, input: QueryResult[ItemResult]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ItemLookupFlow(dex.Flow[GetItemInput]):
    """Read one item through the static amazon-dynamodb connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("item lookup requires the static amazon-dynamodb connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_get_item_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=GetItemInput,
            annotations={
                "groupId": "dynamodb",
                "groupLabel": "Amazon DynamoDB",
                "explanation": "Read one item from the connected Amazon DynamoDB account.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "AmazonDynamoDBItemLookupPython"

    def get_steps(self) -> dex.StepList[GetItemInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
