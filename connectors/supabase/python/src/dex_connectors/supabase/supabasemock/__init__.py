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
from .._generated.mock import select_rows_default_select_rows as _select_rows_default_select_rows
from .._generated.mock import select_rows_selected as select_rows_selected
from .._generated.mock import upsert_rows_default_upsert_rows as _upsert_rows_default_upsert_rows
from .._generated.mock import upsert_rows_upserted as upsert_rows_upserted
from ..client import CONNECTOR_ID as CONNECTOR_ID
from ..client import SELECT_ROWS_DEFINITION as SELECT_ROWS_DEFINITION
from ..client import SELECTED as SELECTED
from ..client import UPSERT_ROWS_DEFINITION as UPSERT_ROWS_DEFINITION
from ..client import UPSERTED as UPSERTED
from ..connection import Connection as Connection
from ..connection import new_connection_with_operations as new_connection_with_operations
from ..models import SelectRowsInput as SelectRowsInput
from ..models import SelectRowsOutput as SelectRowsOutput
from ..models import SelectRowsOutput as _SelectRowsOutput
from ..models import UpsertRowsInput as UpsertRowsInput
from ..models import UpsertRowsOutput as UpsertRowsOutput
from ..models import UpsertRowsOutput as _UpsertRowsOutput


def select_rows_default_answer() -> _Case[_SelectRowsOutput]:
    """Return the manifest's selectRows default answer."""
    return _select_rows_default_select_rows()


def upsert_rows_default_answer() -> _Case[_UpsertRowsOutput]:
    """Return the manifest's upsertRows default answer."""
    return _upsert_rows_default_upsert_rows()


__all__ = [
    "Mock",
    "select_rows_selected",
    "select_rows_default_answer",
    "upsert_rows_upserted",
    "upsert_rows_default_answer",
]
