# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from typing import Any

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# ListJobFunctionsInput has no user-supplied query parameters.
@dataclass(frozen=True, slots=True)
class ListJobFunctionsInput:
    pass


setattr(ListJobFunctionsInput, '__module__', 'dex_connectors.moka.models')


# JobFunction contains non-personal recruitment category metadata.
@dataclass(frozen=True, slots=True)
class JobFunction:
    # ID identifies the job function in Moka.
    id: int = 0
    # Label is the job function's displayed name.
    label: str = ''
    # ParentID identifies the parent function, or zero when Moka omits it.
    parent_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'parentId'})
    # JobCount counts this function and its descendants' jobs.
    job_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'jobCount'})


setattr(JobFunction, '__module__', 'dex_connectors.moka.models')


# ListJobFunctionsOutput contains the available recruitment functions.
@dataclass(frozen=True, slots=True)
class ListJobFunctionsOutput:
    # Functions contains only job function IDs, labels, parent IDs, and counts.
    functions: tuple[JobFunction, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'functions', _replace_nil_slices(self.functions, 1))


ListJobFunctionsOutput.__annotations__['functions'] = tuple[JobFunction, ...] | None
ListJobFunctionsOutput.__dataclass_fields__['functions'].type = tuple[JobFunction, ...] | None
setattr(ListJobFunctionsOutput, '__module__', 'dex_connectors.moka.models')
