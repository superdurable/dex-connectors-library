# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real Dex Worker and Client verification of WordPress Python Flows."""

import asyncio
import os
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import dex
import pytest

from dex_connectors.wordpress import (
    CreatedPost,
    CreatePostInput,
    GetPostInput,
    ListPostsInput,
    Post,
    PostPage,
)
from dex_connectors.wordpress.wordpressmock import Mock

from .post_creation.flow import PostCreationFlow
from .post_list.flow import PostListFlow
from .post_lookup.flow import PostLookupFlow


@pytest.mark.asyncio
async def test_wordpress_flows_use_generated_mocks_with_real_dex(tmp_path: Path) -> None:
    """Run all checked-in operation Flows through Worker, Client, and Attributes."""
    mock = Mock()
    create_flow = PostCreationFlow(mock.connection())
    lookup_flow = PostLookupFlow(mock.connection())
    list_flow = PostListFlow(mock.connection())
    registry = dex.Registry([create_flow, lookup_flow, list_flow], allow_async_handlers=True)
    cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
    address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
    worker_task = asyncio.create_task(worker.start())
    try:
        await worker.wait_pull_registered(timeout=15)
        async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as client:
            create_id = f"wordpress-python-create-{uuid4().hex}"
            await client.start_flow(
                create_flow, create_id, CreatePostInput("Meridian release", "<p>Draft.</p>", "draft")
            )
            created = await asyncio.wait_for(client.wait_for_flow(create_id, timedelta(seconds=25)), 30)
            assert created.status == dex.FlowStatus.COMPLETED, created.error_message
            assert created.single_output(CreatedPost).id == 43
            assert len(mock.create_post_script.calls()) == 1

            lookup_id = f"wordpress-python-lookup-{uuid4().hex}"
            await client.start_flow(lookup_flow, lookup_id, GetPostInput(42))
            looked_up = await asyncio.wait_for(client.wait_for_flow(lookup_id, timedelta(seconds=25)), 30)
            assert looked_up.status == dex.FlowStatus.COMPLETED, looked_up.error_message
            assert looked_up.single_output(Post).slug == "meridian-release"
            assert len(mock.get_post_script.calls()) == 1

            list_id = f"wordpress-python-list-{uuid4().hex}"
            await client.start_flow(list_flow, list_id, ListPostsInput(1, 20))
            listed = await asyncio.wait_for(client.wait_for_flow(list_id, timedelta(seconds=25)), 30)
            assert listed.status == dex.FlowStatus.COMPLETED, listed.error_message
            assert listed.single_output(PostPage).total == 1
            assert len(mock.list_posts_script.calls()) == 1
    finally:
        await worker.stop()
        await worker_task
        cache.close()
