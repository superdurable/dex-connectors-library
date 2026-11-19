// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListCampaignsStep, type Connection, type ListCampaignsInput, type ListCampaignsOutput } from '@superdurable/connector-sensorsdata';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'sensorsdata';
const resultAttribute = new Attribute<ListCampaignsOutput>('sensorsdata-result', jsonCodec<ListCampaignsOutput>());

/** Query SensorsData through a typed connector Step. */
export class SensorsDataInspectionFlow implements Flow<ListCampaignsInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('SensorsData connection name differs');
    this.read = newListCampaignsStep<ListCampaignsInput>({
      stepType: 'CallListCampaigns',
      annotations: { groupId: 'provider', groupLabel: 'SensorsData', explanation: 'Read provider data through SensorsData.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      listed: new StepRef('CompleteSensorsData'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'SensorsDataInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<ListCampaignsInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<ListCampaignsOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteSensorsData'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<ListCampaignsOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
