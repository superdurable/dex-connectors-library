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
