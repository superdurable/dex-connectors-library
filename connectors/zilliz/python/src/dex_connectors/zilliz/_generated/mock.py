# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ClusterSummary, ListClustersInput, ListClustersOutput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, INVALID_RESPONSE, LIST_CLUSTERS_DEFINITION, PROVIDER_REJECTED

def list_clusters_found(value: ListClustersOutput, failure: Failure | None = None) -> Case[ListClustersOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(list_clusters_found, '__module__', "dex_connectors.zilliz.zillizmock")

def list_clusters_provider_rejected(value: ListClustersOutput, failure: Failure | None = None) -> Case[ListClustersOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_clusters_provider_rejected, '__module__', "dex_connectors.zilliz.zillizmock")

def list_clusters_invalid_response(value: ListClustersOutput, failure: Failure | None = None) -> Case[ListClustersOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_clusters_invalid_response, '__module__', "dex_connectors.zilliz.zillizmock")

def list_clusters_defect(value: ListClustersOutput, failure: Failure | None = None) -> Case[ListClustersOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_clusters_defect, '__module__', "dex_connectors.zilliz.zillizmock")

def list_clusters_retry(failure: Failure, after_seconds: float = 0) -> Case[ListClustersOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_clusters_retry, '__module__', "dex_connectors.zilliz.zillizmock")

def list_clusters_one_cluster() -> Case[ListClustersOutput]:
    return Case.branch_result(FOUND, ListClustersOutput(total=1, clusters=(ClusterSummary(cluster_id='inxx-example', name='example-cluster', region_id='aws-us-west-2', plan='Enterprise', status='RUNNING'),)))

setattr(list_clusters_one_cluster, '__module__', "dex_connectors.zilliz.zillizmock")

def list_clusters_no_clusters() -> Case[ListClustersOutput]:
    return Case.branch_result(FOUND, ListClustersOutput(total=0, clusters=()))

setattr(list_clusters_no_clusters, '__module__', "dex_connectors.zilliz.zillizmock")


class Mock:

    def __init__(self, connection_name: str = "zilliz") -> None:
        self.list_clusters_script: Query[ListClustersInput, ListClustersOutput] = Query(LIST_CLUSTERS_DEFINITION)
        self.list_clusters_script.default(list_clusters_one_cluster())
        self._connection = new_connection_with_operations(
            list_clusters=self.list_clusters_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.zilliz.zillizmock")

__all__ = [
    "Mock",
    "list_clusters_found",
    "list_clusters_provider_rejected",
    "list_clusters_invalid_response",
    "list_clusters_defect",
    "list_clusters_retry",
    "list_clusters_one_cluster",
    "list_clusters_no_clusters",
]
