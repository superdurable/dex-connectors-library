# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Amazon Redshift statement result Flow using getStatementResult."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.amazon_redshift import (
    Connection,
    GetStatementResultInput,
    StatementResultPage,
    new_get_statement_result_step,
)

FLOW_TYPE = "AmazonRedshiftStatementResultsPython"
CONNECTION_NAME = "amazon-redshift"
_CALL_STEP_TYPE = "CallGetStatementResult"
_RESULT = dex.Attribute("redshift-statement-results", StatementResultPage)


def map_to_operation_input(request: GetStatementResultInput) -> GetStatementResultInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:redshift group-label:"Amazon Redshift"
# dex:explanation text:"Start the Amazon Redshift request."
class _StartRequest(dex.Step[GetStatementResultInput]):
    def get_step_type(self) -> str:
        return "StartGetStatementResultRequest"

    def execute(self, context: dex.Context, input: GetStatementResultInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallGetStatementResult"), input)


# dex:group group-id:redshift group-label:"Amazon Redshift"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[StatementResultPage]]):
    def get_step_type(self) -> str:
        return "CompleteGetStatementResultRequest"

    def execute(self, context: dex.Context, input: QueryResult[StatementResultPage]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class StatementResultsFlow(dex.Flow[GetStatementResultInput]):
    """Read one statement result page through the static amazon-redshift connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("statement result requires the static amazon-redshift connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_get_statement_result_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=GetStatementResultInput,
            annotations={
                "groupId": "redshift",
                "groupLabel": "Amazon Redshift",
                "explanation": "Read one statement result page from the connected Amazon Redshift account.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            fetched=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "AmazonRedshiftStatementResultsPython"

    def get_steps(self) -> dex.StepList[GetStatementResultInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
