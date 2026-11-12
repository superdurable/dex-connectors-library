# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Check the published Dropbox input schemas."""

import json
from pathlib import Path

import dex_connectors.dropbox as dropbox


def test_list_folder_input_schema_lets_callers_omit_the_root_path() -> None:
    document = json.loads((Path(dropbox.__file__).parent / "dex-connector.json").read_text())
    schemas = {factory["operationId"]: factory["inputSchema"] for factory in document["factories"]}
    # An empty path lists the root folder; limit has no default.
    assert schemas["listFolder"]["required"] == ["limit"]
