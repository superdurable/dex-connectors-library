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
    new_create_speech_step as new_create_speech_step,
)
from ._generated.steps import (
    new_list_voices_step as new_list_voices_step,
)
from .connection import Connection as Connection
from .models import CreateSpeechInput as CreateSpeechInput
from .models import CreateSpeechOutput as CreateSpeechOutput
from .models import ListVoicesInput as ListVoicesInput
from .models import ListVoicesOutput as ListVoicesOutput

BranchTarget = TypeVar("BranchTarget")

new_list_voices_step.__doc__ = "Create the typed voice listing Step; unwired optional outcomes fail the Flow."
new_create_speech_step.__doc__ = (
    "Create the typed speech generation Step; unwired optional outcomes fail the Flow."
)
