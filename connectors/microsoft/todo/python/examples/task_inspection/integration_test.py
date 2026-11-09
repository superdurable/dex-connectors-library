# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker verification for both Microsoft To Do Python Flows."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.microsoft_todo import (
    Client,
    Config,
    Credentials,
    ListTaskListsInput,
    ListTasksInput,
    Task,
    TaskList,
    TaskListsPage,
    TasksPage,
    new_connection,
)

from .lists_flow import CONNECTION_NAME, TaskListsInspectionFlow
from .tasks_flow import TasksInspectionFlow


@pytest.mark.asyncio
async def test_task_inspection_retries_transient_graph_response(tmp_path: Path) -> None:
    """Run both provider Steps across real Dex retry and persistence boundaries."""
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        assert request.headers["Authorization"] == "Bearer sample-access-token"
        if request.url.path == "/v1.0/me/todo/lists":
            return httpx.Response(200, json={"value": [{"id": "sample-list-1", "displayName": "Tasks"}]})
        assert request.url.path == "/v1.0/me/todo/lists/sample-list-1/tasks"
        if sum(item.url.path.endswith("/tasks") for item in requests) == 1:
            return httpx.Response(503)
        return httpx.Response(
            200, json={"value": [{"id": "sample-task-1", "title": "Review request", "status": "notStarted"}]}
        )

    reference = ConnectionRef("microsoft", CONNECTION_NAME)
    credentials = Credentials(
        "sample-client-id",
        SecretString("sample-client-secret"),
        SecretString("sample-access-token"),
        SecretString("sample-refresh-token"),
    )
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        provider = Client(Config(), StaticCredentialProvider({reference: credentials}), http_client)
        connection = new_connection(provider, reference)
        list_flow, task_flow = TaskListsInspectionFlow(connection), TasksInspectionFlow(connection)
        registry = dex.Registry([list_flow, task_flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=15)
            async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
                list_id = f"todo-python-lists-{uuid4().hex}"
                await client.start_flow(list_flow, list_id, ListTaskListsInput())
                list_result = await asyncio.wait_for(client.wait_for_flow(list_id, timedelta(seconds=25)), 30)
                assert list_result.status == dex.FlowStatus.COMPLETED, list_result.error_message
                assert list_result.single_output(TaskListsPage) == TaskListsPage(
                    (TaskList("sample-list-1", "Tasks"),)
                )
                task_id = f"todo-python-tasks-{uuid4().hex}"
                await client.start_flow(task_flow, task_id, ListTasksInput("sample-list-1"))
                task_result = await asyncio.wait_for(client.wait_for_flow(task_id, timedelta(seconds=25)), 30)
                assert task_result.status == dex.FlowStatus.COMPLETED, task_result.error_message
                assert task_result.single_output(TasksPage) == TasksPage(
                    (Task("sample-task-1", "Review request", "notStarted"),)
                )
                assert len(requests) == 3
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
