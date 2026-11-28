// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListEmployeesStep, type Connection, type ListEmployeesInput, type EmployeesPage } from '@superdurable/connector-gusto';

const ConnectionName = 'gusto';
const resultAttribute = new Attribute<EmployeesPage>('gusto-typescript-employee-page', jsonCodec<EmployeesPage>());

/** Read one page of employees from the requested company. */
export class EmployeeListFlow implements Flow<ListEmployeesInput> {
  private readonly complete = new CompleteEmployeePage();
  private readonly read;
  /** Bind the required static connection. */ constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Employee connection name differs');
    this.read = newListEmployeesStep<ListEmployeesInput>({
      stepType: 'CallListEmployees',
      annotations: { groupId: 'provider', groupLabel: 'Gusto', explanation: 'Read one page of employees from the requested company.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, listed: new StepRef('CompleteEmployeePage'),
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'GustoEmployeeListTypeScript'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<ListEmployeesInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Register the persisted provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the employee page and complete the Flow."
class CompleteEmployeePage implements Step<QueryResult<EmployeesPage>> {
  readonly inputCodec = jsonCodec<QueryResult<EmployeesPage>>();
  getStepType(): string { return 'CompleteEmployeePage'; }
  execute(context: Context, result: QueryResult<EmployeesPage>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
