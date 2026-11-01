# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed Vercel deployment pages and deployments."""

from dataclasses import dataclass, field


@dataclass(frozen=True, slots=True)
class ListDeploymentsInput:
    """Optional team ID; blank lists the personal account's deployments."""

    team_id: str = field(default="", metadata={"dex_json_name": "teamId", "dex_json_omit_default": True})


@dataclass(frozen=True, slots=True)
class Deployment:
    """Deployment identity, project name, hostname, states, and creation time in Unix milliseconds."""

    uid: str = ""
    id: str = ""
    name: str = ""
    url: str = ""
    state: str = ""
    ready_state: str = field(default="", metadata={"dex_json_name": "readyState"})
    created: int = 0


@dataclass(frozen=True, slots=True)
class Pagination:
    """Vercel's next-page timestamp cursor, or None on the last page."""

    next: int | None = None


@dataclass(frozen=True, slots=True)
class DeploymentsPage:
    """First page of deployments and its pagination cursor."""

    deployments: tuple[Deployment, ...] = ()
    pagination: Pagination = field(default_factory=Pagination)


@dataclass(frozen=True, slots=True)
class GetDeploymentInput:
    """Deployment ID or hostname, plus the optional owning team ID."""

    id_or_url: str = field(default="", metadata={"dex_json_name": "idOrUrl"})
    team_id: str = field(default="", metadata={"dex_json_name": "teamId", "dex_json_omit_default": True})
