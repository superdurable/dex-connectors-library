// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  StepList, StepRef, gracefulComplete,
  type Context, type Flow, type PersistenceSchema, type Step, type StepDecision,
} from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newDownloadFileStep, type Connection, type DownloadedFile, type DownloadFileInput } from '@superdurable/connector-box';

/** Static Dex Web connection name. */
export const ConnectionName = 'box';

/** Download one bounded Box file by numeric ID. */
export class BoxDownloadFileFlow implements Flow<DownloadFileInput> {
  private readonly complete = new CompleteBoxDownload();
  private readonly connection: Connection;

  /** Bind a named Box connection during Worker registration. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Box download Flow requires the static connection');
    this.connection = connection;
  }

  /** Stable Dex Flow type. */ getFlowType(): string { return 'BoxDownloadFileTypeScript'; }
  /** Register the provider Query and completion Step. */
  getSteps(): StepList<DownloadFileInput> {
    return StepList.startStep(newDownloadFileStep<DownloadFileInput>({
      stepType: 'DownloadBoxFile',
      annotations: { groupId: 'box-file', groupLabel: 'Box file', explanation: 'Download bounded content from one Box file.' },
      connection: this.connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      downloaded: new StepRef('CompleteBoxDownload'),
    })).otherSteps(this.complete);
  }
  /** The Flow persists its input and final result. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [] }; }
}

/** Complete the Flow with bounded file content. */
// dex:group group-id:box-file group-label:"Box file"
// dex:explanation text:"Complete with the downloaded Box file content."
export class CompleteBoxDownload implements Step<QueryResult<DownloadedFile>> {
  /** Stable Dex Step type. */ getStepType(): string { return 'CompleteBoxDownload'; }
  /** Return selected file content. */
  execute(_context: Context, result: QueryResult<DownloadedFile>): StepDecision { return gracefulComplete(result.value); }
}
