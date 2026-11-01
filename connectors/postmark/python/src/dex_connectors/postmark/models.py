# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public model names backed by Go-derived generated dataclasses."""

from dataclasses import dataclass, field

from ._generated.models import (
    GetMessageDetailsInput as GetMessageDetailsInput,
)
from ._generated.models import (
    ListOutboundMessagesInput as ListOutboundMessagesInput,
)
from ._generated.models import (
    MessageDetails as MessageDetails,
)


@dataclass(frozen=True, slots=True)
class MessagePage:
    """One page of outbound messages and Postmark's total match count."""

    total_count: int = field(default=0, metadata={"dex_json_name": "TotalCount"})
    messages: tuple[MessageDetails, ...] = field(default=(), metadata={"dex_json_name": "Messages"})
