# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed Vercel deployment pages and deployments."""

from dataclasses import dataclass, field

from ._generated.models import Deployment as Deployment
from ._generated.models import GetDeploymentInput as GetDeploymentInput
from ._generated.models import ListDeploymentsInput as ListDeploymentsInput


@dataclass(frozen=True, slots=True)
class Pagination:
    """Vercel's next-page timestamp cursor, or None on the last page."""

    next: int | None = None


@dataclass(frozen=True, slots=True)
class DeploymentsPage:
    """First page of deployments and its pagination cursor."""

    deployments: tuple[Deployment, ...] = ()
    pagination: Pagination = field(default_factory=Pagination)


# connectorctl: model-alias DeploymentsPagePagination=Pagination

ListDeploymentsInput.__doc__ = "Optional team ID; blank lists the personal account's deployments."
Deployment.__doc__ = (
    "Deployment identity, project name, hostname, states, and creation time in Unix milliseconds."
)
GetDeploymentInput.__doc__ = "Deployment ID or hostname, plus the optional owning team ID."
