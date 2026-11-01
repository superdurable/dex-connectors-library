# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public manifest-backed mock helpers."""

from dex_connectors.sdk import ConnectionRef as ConnectionRef
from dex_connectors.sdk import Failure as Failure
from dex_connectors.sdk import FailureKind as FailureKind
from dex_connectors.sdk.connectormock import Case as Case
from dex_connectors.sdk.connectormock import Query as Query

from .._generated.mock import Mock as Mock
from .._generated.mock import list_projects_default_answer as list_projects_default_answer
from .._generated.mock import list_projects_invalid_key as list_projects_invalid_key
from .._generated.mock import list_projects_listed as list_projects_listed
from .._generated.mock import list_time_entries_default_answer as list_time_entries_default_answer
from .._generated.mock import list_time_entries_invalid_key as list_time_entries_invalid_key
from .._generated.mock import list_time_entries_listed as list_time_entries_listed
from ..client import CONNECTOR_ID as CONNECTOR_ID
from ..client import LIST_PROJECTS_DEFINITION as LIST_PROJECTS_DEFINITION
from ..client import LIST_TIME_ENTRIES_DEFINITION as LIST_TIME_ENTRIES_DEFINITION
from ..client import LISTED as LISTED
from ..client import PROVIDER_REJECTED as PROVIDER_REJECTED
from ..connection import Connection as Connection
from ..connection import new_connection_with_operations as new_connection_with_operations
from ..models import ListProjectsInput as ListProjectsInput
from ..models import ListTimeEntriesInput as ListTimeEntriesInput
from ..models import Project as Project
from ..models import ProjectPage as ProjectPage
from ..models import TimeEntry as TimeEntry
from ..models import TimeEntryPage as TimeEntryPage
from ..models import TimeEntryProject as TimeEntryProject

__all__ = [
    "Mock",
    "list_projects_listed",
    "list_projects_default_answer",
    "list_projects_invalid_key",
    "list_time_entries_listed",
    "list_time_entries_default_answer",
    "list_time_entries_invalid_key",
]

Mock.__doc__ = "Script Harvest Queries, defaulting to their manifest answers."

list_projects_listed.__doc__ = "Script the successful listed route."
list_projects_default_answer.__doc__ = "Return the manifest's listProjects defaultAnswer."
list_projects_invalid_key.__doc__ = "Return the manifest's listProjects invalidKey answer."
list_time_entries_listed.__doc__ = "Script the successful listed route."
list_time_entries_default_answer.__doc__ = "Return the manifest's listTimeEntries defaultAnswer."
list_time_entries_invalid_key.__doc__ = "Return the manifest's listTimeEntries invalidKey answer."
