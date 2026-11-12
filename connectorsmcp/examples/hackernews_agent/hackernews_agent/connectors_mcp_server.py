# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Start and stop a local ``dex-connectors-mcp serve`` process."""

import asyncio
import json
import os
from collections.abc import Sequence
from dataclasses import dataclass
from datetime import timedelta
from pathlib import Path
from typing import IO, Any

from hackernews_agent.agent import HACKER_NEWS_CONNECTION_NAME

#: Server start deadline: the ready line follows the listener and the first tool set.
SERVER_READY_TIMEOUT = timedelta(seconds=30)

#: Graceful stop deadline after stdin closes.
SERVER_STOP_TIMEOUT = timedelta(seconds=15)

HACKER_NEWS_CONNECTOR_ID = "hacker-news-daily"
HACKER_NEWS_MODULE_PATH = "github.com/superdurable/dex-connectors-library/connectors/hackernews/go"
HACKER_NEWS_PROVIDER = "hacker-news"


@dataclass(frozen=True)
class ConnectorsMcpServerReady:
    """The ready line of ``serve --output json``.

    Attributes:
        url: The MCP endpoint.
        version: The server version.
        tools: The offered MCP tool names, sorted.
        warnings: Non-fatal problems, such as skipped connections.
    """

    url: str
    version: str
    tools: list[str]
    warnings: list[str]


class ConnectorsMcpServerProcess:
    """Own one ``dex-connectors-mcp serve`` child process.

    The child runs with ``--output json --exit-on-stdin-close`` on a free
    loopback port, so closing its stdin stops it and a parent's death never
    leaves it running. Its stderr log goes to ``log_path``.
    """

    def __init__(self, command: Sequence[str], log_path: Path) -> None:
        """Describe the process without starting it.

        Args:
            command: The executable and its arguments, ending with the serve flags.
            log_path: Where the server's stderr log is written.
        """
        self._command = list(command)
        self._log_path = log_path
        self._log_file: IO[bytes] | None = None
        self._process: asyncio.subprocess.Process | None = None

    async def start(self) -> ConnectorsMcpServerReady:
        """Start the server and return its ready line.

        Returns:
            The decoded ready line.

        Raises:
            RuntimeError: If the server reports an error or exits before it is ready.
            TimeoutError: If no ready line arrives within ``SERVER_READY_TIMEOUT``.
        """
        self._log_file = self._log_path.open("wb")
        self._process = await asyncio.create_subprocess_exec(
            *self._command,
            stdin=asyncio.subprocess.PIPE,
            stdout=asyncio.subprocess.PIPE,
            stderr=self._log_file,
        )
        try:
            return await asyncio.wait_for(self._read_ready_line(), SERVER_READY_TIMEOUT.total_seconds())
        except BaseException:
            await self.stop()
            raise

    async def stop(self) -> int | None:
        """Close the server's stdin and wait for it to exit.

        Returns:
            The exit code, or ``None`` when the server was never started.
        """
        process = self._process
        if process is None:
            return None
        if process.stdin is not None and not process.stdin.is_closing():
            process.stdin.close()
        try:
            exit_code = await asyncio.wait_for(process.wait(), SERVER_STOP_TIMEOUT.total_seconds())
        except TimeoutError:
            process.kill()
            exit_code = await process.wait()
        if self._log_file is not None:
            self._log_file.close()
            self._log_file = None
        self._process = None
        return exit_code

    async def _read_ready_line(self) -> ConnectorsMcpServerReady:
        assert self._process is not None and self._process.stdout is not None
        while True:
            line = await self._process.stdout.readline()
            if not line:
                raise RuntimeError(f"{self._command[0]} exited before it was ready; see {self._log_path}")
            announcement: dict[str, Any] = json.loads(line)
            if announcement.get("type") == "error":
                raise RuntimeError(f"{self._command[0]} failed to start: {announcement.get('message')}")
            if announcement.get("type") == "ready":
                return ConnectorsMcpServerReady(
                    url=announcement["url"],
                    version=announcement["version"],
                    tools=list(announcement["tools"]),
                    warnings=list(announcement["warnings"]),
                )


def write_hacker_news_connections_file(path: Path) -> None:
    """Write a Dex Web ``connections.json`` with the public Hacker News connection.

    Hacker News needs no credential, so the record holds empty settings and
    credentials, as Dex Web writes them after the user adds the connection.

    Args:
        path: Where to write the file, with mode ``0600`` as the server requires.
    """
    contents = {
        "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
        "connections": [
            {
                "connectorId": HACKER_NEWS_CONNECTOR_ID,
                "modulePath": HACKER_NEWS_MODULE_PATH,
                "provider": HACKER_NEWS_PROVIDER,
                "connectionName": HACKER_NEWS_CONNECTION_NAME,
                "configuration": {},
                "credentials": {},
            }
        ],
        "triggerBindings": [],
    }
    file_descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(file_descriptor, "w", encoding="utf-8") as connections_file:
        json.dump(contents, connections_file, indent=2)
    os.chmod(path, 0o600)


def hacker_news_serve_command(server_binary: Path, state_dir: Path, connections_file: Path) -> list[str]:
    """Return the ``serve`` command that offers the Hacker News tools.

    Args:
        server_binary: The released ``dex-connectors-mcp`` executable.
        state_dir: The server's call state directory, private to this run.
        connections_file: A file from ``write_hacker_news_connections_file``.

    Returns:
        The command line.
    """
    return [
        str(server_binary),
        "serve",
        "--listen",
        "127.0.0.1:0",
        "--output",
        "json",
        "--exit-on-stdin-close",
        "--state-dir",
        str(state_dir),
        "--connections-file",
        str(connections_file),
        "--connection",
        f"{HACKER_NEWS_CONNECTOR_ID}/{HACKER_NEWS_CONNECTION_NAME}",
    ]
