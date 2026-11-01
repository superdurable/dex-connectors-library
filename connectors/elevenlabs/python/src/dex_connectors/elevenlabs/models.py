# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public model names backed by Go-derived generated dataclasses."""

from ._generated.models import (
    CreateSpeechInput as CreateSpeechInput,
)
from ._generated.models import (
    CreateSpeechOutput as CreateSpeechOutput,
)
from ._generated.models import (
    ListVoicesInput as ListVoicesInput,
)
from ._generated.models import ListVoicesOutput as ListVoicesOutput
from ._generated.models import (
    Voice as Voice,
)

ListVoicesOutput.__doc__ = "One voice page and the token that selects the next page when has_more is true."
