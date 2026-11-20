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


# ListAgentsInput selects a one-based agent page; zero values use page 1 and size 20.
@dataclass(frozen=True, slots=True)
class ListAgentsInput:
    # Page is the one-based page number.
    page: int = 0
    # PageSize is the number of agents requested, from 1 to 100.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize'})


setattr(ListAgentsInput, '__module__', 'dex_connectors.useasy.models')


# AgentSummary excludes passwords and personal contact details returned by the provider.
@dataclass(frozen=True, slots=True)
class AgentSummary:
    # ID is the provider's stable agent ID.
    id: str = ''
    # Number is the agent's work number.
    number: str = ''
    # Enabled reports whether the agent is active.
    enabled: bool = False


setattr(AgentSummary, '__module__', 'dex_connectors.useasy.models')


# ListAgentsOutput contains one page of agent metadata and the provider's page counts.
@dataclass(frozen=True, slots=True)
class ListAgentsOutput:
    # Agents are returned in provider order.
    agents: tuple[AgentSummary, ...] = ()
    # Page is the one-based page number returned by Useasy.
    page: int = 0
    # Total is the number of matching agents.
    total: int = 0
    # TotalPages is the number of available pages.
    total_pages: int = _dataclass_field(default=0, metadata={'dex_json_name': 'totalPages'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'agents', _replace_nil_slices(self.agents, 1))


ListAgentsOutput.__annotations__['agents'] = tuple[AgentSummary, ...] | None
ListAgentsOutput.__dataclass_fields__['agents'].type = tuple[AgentSummary, ...] | None
setattr(ListAgentsOutput, '__module__', 'dex_connectors.useasy.models')
