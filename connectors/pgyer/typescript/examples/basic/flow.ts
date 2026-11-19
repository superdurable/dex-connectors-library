// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListUploadedAppsStep, type Connection, type ListUploadedAppsInput, type UploadedAppPage } from '@superdurable/connector-pgyer';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'pgyer';
const resultAttribute = new Attribute<UploadedAppPage>('pgyer-result', jsonCodec<UploadedAppPage>());

/** Query Pgyer through a typed connector Step. */
export class PgyerInspectionFlow implements Flow<ListUploadedAppsInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Pgyer connection name differs');
    this.read = newListUploadedAppsStep<ListUploadedAppsInput>({
      stepType: 'CallListUploadedApps',
      annotations: { groupId: 'provider', groupLabel: 'Pgyer', explanation: 'Read provider data through Pgyer.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      listed: new StepRef('CompletePgyer'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'PgyerInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<ListUploadedAppsInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<UploadedAppPage>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompletePgyer'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<UploadedAppPage>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
