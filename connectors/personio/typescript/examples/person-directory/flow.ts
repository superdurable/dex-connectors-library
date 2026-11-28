// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  Attribute, StepList, StepRef, gracefulComplete, jsonCodec,
  type Context, type Flow, type PersistenceSchema, type Step, type StepDecision,
} from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import {
  newListPersonsStep, type Connection, type PersonPage, type ListPersonsInput,
} from '@superdurable/connector-personio';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'personio';
const personAttribute = new Attribute<PersonPage>('personio-person-page', jsonCodec<PersonPage>());

/** Read one page through a typed Personio Query. */
export class PersonDirectoryFlow implements Flow<ListPersonsInput> {
  private readonly complete = new CompletePersonDirectory();
  private readonly read;

  /** Bind the static provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Personio connection name differs');
    this.read = newListPersonsStep<ListPersonsInput>({
      stepType: 'CallListPersons',
      annotations: { groupId: 'provider', groupLabel: 'Personio', explanation: 'Read one page from Personio.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      listed: new StepRef('CompletePersonioDirectory'),
    });
  }

  /** Return the stable example Flow identity. */ getFlowType(): string { return 'PersonioPersonDirectoryTypeScript'; }
  /** Register the provider and completion Steps. */
  getSteps(): StepList<ListPersonsInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Persist the selected page result. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [personAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected page and complete the Flow."
class CompletePersonDirectory implements Step<QueryResult<PersonPage>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompletePersonioDirectory'; }
  /** Persist and return the selected page. */
  execute(context: Context, result: QueryResult<PersonPage>): StepDecision {
    personAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
