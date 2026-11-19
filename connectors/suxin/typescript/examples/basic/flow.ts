// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListOrganizationsStep, type Connection, type ListOrganizationsInput, type ListOrganizationsOutput } from '@superdurable/connector-suxin';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'suxin';
const resultAttribute = new Attribute<ListOrganizationsOutput>('suxin-result', jsonCodec<ListOrganizationsOutput>());

/** Query Suxin through a typed connector Step. */
export class SuxinInspectionFlow implements Flow<ListOrganizationsInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Suxin connection name differs');
    this.read = newListOrganizationsStep<ListOrganizationsInput>({
      stepType: 'CallListOrganizations',
      annotations: { groupId: 'provider', groupLabel: 'Suxin', explanation: 'Read provider data through Suxin.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      listed: new StepRef('CompleteSuxin'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'SuxinInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<ListOrganizationsInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<ListOrganizationsOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteSuxin'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<ListOrganizationsOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
