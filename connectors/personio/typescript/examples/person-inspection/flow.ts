// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  Attribute, StepList, StepRef, gracefulComplete, jsonCodec,
  type Context, type Flow, type PersistenceSchema, type Step, type StepDecision,
} from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import {
  newGetPersonStep, type Connection, type PersonProfile, type GetPersonInput,
} from '@superdurable/connector-personio';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'personio';
const personAttribute = new Attribute<PersonProfile>('personio-person', jsonCodec<PersonProfile>());

/** Read one person through a typed Personio Query. */
export class PersonInspectionFlow implements Flow<GetPersonInput> {
  private readonly complete = new CompletePersonInspection();
  private readonly read;

  /** Bind the static provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Personio connection name differs');
    this.read = newGetPersonStep<GetPersonInput>({
      stepType: 'CallGetPerson',
      annotations: { groupId: 'provider', groupLabel: 'Personio', explanation: 'Read one person from Personio.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompletePersonio'),
    });
  }

  /** Return the stable example Flow identity. */ getFlowType(): string { return 'PersonioPersonInspectionTypeScript'; }
  /** Register the provider and completion Steps. */
  getSteps(): StepList<GetPersonInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Persist the selected person result. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [personAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected person and complete the Flow."
class CompletePersonInspection implements Step<QueryResult<PersonProfile>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompletePersonio'; }
  /** Persist and return the selected person. */
  execute(context: Context, result: QueryResult<PersonProfile>): StepDecision {
    personAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
