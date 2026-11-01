# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public model names backed by Go-derived generated dataclasses."""

from ._generated.models import (
    GetMessageDetailsInput as GetMessageDetailsInput,
)
from ._generated.models import (
    ListOutboundMessagesInput as ListOutboundMessagesInput,
)
from ._generated.models import (
    MessageDetails as MessageDetails,
)
from ._generated.models import MessagePage as MessagePage

MessagePage.__doc__ = "One page of outbound messages and Postmark's total match count."

ListOutboundMessagesInput.__doc__ = (
    "One page of outbound messages; count zero means 50, and count plus offset is at most 10,000."
)
GetMessageDetailsInput.__doc__ = "Postmark outbound message ID."
MessageDetails.__doc__ = "Outbound message summary with Postmark's delivery status text."
