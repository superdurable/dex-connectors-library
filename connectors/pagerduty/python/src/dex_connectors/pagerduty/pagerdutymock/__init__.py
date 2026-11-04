# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public manifest-backed mock helpers."""

from dex_connectors.sdk import ConnectionRef as ConnectionRef
from dex_connectors.sdk import Failure as Failure
from dex_connectors.sdk.connectormock import Case as Case
from dex_connectors.sdk.connectormock import Query as Query

from .._generated.mock import Mock as Mock
from .._generated.mock import get_incident_default as get_incident_default
from .._generated.mock import get_incident_found as get_incident_found
from .._generated.mock import get_incident_not_found as get_incident_not_found
from .._generated.mock import list_incidents_default as list_incidents_default
from .._generated.mock import list_incidents_listed as list_incidents_listed
from .._generated.mock import list_incidents_provider_rejected as list_incidents_provider_rejected
from ..client import CONNECTOR_ID as CONNECTOR_ID
from ..client import FOUND as FOUND
from ..client import GET_INCIDENT_DEFINITION as GET_INCIDENT_DEFINITION
from ..client import LIST_INCIDENTS_DEFINITION as LIST_INCIDENTS_DEFINITION
from ..client import LISTED as LISTED
from ..client import NOT_FOUND as NOT_FOUND
from ..client import PROVIDER_REJECTED as PROVIDER_REJECTED
from ..connection import Connection as Connection
from ..connection import new_connection_with_operations as new_connection_with_operations
from ..models import GetIncidentInput as GetIncidentInput
from ..models import Incident as Incident
from ..models import IncidentPage as IncidentPage
from ..models import ListIncidentsInput as ListIncidentsInput

__all__ = [
    "Mock",
    "list_incidents_listed",
    "list_incidents_provider_rejected",
    "list_incidents_default",
    "get_incident_found",
    "get_incident_not_found",
    "get_incident_default",
]

Mock.__doc__ = "Script PagerDuty operations, defaulting to their manifest answers."

list_incidents_listed.__doc__ = "Script the successful listed route."
list_incidents_provider_rejected.__doc__ = "Script a definite provider rejection of the list."
list_incidents_default.__doc__ = "Return the manifest's listIncidentsDefault answer."
get_incident_found.__doc__ = "Script the successful found route."
get_incident_not_found.__doc__ = "Script a missing incident."
get_incident_default.__doc__ = "Return the manifest's getIncidentDefault answer."
