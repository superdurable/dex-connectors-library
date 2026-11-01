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
from .._generated.mock import create_or_update_contact_saved as create_or_update_contact_saved
from .._generated.mock import (
    create_or_update_contact_saved_default_contact as _create_or_update_contact_saved_default_contact,
)
from .._generated.mock import get_contact_found as get_contact_found
from .._generated.mock import get_contact_found_default_contact as _get_contact_found_default_contact
from .._generated.mock import get_contact_not_found as get_contact_not_found
from ..client import CONNECTOR_ID as CONNECTOR_ID
from ..client import CREATE_OR_UPDATE_CONTACT_DEFINITION as CREATE_OR_UPDATE_CONTACT_DEFINITION
from ..client import FOUND as FOUND
from ..client import GET_CONTACT_DEFINITION as GET_CONTACT_DEFINITION
from ..client import NOT_FOUND as NOT_FOUND
from ..client import SAVED as SAVED
from ..connection import Connection as Connection
from ..connection import new_connection_with_operations as new_connection_with_operations
from ..models import Contact as Contact
from ..models import Contact as _Contact
from ..models import ContactWriteResult as ContactWriteResult
from ..models import ContactWriteResult as _ContactWriteResult
from ..models import CreateOrUpdateContactInput as CreateOrUpdateContactInput
from ..models import GetContactInput as GetContactInput


def create_or_update_contact_default_answer() -> _Case[_ContactWriteResult]:
    """Return the manifest's createOrUpdateContact default answer."""
    return _create_or_update_contact_saved_default_contact()


def get_contact_default_answer() -> _Case[_Contact]:
    """Return the manifest's getContact default answer."""
    return _get_contact_found_default_contact()


__all__ = [
    "Mock",
    "create_or_update_contact_saved",
    "create_or_update_contact_default_answer",
    "get_contact_found",
    "get_contact_not_found",
    "get_contact_default_answer",
]
