# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public manifest-backed mock helpers."""

from .._generated.mock import Mock as Mock
from .._generated.mock import create_or_update_contact_saved as create_or_update_contact_saved
from .._generated.mock import (
    create_or_update_contact_saved_default_contact as create_or_update_contact_default_answer,
)
from .._generated.mock import get_contact_found as get_contact_found
from .._generated.mock import get_contact_found_default_contact as get_contact_default_answer
from .._generated.mock import get_contact_not_found as get_contact_not_found

__all__ = [
    'Mock',
    'create_or_update_contact_saved',
    'create_or_update_contact_default_answer',
    'get_contact_found',
    'get_contact_not_found',
    'get_contact_default_answer',
]
