// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetFileStep, newGetFileCommentsStep, type Connection, type File, type FileComments, type GetFileInput } from '@superdurable/connector-figma';

/** Static Dex Web connection used by this example. */
export const ConnectionName = 'figma';

/** Nonsecret Figma lookup request. */
export interface FigmaLookupRequest { /** File key from a Figma URL. */ fileKey: string; }
/** Completed file and comments summary. */
export interface FigmaLookupOutcome { /** File display name. */ name: string; /** Returned comments. */ comments: FileComments['comments']; }

const fileName = new Attribute('figma-file-name', jsonCodec<string>());
const fileKey = new Attribute('figma-file-key', jsonCodec<string>());

/** Read one Figma file and its comments with a static connection. */
export class FigmaLookupFlow implements Flow<FigmaLookupRequest> {
  private readonly start = new StartLookup();
  private readonly fileRead = new ReturnFile();
  private readonly commentsRead = new ReturnComments();
  private readonly connection: Connection;

  /** Bind the connection during Worker registration. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Figma lookup requires the static connection');
    this.connection = connection;
  }

  /** Stable Flow type. */ getFlowType(): string { return 'FigmaTypeScriptLookup'; }
  /** Register start, provider, and result Steps. */
  getSteps(): StepList<FigmaLookupRequest> {
    return StepList.startStep(this.start).otherSteps(
      newGetFileStep<FigmaLookupRequest>({
        stepType: 'ReadFigmaFile', annotations: { groupId: 'figma-lookup', groupLabel: 'Figma lookup', explanation: 'Read file metadata and document.' },
        connection: this.connection, connectionName: ConnectionName, mapToOperationInput: request => ({ fileKey: request.fileKey }),
        found: new StepRef('FigmaFileRead'),
      }),
      this.fileRead,
      newGetFileCommentsStep<GetFileInput>({
        stepType: 'ReadFigmaComments', annotations: { groupId: 'figma-lookup', groupLabel: 'Figma lookup', explanation: 'Read comments on the file.' },
        connection: this.connection, connectionName: ConnectionName, mapToOperationInput: request => request,
        listed: new StepRef('FigmaCommentsRead'),
      }),
      this.commentsRead,
    );
  }
  /** Register the stored file name and key. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [fileName, fileKey] }; }
}

// dex:group group-id:figma-lookup group-label:"Figma lookup"
// dex:explanation text:"Validate the file key before the first provider read."
class StartLookup implements Step<FigmaLookupRequest> {
  /** Stable Step type. */ getStepType(): string { return 'StartFigmaTypeScriptLookup'; }
  /** Forward a safe file key. */
  execute(context: Context, request: FigmaLookupRequest): StepDecision {
    if (!/^[A-Za-z0-9_-]{1,128}$/.test(request.fileKey)) return forceFail('fileKey is invalid');
    context.setAttribute(fileKey, request.fileKey);
    return goTo(new StepRef('ReadFigmaFile'), request);
  }
}

// dex:group group-id:figma-lookup group-label:"Figma lookup"
// dex:explanation text:"Store file metadata and read its comments."
class ReturnFile implements Step<QueryResult<File>> {
  /** Stable Step type. */ getStepType(): string { return 'FigmaFileRead'; }
  /** Persist the name and proceed to comments. */
  execute(context: Context, result: QueryResult<File>): StepDecision {
    context.setAttribute(fileName, result.value.name);
    return goTo(new StepRef('ReadFigmaComments'), { fileKey: context.getAttribute(fileKey) });
  }
}

// dex:group group-id:figma-lookup group-label:"Figma lookup"
// dex:explanation text:"Complete with the file name and returned comments."
class ReturnComments implements Step<QueryResult<FileComments>> {
  /** Stable Step type. */ getStepType(): string { return 'FigmaCommentsRead'; }
  /** Complete the typed Flow. */
  execute(context: Context, result: QueryResult<FileComments>): StepDecision {
    return gracefulComplete({ name: context.getAttribute(fileName), comments: result.value.comments } satisfies FigmaLookupOutcome);
  }
}
