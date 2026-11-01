# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public manifest-backed mock helpers."""

from dex_connectors.sdk import ConnectionRef as ConnectionRef
from dex_connectors.sdk import Failure as Failure
from dex_connectors.sdk import FailureKind as FailureKind
from dex_connectors.sdk.connectormock import Case as Case
from dex_connectors.sdk.connectormock import Query as Query

from .._generated.mock import Mock as Mock
from .._generated.mock import get_lead_default_answer as get_lead_default_answer
from .._generated.mock import get_lead_found as get_lead_found
from .._generated.mock import get_lead_invalid_key as get_lead_invalid_key
from .._generated.mock import get_lead_not_found as get_lead_not_found
from .._generated.mock import list_leads_default_answer as list_leads_default_answer
from .._generated.mock import list_leads_invalid_key as list_leads_invalid_key
from .._generated.mock import list_leads_listed as list_leads_listed
from ..client import CONNECTOR_ID as CONNECTOR_ID
from ..client import FOUND as FOUND
from ..client import GET_LEAD_DEFINITION as GET_LEAD_DEFINITION
from ..client import LIST_LEADS_DEFINITION as LIST_LEADS_DEFINITION
from ..client import LISTED as LISTED
from ..client import NOT_FOUND as NOT_FOUND
from ..client import PROVIDER_REJECTED as PROVIDER_REJECTED
from ..connection import Connection as Connection
from ..connection import new_connection_with_operations as new_connection_with_operations
from ..models import GetLeadInput as GetLeadInput
from ..models import Lead as Lead
from ..models import LeadPage as LeadPage
from ..models import ListLeadsInput as ListLeadsInput

__all__ = [
    "Mock",
    "list_leads_listed",
    "list_leads_default_answer",
    "list_leads_invalid_key",
    "get_lead_found",
    "get_lead_not_found",
    "get_lead_default_answer",
    "get_lead_invalid_key",
]

Mock.__doc__ = "Script Close Queries, defaulting to their manifest answers."

list_leads_listed.__doc__ = "Script the successful listed route."
list_leads_default_answer.__doc__ = "Return the manifest's listLeads defaultAnswer."
list_leads_invalid_key.__doc__ = "Return the manifest's listLeads invalidKey answer."
get_lead_found.__doc__ = "Script the successful found route."
get_lead_not_found.__doc__ = "Script a missing lead."
get_lead_default_answer.__doc__ = "Return the manifest's getLead defaultAnswer."
get_lead_invalid_key.__doc__ = "Return the manifest's getLead invalidKey answer."
