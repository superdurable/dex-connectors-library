# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Yuandian getCompanyProfile Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.yuandian import (
    Connection,
    GetCompanyProfileInput,
    GetCompanyProfileOutput,
    new_get_company_profile_step,
)

CONNECTION_NAME = "yuandian"
_RESULT = dex.Attribute("yuandian-result", GetCompanyProfileOutput)


def map_to_operation_input(request: GetCompanyProfileInput) -> GetCompanyProfileInput:
    """Forward the provider request."""
    return request


# dex:group group-id:yuandian group-label:"Yuandian"
# dex:explanation text:"Start the Yuandian query."
class _StartQuery(dex.Step[GetCompanyProfileInput]):
    def execute(self, context: dex.Context, input: GetCompanyProfileInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallProvider"), input)


# dex:group group-id:yuandian group-label:"Yuandian"
# dex:explanation text:"Persist the provider result before completion."
class _CompleteQuery(dex.Step[QueryResult[GetCompanyProfileOutput]]):
    def execute(self, context: dex.Context, input: QueryResult[GetCompanyProfileOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class GetCompanyProfileFlow(dex.Flow[GetCompanyProfileInput]):
    """Run one Yuandian query through the static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Yuandian Flow requires its static connection")
        self._start = _StartQuery()
        self._complete = _CompleteQuery()
        self._call = new_get_company_profile_step(
            step_type="CallProvider",
            step_input_type=GetCompanyProfileInput,
            annotations={
                "groupId": "yuandian",
                "groupLabel": "Yuandian",
                "explanation": "Run Yuandian getCompanyProfile.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return this example's stable Flow identity."""
        return "GetCompanyProfileYuandian"

    def get_steps(self) -> dex.StepList[GetCompanyProfileInput]:
        """Register the provider and persistence Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the filtered provider result."""
        return dex.PersistenceSchema.of(_RESULT)
