# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public model names backed by Go-derived generated dataclasses."""

from ._generated.models import (
    GetLeadInput as GetLeadInput,
)
from ._generated.models import (
    Lead as Lead,
)
from ._generated.models import LeadPage as LeadPage
from ._generated.models import (
    ListLeadsInput as ListLeadsInput,
)

LeadPage.__doc__ = "One page of leads; has_more reports whether Close has another page."

ListLeadsInput.__doc__ = "Offset page of leads; limit zero means 100, at most 100."
GetLeadInput.__doc__ = "Close lead ID, which starts with lead_."
Lead.__doc__ = "Close lead identity and display name."
