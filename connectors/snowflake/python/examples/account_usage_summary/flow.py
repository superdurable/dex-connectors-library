# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Submit and read a Snowflake statement through Dex Steps."""

import dex
from dex_connectors.sdk import MutationResult, QueryResult

from dex_connectors.snowflake import (
    Connection,
    GetStatementResultInput,
    StatementResult,
    StatementSubmission,
    SubmitStatementInput,
    new_get_statement_result_step,
    new_submit_statement_step,
)

CONNECTION_NAME = "snowflake-warehouse"


# dex:group group-id:snowflake group-label:"Snowflake"
# dex:explanation text:"Carry the accepted statement handle to a separate status read."
class ReadSubmittedStatement(dex.Step[MutationResult[StatementSubmission]]):
    """Use a submission handle in the following Query Step."""

    def execute(self, context: dex.Context, input: MutationResult[StatementSubmission]) -> dex.StepDecision:
        """Continue with the handle Snowflake returned."""
        return dex.go_to(
            dex.StepRef("ReadStatementResult"),
            GetStatementResultInput(statement_handle=input.value.statement_handle),
        )


# dex:group group-id:snowflake group-label:"Snowflake"
# dex:explanation text:"Return the bounded result partition to the Flow caller."
class CompleteResult(dex.Step[QueryResult[StatementResult]]):
    """Complete a successful status read."""

    def execute(self, context: dex.Context, input: QueryResult[StatementResult]) -> dex.StepDecision:
        """Return the typed result without provider credentials."""
        return dex.graceful_complete(input.value)


class StatementResultFlow(dex.Flow[SubmitStatementInput]):
    """Submit one statement and read its immediately available result."""

    def __init__(self, connection: Connection) -> None:
        self.complete = CompleteResult()
        self.read = new_get_statement_result_step(
            step_type="ReadStatementResult",
            step_input_type=GetStatementResultInput,
            annotations={
                "groupId": "snowflake",
                "groupLabel": "Snowflake",
                "explanation": "Read the submitted statement's first result partition.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda value: value,
            completed=self.complete,
        )
        self.route = ReadSubmittedStatement()
        self.submit = new_submit_statement_step(
            step_type="SubmitStatement",
            step_input_type=SubmitStatementInput,
            annotations={
                "groupId": "snowflake",
                "groupLabel": "Snowflake",
                "explanation": "Submit one parameterized SQL statement asynchronously.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda value: value,
            submitted=self.route,
        )

    def get_flow_type(self) -> str:
        """Return the stable application Flow identity."""
        return "SnowflakePythonStatementResult"

    def get_steps(self) -> dex.StepList[SubmitStatementInput]:
        """Register the submit, route, read, and completion Steps."""
        return dex.StepList.start_step(self.submit).other_steps(self.route, self.read, self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Declare that this short Flow keeps no Attributes or Streams."""
        return dex.PersistenceSchema.of()
