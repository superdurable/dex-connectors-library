# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Dex-native Microsoft Outlook Mail connector."""

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from .client import Client as Client
from .connection import Connection as Connection
from .connection import new_connection as new_connection
from .connection import new_project_connection as new_project_connection
from .models import (
    Attachment as Attachment,
)
from .models import (
    EmailAddress as EmailAddress,
)
from .models import (
    GetMessageInput as GetMessageInput,
)
from .models import (
    Message as Message,
)
from .models import (
    MessageFlags as MessageFlags,
)
from .models import (
    MessageSummary as MessageSummary,
)
from .models import (
    MovedMessage as MovedMessage,
)
from .models import (
    MoveMessageInput as MoveMessageInput,
)
from .models import (
    ReplyToMessageInput as ReplyToMessageInput,
)
from .models import (
    SearchMessagesInput as SearchMessagesInput,
)
from .models import (
    SearchMessagesOutput as SearchMessagesOutput,
)
from .models import (
    SendMessageInput as SendMessageInput,
)
from .models import (
    SentMessage as SentMessage,
)
from .models import (
    SetMessageFlagsInput as SetMessageFlagsInput,
)
from .steps import new_get_message_step as new_get_message_step
from .steps import new_move_message_step as new_move_message_step
from .steps import new_reply_to_message_step as new_reply_to_message_step
from .steps import new_search_messages_step as new_search_messages_step
from .steps import new_send_message_step as new_send_message_step
from .steps import new_set_message_flags_step as new_set_message_flags_step
