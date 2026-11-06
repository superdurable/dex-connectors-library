# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Resolve a Drive file, read its text, upload a copy, and read back metadata."""

from dataclasses import dataclass, field

import dex
from dex_connectors.sdk import MutationResult, QueryResult

from dex_connectors.google_drive import (
    Connection,
    File,
    GetFileInput,
    ReadFileTextInput,
    ReadFileTextOutput,
    SearchFilesInput,
    SearchFilesOutput,
    UploadFileInput,
    UploadFileOutput,
    new_get_file_step,
    new_read_file_text_step,
    new_search_files_step,
    new_upload_file_step,
)

CONNECTION_NAME = "google-drive-files"
FLOW_TYPE = "GoogleDriveTextCopyPython"


@dataclass(frozen=True, slots=True)
class TextCopyInput:
    """Safe file names supplied when the Flow starts."""

    source_name: str = field(metadata={"dex_json_name": "sourceName"})
    copy_name: str = field(metadata={"dex_json_name": "copyName"})


request_attribute = dex.Attribute("google-drive-python-text-copy-request", TextCopyInput)


# dex:group group-id:google-drive group-label:"Google Drive"
# dex:explanation text:"Validate and retain source and copy file names."
class PrepareTextCopy(dex.Step[TextCopyInput]):
    """Persist safe names before any provider call."""

    def execute(self, context: dex.Context, input: TextCopyInput) -> dex.StepDecision:
        """Begin the exact-name search when both names are usable."""
        if not input.source_name.strip() or not input.copy_name.strip():
            return dex.force_fail("sourceName and copyName are required")
        request_attribute.set(context, input)
        return dex.go_to(dex.StepRef("SearchSourceFile"), input)


# dex:group group-id:google-drive group-label:"Google Drive"
# dex:explanation text:"Choose one exact-name source file and read its text."
class SelectSourceFile(dex.Step[QueryResult[SearchFilesOutput]]):
    """Choose the first file returned by the exact-name search."""

    def execute(self, context: dex.Context, input: QueryResult[SearchFilesOutput]) -> dex.StepDecision:
        """Pass the provider file ID to the text read Step."""
        if len(input.value.files) != 1 or input.value.next_page_token or input.value.is_incomplete_search:
            return dex.force_fail("sourceName must resolve to exactly one file")
        return dex.go_to(dex.StepRef("ReadSourceText"), ReadFileTextInput(input.value.files[0].id))


# dex:group group-id:google-drive group-label:"Google Drive"
# dex:explanation text:"Prepare a text copy from the bounded source content."
class PrepareUpload(dex.Step[QueryResult[ReadFileTextOutput]]):
    """Map the bounded text result to one upload request."""

    def execute(self, context: dex.Context, input: QueryResult[ReadFileTextOutput]) -> dex.StepDecision:
        """Continue to the fenced upload Mutation."""
        request = request_attribute.get(context)
        return dex.go_to(
            dex.StepRef("UploadTextCopy"),
            UploadFileInput(
                name=request.copy_name, mime_type=input.value.text_mime_type, text_content=input.value.text
            ),
        )


# dex:group group-id:google-drive group-label:"Google Drive"
# dex:explanation text:"Return the newly uploaded file metadata."
class CompleteTextCopy(dex.Step[QueryResult[File]]):
    """Complete with the provider's full copy metadata."""

    def execute(self, context: dex.Context, input: QueryResult[File]) -> dex.StepDecision:
        """Return the verified uploaded file."""
        return dex.graceful_complete(input.value)


class TextCopyFlow(dex.Flow[TextCopyInput]):
    """Copy one uniquely named Drive text file into the authorized Drive."""

    def __init__(self, connection: Connection) -> None:
        if connection is None or connection.reference.name != CONNECTION_NAME:
            raise ValueError("Google Drive text copy requires the static connection")
        self.prepare = PrepareTextCopy()
        self.select = SelectSourceFile()
        self.prepare_upload = PrepareUpload()
        self.complete = CompleteTextCopy()
        self.search = new_search_files_step(
            step_type="SearchSourceFile",
            step_input_type=TextCopyInput,
            annotations={
                "groupId": "google-drive",
                "groupLabel": "Google Drive",
                "explanation": "Find exactly one source file by name.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda request: SearchFilesInput(name=request.source_name),
            found=self.select,
        )
        self.read = new_read_file_text_step(
            step_type="ReadSourceText",
            step_input_type=ReadFileTextInput,
            annotations={
                "groupId": "google-drive",
                "groupLabel": "Google Drive",
                "explanation": "Read bounded UTF-8 source content.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda request: request,
            read=self.prepare_upload,
        )
        self.get_copy = new_get_file_step(
            step_type="ReadBackTextCopy",
            step_input_type=MutationResult[UploadFileOutput],
            annotations={
                "groupId": "google-drive",
                "groupLabel": "Google Drive",
                "explanation": "Read back the uploaded copy's full metadata.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda result: GetFileInput(result.value.file.id),
            found=self.complete,
        )
        self.upload = new_upload_file_step(
            step_type="UploadTextCopy",
            step_input_type=UploadFileInput,
            annotations={
                "groupId": "google-drive",
                "groupLabel": "Google Drive",
                "explanation": "Upload one text copy with durable duplicate protection.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda request: request,
            uploaded=self.get_copy,
        )

    def get_flow_type(self) -> str:
        """Return the stable Dex Flow identity."""
        return "GoogleDriveTextCopyPython"

    def get_steps(self) -> dex.StepList[TextCopyInput]:
        """Register local and provider Steps in execution order."""
        return dex.StepList.start_step(self.prepare).other_steps(
            self.search,
            self.select,
            self.read,
            self.prepare_upload,
            self.upload,
            self.get_copy,
            self.complete,
        )

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the request Attribute consumed by the upload preparation."""
        return dex.PersistenceSchema.of(request_attribute)
