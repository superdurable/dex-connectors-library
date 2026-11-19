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


# ListDeploymentsInput has no operation parameters; the key's project scope selects deployments.
@dataclass(frozen=True, slots=True)
class ListDeploymentsInput:
    pass


setattr(ListDeploymentsInput, '__module__', 'dex_connectors.emq.models')


# Deployment is a bounded view of one EMQX Cloud deployment.
@dataclass(frozen=True, slots=True)
class Deployment:
    # DeploymentID is the provider's stable deployment identifier.
    deployment_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'deploymentId'})
    # DeploymentName is the console display name.
    deployment_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'deploymentName'})
    # DeploymentType identifies the serverless or dedicated plan.
    deployment_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'deploymentType'})
    # ProjectName is the containing project name.
    project_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'projectName'})
    # Region is the provider's region label.
    region: str = ''
    # Status is the provider-reported lifecycle state.
    status: str = ''
    # Version is the EMQX major version label.
    version: str = ''
    # Platform is the cloud provider label.
    platform: str = ''
    # Connections is the deployment's connection capacity.
    connections: int = 0
    # TPS is the deployment's transactions-per-second capacity.
    tps: int = 0
    # CreatedAt is the provider's creation time string.
    created_at: str = _dataclass_field(default='', metadata={'dex_json_name': 'createdAt'})


setattr(Deployment, '__module__', 'dex_connectors.emq.models')


# ListDeploymentsOutput contains zero or more scoped deployments.
@dataclass(frozen=True, slots=True)
class ListDeploymentsOutput:
    # Deployments is empty when the key can access no deployments.
    deployments: tuple[Deployment, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'deployments', _replace_nil_slices(self.deployments, 1))


ListDeploymentsOutput.__annotations__['deployments'] = tuple[Deployment, ...] | None
ListDeploymentsOutput.__dataclass_fields__['deployments'].type = tuple[Deployment, ...] | None
setattr(ListDeploymentsOutput, '__module__', 'dex_connectors.emq.models')
