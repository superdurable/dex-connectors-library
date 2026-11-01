# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated Python dataclasses from Go operation types."""

from dataclasses import dataclass, field as _dataclass_field

@dataclass(frozen=True, slots=True)
class ListDeploymentsInput:
    """Typed ListDeploymentsInput value from the Go connector contract."""

    team_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'teamId', 'dex_json_omit_default': True})


setattr(ListDeploymentsInput, '__module__', 'dex_connectors.vercel.models')


@dataclass(frozen=True, slots=True)
class Deployment:
    """Typed Deployment value from the Go connector contract."""

    uid: str = ''
    id: str = ''
    name: str = ''
    url: str = ''
    state: str = ''
    ready_state: str = _dataclass_field(default='', metadata={'dex_json_name': 'readyState'})
    created: int = 0


setattr(Deployment, '__module__', 'dex_connectors.vercel.models')


@dataclass(frozen=True, slots=True)
class DeploymentsPagePagination:
    """Typed DeploymentsPagePagination value from the Go connector contract."""

    next: int | None = None


setattr(DeploymentsPagePagination, '__module__', 'dex_connectors.vercel.models')


@dataclass(frozen=True, slots=True)
class DeploymentsPage:
    """Typed DeploymentsPage value from the Go connector contract."""

    deployments: tuple[Deployment, ...] | None = None
    pagination: DeploymentsPagePagination = _dataclass_field(default_factory=DeploymentsPagePagination)


setattr(DeploymentsPage, '__module__', 'dex_connectors.vercel.models')


@dataclass(frozen=True, slots=True)
class GetDeploymentInput:
    """Typed GetDeploymentInput value from the Go connector contract."""

    id_or_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'idOrUrl'})
    team_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'teamId', 'dex_json_omit_default': True})


setattr(GetDeploymentInput, '__module__', 'dex_connectors.vercel.models')
