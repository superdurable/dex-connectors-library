# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public model names backed by Go-derived generated dataclasses."""

from ._generated.models import (
    CreateEventInput as CreateEventInput,
)
from ._generated.models import (
    EventAcceptance as EventAcceptance,
)
from ._generated.models import (
    GetProfileInput as GetProfileInput,
)
from ._generated.models import (
    Profile as Profile,
)

GetProfileInput.__doc__ = "Klaviyo profile ID; it must not contain /, ?, &, or #."
Profile.__doc__ = "Profile ID and email address, which can be blank."
CreateEventInput.__doc__ = "Profile email, metric name, and optional string event properties."
EventAcceptance.__doc__ = "Whether Klaviyo accepted the event for asynchronous processing."
