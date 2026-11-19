// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListDepartmentsStep, type Connection, type ListDepartmentsInput, type ListDepartmentsOutput } from '../../src/index.js';

/** Static Dex Web connection for the Hesi example. */
export const ConnectionName = 'hesi';
const departmentsResult = new Attribute<ListDepartmentsOutput>('hesi-department-list-result', jsonCodec<ListDepartmentsOutput>());

/** Read one bounded page of Hesi departments. */
export class DepartmentCatalogFlow implements Flow<ListDepartmentsInput> {
  private readonly read;
  private readonly complete = new CompleteDepartmentList();
  /** Bind the static project connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Hesi connection name differs');
    this.read = newListDepartmentsStep<ListDepartmentsInput>({
      stepType: 'CallHesi',
      annotations: { groupId: 'hesi', groupLabel: 'Hesi', explanation: 'Read a department page using a short-lived access token.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, listed: new StepRef('CompleteHesiDepartments'),
    });
  }
  /** Preserve the Flow identity. */ getFlowType(): string { return 'HesiDepartmentCatalog'; }
  /** Register the provider and terminal Steps. */
  getSteps(): StepList<ListDepartmentsInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Register the durable department page. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [departmentsResult] }; }
}

// dex:group group-id:hesi group-label:"Hesi"
// dex:explanation text:"Persist the department page and complete the Flow."
class CompleteDepartmentList implements Step<QueryResult<ListDepartmentsOutput>> {
  getStepType(): string { return 'CompleteHesiDepartments'; }
  execute(context: Context, result: QueryResult<ListDepartmentsOutput>): StepDecision { departmentsResult.set(context, result.value); return gracefulComplete(result.value); }
}
