// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListFolderFilesStep, type Connection, type ListFolderFilesInput, type ListFolderFilesOutput } from '@superdurable/connector-kingsoftoffice';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'kingsoftoffice';
const resultAttribute = new Attribute<ListFolderFilesOutput>('kingsoftoffice-result', jsonCodec<ListFolderFilesOutput>());

/** Query KingsoftOffice through a typed connector Step. */
export class KingsoftOfficeInspectionFlow implements Flow<ListFolderFilesInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('KingsoftOffice connection name differs');
    this.read = newListFolderFilesStep<ListFolderFilesInput>({
      stepType: 'CallListFolderFiles',
      annotations: { groupId: 'provider', groupLabel: 'KingsoftOffice', explanation: 'Read provider data through KingsoftOffice.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      listed: new StepRef('CompleteKingsoftOffice'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'KingsoftOfficeInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<ListFolderFilesInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<ListFolderFilesOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteKingsoftOffice'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<ListFolderFilesOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
