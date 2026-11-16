# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""LinkedIn OpenID Connect connector for Dex Python applications.

The getAuthenticatedProfile Query reads the connected member's verified UserInfo profile, and the
profileChanged poll Trigger delivers each change of that member's own profile claims.
"""

from .client import MINIMUM_POLL_INTERVAL as MINIMUM_POLL_INTERVAL
from .client import Client as Client
from .client import Config as Config
from .client import Credentials as Credentials
from .connection import Connection as Connection
from .connection import new_connection as new_connection
from .connection import new_connection_with_operations as new_connection_with_operations
from .connection import new_project_connection as new_project_connection
from .models import AuthenticatedProfile as AuthenticatedProfile
from .models import GetAuthenticatedProfileInput as GetAuthenticatedProfileInput
from .models import ProfileChangedEvent as ProfileChangedEvent
from .models import ProfileChangedTriggerConfiguration as ProfileChangedTriggerConfiguration
from .models import ProfileClaims as ProfileClaims
from .steps import new_get_authenticated_profile_step as new_get_authenticated_profile_step
from .triggers import PROFILE_CHANGED_TRIGGER_DEFINITION as PROFILE_CHANGED_TRIGGER_DEFINITION
from .triggers import ProfileChangedTriggerRoute as ProfileChangedTriggerRoute
from .triggers import ProfileField as ProfileField
from .triggers import ProjectProfileChangedTriggerRoute as ProjectProfileChangedTriggerRoute
from .triggers import decode_profile_changed_event_payload as decode_profile_changed_event_payload
from .triggers import (
    decode_profile_changed_trigger_configuration as decode_profile_changed_trigger_configuration,
)
from .triggers import define_profile_changed_trigger_binding as define_profile_changed_trigger_binding
from .triggers import new_profile_changed_trigger as new_profile_changed_trigger
from .triggers import new_profile_trigger_runner as new_profile_trigger_runner
from .triggers import new_project_profile_trigger_runner as new_project_profile_trigger_runner

__all__ = [
    "MINIMUM_POLL_INTERVAL",
    "PROFILE_CHANGED_TRIGGER_DEFINITION",
    "AuthenticatedProfile",
    "Client",
    "Config",
    "Connection",
    "Credentials",
    "GetAuthenticatedProfileInput",
    "ProfileChangedEvent",
    "ProfileChangedTriggerConfiguration",
    "ProfileChangedTriggerRoute",
    "ProfileClaims",
    "ProfileField",
    "ProjectProfileChangedTriggerRoute",
    "decode_profile_changed_event_payload",
    "decode_profile_changed_trigger_configuration",
    "define_profile_changed_trigger_binding",
    "new_connection",
    "new_connection_with_operations",
    "new_get_authenticated_profile_step",
    "new_profile_changed_trigger",
    "new_profile_trigger_runner",
    "new_project_profile_trigger_runner",
]
