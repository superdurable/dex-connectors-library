# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read one Qichacha response through a durable Dex Query Step."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.qichacha import (
    Connection,
    GetEnterpriseBasicDetailsInput,
    GetEnterpriseBasicDetailsOutput,
    new_get_enterprise_basic_details_step,
)

CONNECTION_NAME = "qichacha"
_RESULT = dex.Attribute("qichacha-query-result", GetEnterpriseBasicDetailsOutput)


def map_to_operation_input(input: GetEnterpriseBasicDetailsInput) -> GetEnterpriseBasicDetailsInput:
    """Pass the typed request into the provider Query."""
    return input


# dex:group group-id:query group-label:"Provider query"
# dex:explanation text:"Start the Qichacha provider Query."
class _StartQuery(dex.Step[GetEnterpriseBasicDetailsInput]):
    def get_step_type(self) -> str:
        return "StartGetEnterpriseBasicDetailsQuery"

    def execute(self, context: dex.Context, input: GetEnterpriseBasicDetailsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("GetEnterpriseBasicDetailsQuery"), input)


# dex:group group-id:query group-label:"Provider query"
# dex:explanation text:"Persist the validated Qichacha Query result."
class _CompleteQuery(dex.Step[QueryResult[GetEnterpriseBasicDetailsOutput]]):
    def get_step_type(self) -> str:
        return "CompleteGetEnterpriseBasicDetailsQuery"

    def execute(
        self, context: dex.Context, input: QueryResult[GetEnterpriseBasicDetailsOutput]
    ) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class QichachaGetEnterpriseBasicDetailsFlow(dex.Flow[GetEnterpriseBasicDetailsInput]):
    """Run the Qichacha Query with the static named connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Qichacha Query requires its static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._query = new_get_enterprise_basic_details_step(
            step_type="GetEnterpriseBasicDetailsQuery",
            step_input_type=GetEnterpriseBasicDetailsInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "Qichacha",
                "explanation": "Call Qichacha GetEnterpriseBasicDetails using the configured credential.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            fetched=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity for Worker registration."""
        return "QichachaGetEnterpriseBasicDetails"

    def get_steps(self) -> dex.StepList[GetEnterpriseBasicDetailsInput]:
        """Register the start, Query, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._query, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the Query output for later reads."""
        return dex.PersistenceSchema.of(_RESULT)
