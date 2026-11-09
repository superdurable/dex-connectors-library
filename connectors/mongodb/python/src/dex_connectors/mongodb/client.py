# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Native MongoDB client configuration and rotating credential source."""

import math
import re
from collections.abc import Callable
from typing import TYPE_CHECKING, Any

import httpx
from dex_connectors.sdk import CredentialProvider
from pymongo import AsyncMongoClient

from ._generated.config import Config, Credentials

if TYPE_CHECKING:
    from .operations import FindDocumentsOperation

_DATABASE_NAME = re.compile(r"[A-Za-z_][A-Za-z0-9_-]*\Z")


class Client:
    """Validate read bounds and resolve the MongoDB URI for every Step attempt."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        *,
        create_driver: Callable[..., AsyncMongoClient[Any]] = AsyncMongoClient,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("MongoDB configuration and credential source are required")
        if http_client is not None:
            raise ValueError("MongoDB uses the native wire protocol, not an HTTP client")
        if not _DATABASE_NAME.fullmatch(config.database):
            raise ValueError("MongoDB database name is invalid")
        if (
            not math.isfinite(config.timeout_seconds)
            or config.timeout_seconds <= 0
            or type(config.max_documents) is not int
            or not 1 <= config.max_documents <= 1000
            or type(config.max_response_bytes) is not int
            or not 1 <= config.max_response_bytes <= 16 << 20
        ):
            raise ValueError("MongoDB read bounds are invalid")
        self.config = config
        self.credentials = credentials
        self.create_driver = create_driver

    def find_documents(self) -> "FindDocumentsOperation":
        """Bind the read-only document Query."""
        from .operations import FindDocumentsOperation

        return FindDocumentsOperation(self)

    async def aclose(self) -> None:
        """Close no shared connection; each attempt owns its driver client."""


__all__ = ["Client", "Config", "Credentials"]
