// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StepList, gracefulComplete, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetFileStep, type Connection, type GetFileInput, type DriveItem } from '@superdurable/connector-microsoft-onedrive';

/** Static Dex Web connection name shared by the Worker and starter. */
export const ConnectionName = 'onedrive-files';

/** Read one OneDrive or SharePoint file's metadata. */
export class GetFileFlow implements Flow<GetFileInput> {
  private readonly complete = new CompleteFileRead();

  /** Bind the named Microsoft connection at registration. */
  constructor(private readonly connection: Connection) {
    if (!connection) throw new TypeError('OneDrive connection is required');
  }

  /** Stable Dex Flow type. */ getFlowType(): string { return 'OneDriveGetFileFlow'; }
  /** Register the provider Step and completion Step. */
  getSteps(): StepList<GetFileInput> {
    return StepList.startStep(newGetFileStep<GetFileInput>({
      stepType: 'ReadOneDriveFile',
      annotations: { groupId: 'onedrive-file', groupLabel: 'OneDrive File', explanation: 'Read metadata for one OneDrive or SharePoint file.' },
      connection: this.connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: this.complete,
    })).otherSteps(this.complete);
  }
  /** This Flow persists its input and final output only. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [] }; }
}

/** Complete the Flow with safe file metadata. */
// dex:group group-id:onedrive-file group-label:"OneDrive File"
// dex:explanation text:"Complete after the file metadata is read."
export class CompleteFileRead implements Step<QueryResult<DriveItem>> {
  /** Stable Dex Step type. */ getStepType(): string { return 'CompleteOneDriveFileRead'; }
  /** Return the connector's safe output. */
  execute(_context: Context, result: QueryResult<DriveItem>): StepDecision { return gracefulComplete(result.value); }
}
