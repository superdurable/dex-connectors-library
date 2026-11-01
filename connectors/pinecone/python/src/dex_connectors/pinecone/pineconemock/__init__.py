# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public manifest-backed mock helpers."""

from dex_connectors.sdk import ConnectionRef as ConnectionRef
from dex_connectors.sdk import Failure as Failure
from dex_connectors.sdk.connectormock import Case as Case
from dex_connectors.sdk.connectormock import Case as _Case
from dex_connectors.sdk.connectormock import Mutation as Mutation
from dex_connectors.sdk.connectormock import Query as Query

from .._generated.mock import Mock as Mock
from .._generated.mock import query_vectors_default_query_vectors as _query_vectors_default_query_vectors
from .._generated.mock import query_vectors_queried as query_vectors_queried
from .._generated.mock import upsert_vectors_default_upsert_vectors as _upsert_vectors_default_upsert_vectors
from .._generated.mock import upsert_vectors_upserted as upsert_vectors_upserted
from ..client import CONNECTOR_ID as CONNECTOR_ID
from ..client import QUERIED as QUERIED
from ..client import QUERY_VECTORS_DEFINITION as QUERY_VECTORS_DEFINITION
from ..client import UPSERT_VECTORS_DEFINITION as UPSERT_VECTORS_DEFINITION
from ..client import UPSERTED as UPSERTED
from ..connection import Connection as Connection
from ..connection import new_connection_with_operations as new_connection_with_operations
from ..models import QueryVectorsInput as QueryVectorsInput
from ..models import QueryVectorsOutput as QueryVectorsOutput
from ..models import QueryVectorsOutput as _QueryVectorsOutput
from ..models import UpsertVectorsInput as UpsertVectorsInput
from ..models import UpsertVectorsOutput as UpsertVectorsOutput
from ..models import UpsertVectorsOutput as _UpsertVectorsOutput


def query_vectors_default_answer() -> _Case[_QueryVectorsOutput]:
    """Return the manifest's queryVectors default answer."""
    return _query_vectors_default_query_vectors()


def upsert_vectors_default_answer() -> _Case[_UpsertVectorsOutput]:
    """Return the manifest's upsertVectors default answer."""
    return _upsert_vectors_default_upsert_vectors()


__all__ = [
    "Mock",
    "upsert_vectors_upserted",
    "upsert_vectors_default_answer",
    "query_vectors_queried",
    "query_vectors_default_answer",
]
