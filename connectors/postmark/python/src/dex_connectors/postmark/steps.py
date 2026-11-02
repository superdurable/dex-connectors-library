# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public operation-specific Step factories from connector.yaml."""

from collections.abc import Callable as Callable
from collections.abc import Mapping as Mapping
from typing import TypeVar as TypeVar

import dex as dex
from dex_connectors.sdk import QueryResult as QueryResult
from dex_connectors.sdk import QueryStep as QueryStep
from dex_connectors.sdk import QueryStepConfig as QueryStepConfig
from dex_connectors.sdk import StepAnnotations as StepAnnotations

from ._generated.steps import (
    StepInput as StepInput,
)
from ._generated.steps import (
    new_get_message_details_step as new_get_message_details_step,
)
from ._generated.steps import (
    new_list_outbound_messages_step as new_list_outbound_messages_step,
)
from .connection import Connection as Connection
from .models import GetMessageDetailsInput as GetMessageDetailsInput
from .models import ListOutboundMessagesInput as ListOutboundMessagesInput
from .models import MessageDetails as MessageDetails
from .models import MessagePage as MessagePage

BranchTarget = TypeVar("BranchTarget")

new_list_outbound_messages_step.__doc__ = (
    "Create the typed outbound message search Step; unwired optional outcomes fail the Flow."
)
new_get_message_details_step.__doc__ = (
    "Create the typed message details Step; unwired optional outcomes fail the Flow."
)
