# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read one Halo article page through a statically named connection."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.fit2cloud import (
    Connection,
    ListPostsInput,
    ListPostsOutput,
    new_list_posts_step,
)

CONNECTION_NAME = "fit2cloud"
post_page_result = dex.Attribute("fit2cloud-post-page-python", ListPostsOutput)


# dex:group group-id:posts group-label:"Posts"
# dex:explanation text:"Persist the article page and complete the Flow."
class RecordPostPage(dex.Step[QueryResult[ListPostsOutput]]):
    """Persist article metadata after provider success."""

    def get_step_type(self) -> str:
        """Return the stable completion Step identity."""
        return "RecordFIT2CLOUDPostsPython"

    def execute(self, context: dex.Context, input: QueryResult[ListPostsOutput]) -> dex.StepDecision:
        """Store article metadata without credential material."""
        post_page_result.set(context, input.value)
        return dex.graceful_complete(input.value)


class PostCatalogFlow(dex.Flow[ListPostsInput]):
    """Read one Halo article page using the named FIT2CLOUD connection."""

    def __init__(self, connection: Connection) -> None:
        """Bind the provider Query and completion Step."""
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("post catalog Flow requires the static FIT2CLOUD connection")
        self.record = RecordPostPage()
        self.lookup = new_list_posts_step(
            step_type="ListFIT2CLOUDPostsPython",
            step_input_type=ListPostsInput,
            annotations={
                "groupId": "posts",
                "groupLabel": "Posts",
                "explanation": "Read one page of Halo article metadata.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda input: input,
            listed=self.record,
        )

    def get_flow_type(self) -> str:
        """Return the stable article catalog Flow identity."""
        return "FIT2CLOUDPostCatalogPython"

    def get_steps(self) -> dex.StepList[ListPostsInput]:
        """Register the provider and completion Steps."""
        return dex.StepList.start_step(self.lookup).other_steps(self.record)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the durable article page."""
        return dex.PersistenceSchema.of(post_page_result)
