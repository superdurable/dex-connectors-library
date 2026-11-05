# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Cancel one submitted Snowflake statement through Dex."""

import dex
from dex_connectors.sdk import MutationResult

from dex_connectors.snowflake import (
    CancelStatementInput,
    Connection,
    StatementCancellation,
    new_cancel_statement_step,
)

from .flow import CONNECTION_NAME


# dex:group group-id:snowflake group-label:"Snowflake"
# dex:explanation text:"Return a confirmed cancellation to the Flow caller."
class CompleteCancellation(dex.Step[MutationResult[StatementCancellation]]):
    """Complete an accepted cancellation."""

    def execute(self, context: dex.Context, input: MutationResult[StatementCancellation]) -> dex.StepDecision:
        """Return the canceled handle."""
        return dex.graceful_complete(input.value)


class CancelStatementFlow(dex.Flow[CancelStatementInput]):
    """Cancel a known Snowflake statement by handle."""

    def __init__(self, connection: Connection) -> None:
        self.complete = CompleteCancellation()
        self.cancel = new_cancel_statement_step(
            step_type="CancelStatement",
            step_input_type=CancelStatementInput,
            annotations={
                "groupId": "snowflake",
                "groupLabel": "Snowflake",
                "explanation": "Cancel one submitted Snowflake statement.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda value: value,
            canceled=self.complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable application Flow identity."""
        return "SnowflakePythonCancelStatement"

    def get_steps(self) -> dex.StepList[CancelStatementInput]:
        """Register the cancel and completion Steps."""
        return dex.StepList.start_step(self.cancel).other_steps(self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Declare that cancellation keeps no Attributes or Streams."""
        return dex.PersistenceSchema.of()
