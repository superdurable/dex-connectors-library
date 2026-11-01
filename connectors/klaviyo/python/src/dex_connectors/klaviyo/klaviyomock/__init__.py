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
from .._generated.mock import create_event_accepted as create_event_accepted
from .._generated.mock import create_event_accepted_default_event as _create_event_accepted_default_event
from .._generated.mock import get_profile_found as get_profile_found
from .._generated.mock import get_profile_found_default_profile as _get_profile_found_default_profile
from .._generated.mock import get_profile_not_found as get_profile_not_found
from ..client import ACCEPTED as ACCEPTED
from ..client import CONNECTOR_ID as CONNECTOR_ID
from ..client import CREATE_EVENT_DEFINITION as CREATE_EVENT_DEFINITION
from ..client import FOUND as FOUND
from ..client import GET_PROFILE_DEFINITION as GET_PROFILE_DEFINITION
from ..client import NOT_FOUND as NOT_FOUND
from ..connection import Connection as Connection
from ..connection import new_connection_with_operations as new_connection_with_operations
from ..models import CreateEventInput as CreateEventInput
from ..models import EventAcceptance as EventAcceptance
from ..models import EventAcceptance as _EventAcceptance
from ..models import GetProfileInput as GetProfileInput
from ..models import Profile as Profile
from ..models import Profile as _Profile


def create_event_default_answer() -> _Case[_EventAcceptance]:
    """Return the manifest's createEvent default answer."""
    return _create_event_accepted_default_event()


def get_profile_default_answer() -> _Case[_Profile]:
    """Return the manifest's getProfile default answer."""
    return _get_profile_found_default_profile()


__all__ = [
    "Mock",
    "get_profile_found",
    "get_profile_not_found",
    "get_profile_default_answer",
    "create_event_accepted",
    "create_event_default_answer",
]

Mock.__doc__ = "Script Klaviyo operations, defaulting to their manifest answers."

get_profile_found.__doc__ = "Script the successful found route."
get_profile_not_found.__doc__ = "Script a missing profile."
create_event_accepted.__doc__ = "Script the accepted route."
