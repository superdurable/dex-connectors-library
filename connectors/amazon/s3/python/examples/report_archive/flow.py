# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Write, inspect, read, and list one S3 report through Dex Steps."""

from dataclasses import dataclass

import dex
from dex_connectors.sdk import MutationResult, QueryResult

from dex_connectors.amazon_s3 import (
    Connection,
    GetObjectTextInput,
    HeadObjectInput,
    ListObjectsInput,
    ListObjectsOutput,
    ObjectMetadata,
    ObjectText,
    PutObjectInput,
    StoredObject,
    new_get_object_text_step,
    new_head_object_step,
    new_list_objects_step,
    new_put_object_step,
)

CONNECTION_NAME = "amazon-s3-report-archive"
FLOW_TYPE = "AmazonS3ReportArchivePython"


@dataclass(frozen=True, slots=True)
class ReportArchiveInput:
    """Nonsecret report content supplied through Dex Web Start Flow."""

    bucket: str
    key: str
    text: str


@dataclass(frozen=True, slots=True)
class ReportArchiveOutput:
    """Proof of a stored, inspected, read, and listed report."""

    stored: StoredObject
    metadata: ObjectMetadata
    text: ObjectText
    listing: ListObjectsOutput


_REQUEST = dex.Attribute("s3-python-archive-request", ReportArchiveInput)
_STORED = dex.Attribute("s3-python-archive-stored", StoredObject)
_METADATA = dex.Attribute("s3-python-archive-metadata", ObjectMetadata)
_TEXT = dex.Attribute("s3-python-archive-text", ObjectText)


# dex:group group-id:s3-archive group-label:"Report archive"
# dex:explanation text:"Validate safe report input and prepare a create-only object write."
class PrepareArchive(dex.Step[ReportArchiveInput]):
    """Validate and persist the report before invoking S3."""

    def get_step_type(self) -> str:
        """Return the stable preparation Step identity."""
        return "PrepareArchive"

    def execute(self, context: dex.Context, input: ReportArchiveInput) -> dex.StepDecision:
        """Continue to the repeat-safe S3 write."""
        if not input.bucket or not input.key or not input.text:
            return dex.force_fail("bucket, key, and text are required")
        _REQUEST.set(context, input)
        return dex.go_to(
            dex.StepRef("StoreReport"),
            PutObjectInput(
                bucket=input.bucket,
                key=input.key,
                content_type="text/plain; charset=utf-8",
                text_content=input.text,
                is_create_only=True,
            ),
        )


# dex:group group-id:s3-archive group-label:"Report archive"
# dex:explanation text:"Persist the stored object and inspect its S3 metadata."
class ArchiveStored(dex.Step[MutationResult[StoredObject]]):
    """Persist the write result and route to metadata read-back."""

    def get_step_type(self) -> str:
        """Return the stable stored Step identity."""
        return "ArchiveStored"

    def execute(self, context: dex.Context, input: MutationResult[StoredObject]) -> dex.StepDecision:
        """Continue with the stored object's identity."""
        _STORED.set(context, input.value)
        return dex.go_to(dex.StepRef("InspectReport"), HeadObjectInput(input.value.bucket, input.value.key))


# dex:group group-id:s3-archive group-label:"Report archive"
# dex:explanation text:"Persist S3 metadata and read the report text."
class ArchiveInspected(dex.Step[QueryResult[ObjectMetadata]]):
    """Persist metadata and route to text read-back."""

    def get_step_type(self) -> str:
        """Return the stable inspection Step identity."""
        return "ArchiveInspected"

    def execute(self, context: dex.Context, input: QueryResult[ObjectMetadata]) -> dex.StepDecision:
        """Continue to bounded text retrieval."""
        _METADATA.set(context, input.value)
        return dex.go_to(dex.StepRef("ReadReport"), GetObjectTextInput(input.value.bucket, input.value.key))


# dex:group group-id:s3-archive group-label:"Report archive"
# dex:explanation text:"Persist bounded text and list the matching archive key."
class ArchiveRead(dex.Step[QueryResult[ObjectText]]):
    """Persist text and route to one bounded listing page."""

    def get_step_type(self) -> str:
        """Return the stable text Step identity."""
        return "ArchiveRead"

    def execute(self, context: dex.Context, input: QueryResult[ObjectText]) -> dex.StepDecision:
        """List the exact report key as a prefix."""
        _TEXT.set(context, input.value)
        request = _REQUEST.get(context)
        return dex.go_to(
            dex.StepRef("ListReports"), ListObjectsInput(bucket=request.bucket, prefix=request.key)
        )


# dex:group group-id:s3-archive group-label:"Report archive"
# dex:explanation text:"Complete with the stored identity, metadata, text, and listing."
class ArchiveListed(dex.Step[QueryResult[ListObjectsOutput]]):
    """Complete with safe report evidence."""

    def get_step_type(self) -> str:
        """Return the stable listing Step identity."""
        return "ArchiveListed"

    def execute(self, context: dex.Context, input: QueryResult[ListObjectsOutput]) -> dex.StepDecision:
        """Return all four operation outcomes."""
        return dex.graceful_complete(
            ReportArchiveOutput(_STORED.get(context), _METADATA.get(context), _TEXT.get(context), input.value)
        )


class ReportArchiveFlow(dex.Flow[ReportArchiveInput]):
    """Archive a text report with the four S3 connector operations."""

    def __init__(self, connection: Connection) -> None:
        """Bind the static connection before Worker registration."""
        if connection is None or connection.reference.name != CONNECTION_NAME:
            raise ValueError("report archive requires its static S3 connection")
        self.prepare = PrepareArchive()
        self.stored = ArchiveStored()
        self.inspected = ArchiveInspected()
        self.read = ArchiveRead()
        self.listed = ArchiveListed()
        annotations = {
            "groupId": "s3-archive",
            "groupLabel": "Report archive",
            "explanation": "Process one report through its S3 operation.",
        }
        self.put = new_put_object_step(
            step_type="StoreReport",
            step_input_type=PutObjectInput,
            annotations=annotations,
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda value: value,
            stored=self.stored,
        )
        self.head = new_head_object_step(
            step_type="InspectReport",
            step_input_type=HeadObjectInput,
            annotations=annotations,
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda value: value,
            found=self.inspected,
        )
        self.get = new_get_object_text_step(
            step_type="ReadReport",
            step_input_type=GetObjectTextInput,
            annotations=annotations,
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda value: value,
            read=self.read,
        )
        self.list = new_list_objects_step(
            step_type="ListReports",
            step_input_type=ListObjectsInput,
            annotations=annotations,
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda value: value,
            found=self.listed,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[ReportArchiveInput]:
        """Register all provider and application Steps."""
        return dex.StepList.start_step(self.prepare).other_steps(
            self.put, self.stored, self.head, self.inspected, self.get, self.read, self.list, self.listed
        )

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register every safe value persisted across Steps."""
        return dex.PersistenceSchema.of(_REQUEST, _STORED, _METADATA, _TEXT)
