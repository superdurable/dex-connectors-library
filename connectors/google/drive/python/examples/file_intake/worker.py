# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Open the Dex registry and blob cache that the Worker and the Trigger's Client share."""

import os
import tempfile
from pathlib import Path

import dex

from .flow import FileIntakeFlow


def open_registry_and_cache(flow: FileIntakeFlow) -> tuple[dex.Registry, dex.BlobCache]:
    """Register the Flow and open the local blob cache; the caller closes the cache."""
    registry = dex.Registry([flow], allow_async_handlers=True)
    cache_directory = os.environ.get(
        "DEX_BLOB_CACHE_DIR", str(Path(tempfile.gettempdir()) / "dex-google-drive-python-file-intake-blobs")
    )
    return registry, dex.open_blob_cache(dex.BlobCacheConfig(cache_directory, 1 << 30))


def dex_server_address() -> str:
    """Return the Dex Flow service address, defaulting to local dexcli dev."""
    return os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")
