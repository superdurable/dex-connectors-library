// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { MutationResult, QueryResult } from '@superdurable/connectors-sdk';
import { newGetFileStep, newReadFileTextStep, newSearchFilesStep, newUploadFileStep, type Connection, type File, type ReadFileTextOutput, type SearchFilesOutput, type UploadFileOutput } from '@superdurable/connector-google-drive';

/** Static Google Drive connection name used by Worker and starter. */
export const ConnectionName = 'google-drive-files';
/** User supplied text copy request. */
export interface TextCopyInput { sourceName: string; copyName: string }
/** Persisted nonsecret request for later Steps. */
export const copyRequest = new Attribute<TextCopyInput>('google-drive-copy-request', jsonCodec<TextCopyInput>());

/** Copy the text of one named Drive file and read back the created file. */
export class TextCopyFlow implements Flow<TextCopyInput> {
  readonly #prepare = new PrepareCopy();
  readonly #select = new SelectSource();
  readonly #prepareUpload = new PrepareUpload();
  readonly #prepareReadBack = new PrepareReadBack();
  readonly #complete = new CompleteCopy();
  constructor(readonly connection: Connection) { if (!connection) throw new TypeError('Drive connection is required'); }
  /** Stable Dex Flow type. */ getFlowType(): string { return 'GoogleDriveTextCopyFlow'; }
  /** Register all four provider operations and application Steps. */
  getSteps(): StepList<TextCopyInput> {
    return StepList.startStep(this.#prepare).otherSteps(
      newSearchFilesStep({ stepType: 'SearchDriveSource', annotations: { groupId: 'drive-copy', groupLabel: 'Drive text copy', explanation: 'Find the source file by exact name.' },
        connection: this.connection, connectionName: ConnectionName, mapToOperationInput: (input: TextCopyInput) => ({ name: input.sourceName, pageSize: 2 }), found: this.#select }),
      this.#select,
      newReadFileTextStep({ stepType: 'ReadDriveSource', annotations: { groupId: 'drive-copy', groupLabel: 'Drive text copy', explanation: 'Read bounded UTF-8 source text.' },
        connection: this.connection, connectionName: ConnectionName, mapToOperationInput: (input: { fileId: string }) => input, read: this.#prepareUpload }),
      this.#prepareUpload,
      newUploadFileStep({ stepType: 'UploadDriveCopy', annotations: { groupId: 'drive-copy', groupLabel: 'Drive text copy', explanation: 'Create one text copy with duplicate read-back.' },
        connection: this.connection, connectionName: ConnectionName, mapToOperationInput: (input: { name: string; mimeType: string; textContent: string }) => input, uploaded: this.#prepareReadBack }),
      this.#prepareReadBack,
      newGetFileStep({ stepType: 'GetDriveCopy', annotations: { groupId: 'drive-copy', groupLabel: 'Drive text copy', explanation: 'Read the created copy metadata.' },
        connection: this.connection, connectionName: ConnectionName, mapToOperationInput: (input: { fileId: string }) => input, found: this.#complete }),
      this.#complete,
    );
  }
  /** Register the request Attribute used after the search. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [copyRequest] }; }
}

// dex:group group-id:drive-copy group-label:"Drive text copy"
// dex:explanation text:"Validate names before searching Drive."
class PrepareCopy implements Step<TextCopyInput> {
  getStepType(): string { return 'PrepareDriveCopy'; }
  execute(context: Context, input: TextCopyInput): StepDecision {
    if (!input.sourceName?.trim() || !input.copyName?.trim()) return forceFail('Source and copy names are required');
    copyRequest.set(context, input);
    return goTo(new StepRef('SearchDriveSource'), input);
  }
}

// dex:group group-id:drive-copy group-label:"Drive text copy"
// dex:explanation text:"Select one source file from the Drive search."
class SelectSource implements Step<QueryResult<SearchFilesOutput>> {
  getStepType(): string { return 'SelectDriveSource'; }
  execute(_context: Context, result: QueryResult<SearchFilesOutput>): StepDecision {
    if (result.value.files?.length !== 1 || result.value.nextPageToken) return forceFail('Source file name is ambiguous');
    return goTo(new StepRef('ReadDriveSource'), { fileId: result.value.files[0]!.id });
  }
}

// dex:group group-id:drive-copy group-label:"Drive text copy"
// dex:explanation text:"Prepare the text upload from the read result."
class PrepareUpload implements Step<QueryResult<ReadFileTextOutput>> {
  getStepType(): string { return 'PrepareDriveUpload'; }
  execute(context: Context, result: QueryResult<ReadFileTextOutput>): StepDecision {
    const request = copyRequest.get(context);
    return goTo(new StepRef('UploadDriveCopy'), { name: request.copyName, mimeType: result.value.textMimeType || 'text/plain', textContent: result.value.text || '' });
  }
}

// dex:group group-id:drive-copy group-label:"Drive text copy"
// dex:explanation text:"Read back the provider ID returned by upload."
class PrepareReadBack implements Step<MutationResult<UploadFileOutput>> {
  getStepType(): string { return 'PrepareDriveReadBack'; }
  execute(_context: Context, result: MutationResult<UploadFileOutput>): StepDecision { return goTo(new StepRef('GetDriveCopy'), { fileId: result.value.file.id }); }
}

// dex:group group-id:drive-copy group-label:"Drive text copy"
// dex:explanation text:"Complete with the copied file metadata."
class CompleteCopy implements Step<QueryResult<File>> {
  getStepType(): string { return 'CompleteDriveCopy'; }
  execute(_context: Context, result: QueryResult<File>): StepDecision { return gracefulComplete(result.value); }
}
