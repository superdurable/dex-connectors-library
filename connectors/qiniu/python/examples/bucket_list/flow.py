# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""List Qiniu Kodo buckets through a statically named connection."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.qiniu import Connection, ListBucketsInput, ListBucketsOutput, new_list_buckets_step

CONNECTION_NAME = "qiniu"
bucket_result = dex.Attribute("qiniu-buckets-python", ListBucketsOutput)


# dex:group group-id:buckets group-label:"Storage buckets"
# dex:explanation text:"Persist bucket names and complete the Flow."
class RecordBuckets(dex.Step[QueryResult[ListBucketsOutput]]):
    """Persist the non-secret bucket names after provider success."""

    def get_step_type(self) -> str:
        """Return the stable completion Step identity."""
        return "RecordQiniuBucketsPython"

    def execute(self, context: dex.Context, input: QueryResult[ListBucketsOutput]) -> dex.StepDecision:
        """Store bucket names without the signing credentials."""
        bucket_result.set(context, input.value)
        return dex.graceful_complete(input.value)


class BucketListFlow(dex.Flow[ListBucketsInput]):
    """List Kodo bucket names using the named Qiniu connection."""

    def __init__(self, connection: Connection) -> None:
        """Bind the provider Query and completion Step."""
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("bucket Flow requires the static Qiniu connection")
        self.record = RecordBuckets()
        self.lookup = new_list_buckets_step(
            step_type="ListQiniuBucketsPython",
            step_input_type=ListBucketsInput,
            annotations={
                "groupId": "buckets",
                "groupLabel": "Storage buckets",
                "explanation": "List buckets owned by the Qiniu signing key.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda input: input,
            listed=self.record,
        )

    def get_flow_type(self) -> str:
        """Return the stable bucket-list Flow identity."""
        return "QiniuBucketListPython"

    def get_steps(self) -> dex.StepList[ListBucketsInput]:
        """Register the provider and completion Steps."""
        return dex.StepList.start_step(self.lookup).other_steps(self.record)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the durable bucket-list result."""
        return dex.PersistenceSchema.of(bucket_result)
