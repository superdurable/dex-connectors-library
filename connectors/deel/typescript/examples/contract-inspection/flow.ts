// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  Attribute, StepList, StepRef, gracefulComplete, jsonCodec,
  type Context, type Flow, type PersistenceSchema, type Step, type StepDecision,
} from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import {
  newGetContractStep, type Connection, type ContractStatus, type GetContractInput,
} from '@superdurable/connector-deel';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'deel';
const contractAttribute = new Attribute<ContractStatus>('deel-contract', jsonCodec<ContractStatus>());

/** Read one contract through a typed Deel Query. */
export class ContractInspectionFlow implements Flow<GetContractInput> {
  private readonly complete = new CompleteContractInspection();
  private readonly read;

  /** Bind the static provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Deel connection name differs');
    this.read = newGetContractStep<GetContractInput>({
      stepType: 'CallGetContract',
      annotations: { groupId: 'provider', groupLabel: 'Deel', explanation: 'Read one contract from Deel.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteDeel'),
    });
  }

  /** Return the stable example Flow identity. */ getFlowType(): string { return 'DeelContractInspectionTypeScript'; }
  /** Register the provider and completion Steps. */
  getSteps(): StepList<GetContractInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Persist the selected contract result. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [contractAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected contract and complete the Flow."
class CompleteContractInspection implements Step<QueryResult<ContractStatus>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteDeel'; }
  /** Persist and return the selected contract. */
  execute(context: Context, result: QueryResult<ContractStatus>): StepDecision {
    contractAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
