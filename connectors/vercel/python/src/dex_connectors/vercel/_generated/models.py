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


# ListDeploymentsInput optionally selects a Vercel team ID.
@dataclass(frozen=True, slots=True)
class ListDeploymentsInput:
    # TeamID is the team ID; blank uses the personal account.
    team_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'teamId', 'dex_json_omit_default': True})


setattr(ListDeploymentsInput, '__module__', 'dex_connectors.vercel.models')


# Deployment is a Vercel deployment summary.
@dataclass(frozen=True, slots=True)
class Deployment:
    # UID is the deployment identifier returned by the list endpoint.
    uid: str = ''
    # ID is the identifier returned by some deployment detail responses.
    id: str = ''
    # Name is the Vercel project name.
    name: str = ''
    # URL is the deployment hostname without a scheme.
    url: str = ''
    # State is the list endpoint's deployment state.
    state: str = ''
    # ReadyState is the detail endpoint's deployment state.
    ready_state: str = _dataclass_field(default='', metadata={'dex_json_name': 'readyState'})
    # Created is the creation timestamp in Unix milliseconds.
    created: int = 0


setattr(Deployment, '__module__', 'dex_connectors.vercel.models')


@dataclass(frozen=True, slots=True)
class DeploymentsPagePagination:
    next: int | None = None


setattr(DeploymentsPagePagination, '__module__', 'dex_connectors.vercel.models')


# DeploymentsPage contains Vercel's deployments and pagination cursor.
@dataclass(frozen=True, slots=True)
class DeploymentsPage:
    # Deployments holds the listed deployments.
    deployments: tuple[Deployment, ...] = ()
    # Pagination contains the next cursor when more results exist.
    pagination: DeploymentsPagePagination = _dataclass_field(default_factory=DeploymentsPagePagination)

    def __post_init__(self) -> None:
        object.__setattr__(self, 'deployments', _replace_nil_slices(self.deployments, 1))


DeploymentsPage.__annotations__['deployments'] = tuple[Deployment, ...] | None
DeploymentsPage.__dataclass_fields__['deployments'].type = tuple[Deployment, ...] | None
setattr(DeploymentsPage, '__module__', 'dex_connectors.vercel.models')


# GetDeploymentInput identifies one deployment and an optional team.
@dataclass(frozen=True, slots=True)
class GetDeploymentInput:
    # IDOrURL is a deployment ID or hostname.
    id_or_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'idOrUrl'})
    # TeamID is the team ID; blank uses the personal account.
    team_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'teamId', 'dex_json_omit_default': True})


setattr(GetDeploymentInput, '__module__', 'dex_connectors.vercel.models')
