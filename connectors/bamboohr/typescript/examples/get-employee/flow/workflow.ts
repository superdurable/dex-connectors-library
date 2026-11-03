// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, jsonCodec, gracefulComplete, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetEmployeeStep, type Connection, type EmployeeRecord, type GetEmployeeInput } from '@superdurable/connector-bamboohr';

/** Static Dex Web connection name shared by the Worker and starter. */
export const ConnectionName = 'bamboohr-employees';

/** Nonsecret employee record retained for Flow inspection. */
export const employeeRecord = new Attribute<EmployeeRecord>('bamboohr-employee-record', jsonCodec<EmployeeRecord>());

/** Read one employee through a configured BambooHR connection. */
export class GetEmployeeFlow implements Flow<GetEmployeeInput> {
  private readonly complete = new CompleteGetEmployee();
  /** Bind the connection at Worker registration. */
  constructor(private readonly connection: Connection) { if (!connection) throw new TypeError('BambooHR connection is required'); }
  /** Stable Flow identity. */ getFlowType(): string { return 'BambooHRGetEmployeeFlow'; }
  /** Register the provider Step and happy-path completion. */
  getSteps(): StepList<GetEmployeeInput> {
    return StepList.startStep(newGetEmployeeStep<GetEmployeeInput>({
      stepType: 'GetBambooHREmployee',
      annotations: { groupId: 'bamboohr-employee', groupLabel: 'BambooHR Employee', explanation: 'Read one BambooHR employee with the requested fields.' },
      connection: this.connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: this.complete,
    })).otherSteps(this.complete);
  }
  /** Register persisted output. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [employeeRecord] }; }
}

/** Persist the safe employee record and finish. */
// dex:group group-id:bamboohr-employee group-label:"BambooHR Employee"
// dex:explanation text:"Persist the returned employee fields and complete the Flow."
export class CompleteGetEmployee implements Step<QueryResult<EmployeeRecord>> {
  /** Stable Step identity. */ getStepType(): string { return 'CompleteBambooHRGetEmployee'; }
  /** Save the provider result. */
  execute(context: Context, result: QueryResult<EmployeeRecord>): StepDecision {
    employeeRecord.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
