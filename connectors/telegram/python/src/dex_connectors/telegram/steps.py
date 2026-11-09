# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public operation-specific Telegram Step factories."""

from collections.abc import Callable as Callable
from collections.abc import Mapping as Mapping
from typing import TypeVar as TypeVar

import dex as dex
from dex_connectors.sdk import MutationResult as MutationResult
from dex_connectors.sdk import MutationStep as MutationStep
from dex_connectors.sdk import MutationStepConfig as MutationStepConfig
from dex_connectors.sdk import QueryResult as QueryResult
from dex_connectors.sdk import QueryStep as QueryStep
from dex_connectors.sdk import QueryStepConfig as QueryStepConfig
from dex_connectors.sdk import StepAnnotations as StepAnnotations

from ._generated.steps import StepInput as StepInput
from ._generated.steps import new_get_chat_step as new_get_chat_step
from ._generated.steps import new_send_message_step as new_send_message_step
from .connection import Connection as Connection
from .models import Chat as Chat
from .models import GetChatInput as GetChatInput
from .models import SendMessageInput as SendMessageInput
from .models import SentMessage as SentMessage

BranchTarget = TypeVar("BranchTarget")

new_get_chat_step.__doc__ = "Create the typed chat metadata Query Step."
new_send_message_step.__doc__ = "Create the typed single-dispatch text Mutation Step."
