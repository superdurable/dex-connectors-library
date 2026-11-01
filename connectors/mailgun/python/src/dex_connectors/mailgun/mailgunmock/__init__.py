# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public manifest-backed mock helpers."""

from dex_connectors.sdk import ConnectionRef as ConnectionRef
from dex_connectors.sdk import Failure as Failure
from dex_connectors.sdk.connectormock import Case as Case
from dex_connectors.sdk.connectormock import Case as _Case
from dex_connectors.sdk.connectormock import Query as Query

from .._generated.mock import Mock as Mock
from .._generated.mock import get_events_listed as get_events_listed
from .._generated.mock import get_events_listed_default as _get_events_listed_default
from .._generated.mock import list_domains_listed as list_domains_listed
from .._generated.mock import list_domains_listed_default as _list_domains_listed_default
from ..client import CONNECTOR_ID as CONNECTOR_ID
from ..client import GET_EVENTS_DEFINITION as GET_EVENTS_DEFINITION
from ..client import LIST_DOMAINS_DEFINITION as LIST_DOMAINS_DEFINITION
from ..client import LISTED as LISTED
from ..connection import Connection as Connection
from ..connection import new_connection_with_operations as new_connection_with_operations
from ..models import Domain as Domain
from ..models import DomainsOutput as DomainsOutput
from ..models import DomainsOutput as _DomainsOutput
from ..models import Event as Event
from ..models import EventsOutput as EventsOutput
from ..models import EventsOutput as _EventsOutput
from ..models import GetEventsInput as GetEventsInput
from ..models import ListDomainsInput as ListDomainsInput


def get_events_default_answer() -> _Case[_EventsOutput]:
    """Return the manifest's getEvents default answer."""
    return _get_events_listed_default()


def list_domains_default_answer() -> _Case[_DomainsOutput]:
    """Return the manifest's listDomains default answer."""
    return _list_domains_listed_default()


__all__ = [
    "Mock",
    "get_events_listed",
    "get_events_default_answer",
    "list_domains_listed",
    "list_domains_default_answer",
]
