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


# ListRunningAgentsInput has no parameters; the connection identifies the project.
@dataclass(frozen=True, slots=True)
class ListRunningAgentsInput:
    pass


setattr(ListRunningAgentsInput, '__module__', 'dex_connectors.agora.models')


# RunningAgent describes one agent in Agora's list response.
@dataclass(frozen=True, slots=True)
class RunningAgent:
    # AgentID identifies the running agent within Agora.
    agent_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'agentId'})
    # Status is RUNNING for the default list filter.
    status: str = ''
    # StartTimestamp is the agent creation time in Unix seconds.
    start_timestamp: int = _dataclass_field(default=0, metadata={'dex_json_name': 'startTimestamp'})


setattr(RunningAgent, '__module__', 'dex_connectors.agora.models')


# RunningAgents is one page of Agora Conversational AI agents.
@dataclass(frozen=True, slots=True)
class RunningAgents:
    # Count is the number of agents in this page.
    count: int = 0
    # Agents holds the current page, limited by Agora to its default 20 entries.
    agents: tuple[RunningAgent, ...] = ()
    # Total is the number of matching agents across pages.
    total: int = 0
    # Cursor is Agora’s cursor for the next page, if any.
    cursor: str = ''

    def __post_init__(self) -> None:
        object.__setattr__(self, 'agents', _replace_nil_slices(self.agents, 1))


RunningAgents.__annotations__['agents'] = tuple[RunningAgent, ...] | None
RunningAgents.__dataclass_fields__['agents'].type = tuple[RunningAgent, ...] | None
setattr(RunningAgents, '__module__', 'dex_connectors.agora.models')
