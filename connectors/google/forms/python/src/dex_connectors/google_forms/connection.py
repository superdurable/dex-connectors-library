# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Generated Google Forms connection and project credential wiring."""

from ._generated.connection import (
    Connection,
    decode_credentials,
    decode_shared_credentials,
    encode_credentials,
    new_connection,
    new_connection_with_operations,
    new_project_connection,
)

__all__ = [
    "Connection",
    "decode_credentials",
    "decode_shared_credentials",
    "encode_credentials",
    "new_connection",
    "new_connection_with_operations",
    "new_project_connection",
]
