# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Go-derived Agora request and response models."""

from ._generated.models import ListRunningAgentsInput as ListRunningAgentsInput
from ._generated.models import RunningAgent as RunningAgent
from ._generated.models import RunningAgents as RunningAgents

ListRunningAgentsInput.__doc__ = "Request the project bound to the current connection."
RunningAgent.__doc__ = "A running Agora agent with its start time in Unix seconds."
RunningAgents.__doc__ = "One page of running agents and the next-page cursor."
