# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetJobRunInput selects a job run by its non-secret Databricks ID.
@dataclass(frozen=True, slots=True)
class GetJobRunInput:
    # RunID is the ID shown in Databricks JobRuns or returned by the API.
    run_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'runId'})


setattr(GetJobRunInput, '__module__', 'dex_connectors.databricks.models')


# JobRunStatus excludes task details and other workspace metadata.
@dataclass(frozen=True, slots=True)
class JobRunStatus:
    # ID is the Databricks job run ID.
    id: str = ''
    # ResultState is the terminal result state, empty for nonterminal runs.
    result_state: str = _dataclass_field(default='', metadata={'dex_json_name': 'resultState'})
    # Status is the current run state, such as RUNNING.
    status: str = ''


setattr(JobRunStatus, '__module__', 'dex_connectors.databricks.models')
