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


# ListClustersInput requests the first page of clusters visible to the API Key.
@dataclass(frozen=True, slots=True)
class ListClustersInput:
    pass


setattr(ListClustersInput, '__module__', 'dex_connectors.zilliz.models')


# ClusterSummary is the nonsecret metadata of one Zilliz Cloud cluster.
@dataclass(frozen=True, slots=True)
class ClusterSummary:
    # ClusterID identifies the cluster.
    cluster_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'clusterID'})
    # Name is the cluster's display name.
    name: str = ''
    # RegionID is the cloud region identifier.
    region_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'regionID'})
    # Plan is the subscription plan.
    plan: str = ''
    # Status is the provider's cluster lifecycle status.
    status: str = ''


setattr(ClusterSummary, '__module__', 'dex_connectors.zilliz.models')


# ListClustersOutput contains the total count and first 25 clusters.
@dataclass(frozen=True, slots=True)
class ListClustersOutput:
    # Total is the provider's count of visible clusters.
    total: int = 0
    # Clusters are the first page of visible cluster summaries.
    clusters: tuple[ClusterSummary, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'clusters', _replace_nil_slices(self.clusters, 1))


ListClustersOutput.__annotations__['clusters'] = tuple[ClusterSummary, ...] | None
ListClustersOutput.__dataclass_fields__['clusters'].type = tuple[ClusterSummary, ...] | None
setattr(ListClustersOutput, '__module__', 'dex_connectors.zilliz.models')
