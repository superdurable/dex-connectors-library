// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetEmployeeStep, type Connection, type GetEmployeeInput, type Employee } from '@superdurable/connector-gusto';

/** Static Dex Web connection for the Employee example. */
export const ConnectionName = 'gusto';
const resultAttribute = new Attribute<Employee>('gusto-typescript-employee', jsonCodec<Employee>());

/** Read the requested Gusto employee. */
export class EmployeeFlow implements Flow<GetEmployeeInput> {
  private readonly complete = new CompleteEmployee();
  private readonly read;
  /** Bind the required static connection. */ constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Employee connection name differs');
    this.read = newGetEmployeeStep<GetEmployeeInput>({
      stepType: 'CallGetEmployee',
      annotations: { groupId: 'provider', groupLabel: 'Gusto', explanation: 'Read the requested Gusto employee.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, found: new StepRef('CompleteEmployee'),
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'GustoEmployeeLookupTypeScript'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<GetEmployeeInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Register the persisted provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the employee and complete the Flow."
class CompleteEmployee implements Step<QueryResult<Employee>> {
  readonly inputCodec = jsonCodec<QueryResult<Employee>>();
  getStepType(): string { return 'CompleteEmployee'; }
  execute(context: Context, result: QueryResult<Employee>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
