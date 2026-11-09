# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Databricks job_run inspection Flow using getJobRun."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.databricks import (
    Connection,
    GetJobRunInput,
    JobRunStatus,
    new_get_job_run_step,
)

FLOW_TYPE = "DatabricksJobRunInspectionPython"
CONNECTION_NAME = "databricks"
_CALL_STEP_TYPE = "CallDatabricks"
_RESULT = dex.Attribute("databricks-job_run", JobRunStatus)


def map_to_operation_input(request: GetJobRunInput) -> GetJobRunInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:databricks group-label:"Databricks"
# dex:explanation text:"Start the Databricks request."
class _StartRequest(dex.Step[GetJobRunInput]):
    def get_step_type(self) -> str:
        return "StartDatabricksRequest"

    def execute(self, context: dex.Context, input: GetJobRunInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallDatabricks"), input)


# dex:group group-id:databricks group-label:"Databricks"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[JobRunStatus]]):
    def get_step_type(self) -> str:
        return "CompleteDatabricksRequest"

    def execute(self, context: dex.Context, input: QueryResult[JobRunStatus]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class JobRunInspectionFlow(dex.Flow[GetJobRunInput]):
    """Read one job_run through the static databricks connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("job_run inspection requires the static databricks connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_get_job_run_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=GetJobRunInput,
            annotations={
                "groupId": "databricks",
                "groupLabel": "Databricks",
                "explanation": "Read one job_run from the connected Databricks account.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "DatabricksJobRunInspectionPython"

    def get_steps(self) -> dex.StepList[GetJobRunInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
