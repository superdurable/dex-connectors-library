# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public model names backed by Go-derived generated dataclasses."""

from ._generated.models import (
    Domain as Domain,
)
from ._generated.models import DomainsOutput as DomainsOutput
from ._generated.models import (
    Event as Event,
)
from ._generated.models import EventsOutput as EventsOutput
from ._generated.models import (
    GetEventsInput as GetEventsInput,
)
from ._generated.models import (
    ListDomainsInput as ListDomainsInput,
)

EventsOutput.__doc__ = "Events on the first page."
DomainsOutput.__doc__ = "Domains on the page and Mailgun's total domain count."

GetEventsInput.__doc__ = "Sending domain and events per page; zero limit uses Mailgun's default, at most 300."
Event.__doc__ = "Event ID, type such as delivered, and Unix timestamp in seconds."
ListDomainsInput.__doc__ = (
    "Domains per page (zero uses Mailgun's default, at most 1000) and the number to skip."
)
Domain.__doc__ = "Domain name and verification state such as active."
