// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  StepList, StepRef, gracefulComplete,
  type Context, type Flow, type PersistenceSchema, type Step, type StepDecision,
} from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetMetadataStep, type Connection, type EntryMetadata, type GetMetadataInput } from '../../src/index.js';

/** Static Dex Web connection name. */
export const ConnectionName = 'dropbox';

/** Read one Dropbox file or folder identity. */
export class DropboxMetadataFlow implements Flow<GetMetadataInput> {
  private readonly complete = new CompleteMetadataRead();
  private readonly read;

  /** Bind a named Dropbox connection during Worker registration. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Dropbox metadata Flow requires the static connection');
    this.read = newGetMetadataStep<GetMetadataInput>({
      stepType: 'ReadDropboxMetadata',
      annotations: { groupId: 'dropbox-file', groupLabel: 'Dropbox file', explanation: 'Read one Dropbox file or folder identity.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteDropboxMetadataRead'),
    });
  }

  /** Stable Dex Flow type. */ getFlowType(): string { return 'DropboxGetMetadataTypeScript'; }
  /** Register the provider Query and completion Step. */
  getSteps(): StepList<GetMetadataInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** The Flow persists its input and final result. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [] }; }
}

/** Complete the Flow with safe provider metadata. */
// dex:group group-id:dropbox-file group-label:"Dropbox file"
// dex:explanation text:"Complete with the returned Dropbox file identity."
export class CompleteMetadataRead implements Step<QueryResult<EntryMetadata>> {
  /** Stable Dex Step type. */ getStepType(): string { return 'CompleteDropboxMetadataRead'; }
  /** Return selected file identity. */
  execute(_context: Context, result: QueryResult<EntryMetadata>): StepDecision { return gracefulComplete(result.value); }
}
