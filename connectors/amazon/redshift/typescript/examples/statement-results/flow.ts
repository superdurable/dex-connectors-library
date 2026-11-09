// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  Attribute, StepList, StepRef, gracefulComplete, jsonCodec,
  type Context, type Flow, type PersistenceSchema, type Step, type StepDecision,
} from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import {
  newGetStatementResultStep, type Connection, type StatementResultPage, type GetStatementResultInput,
} from '@superdurable/connector-amazon-redshift';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'amazon-redshift';
const functionDetailsAttribute = new Attribute<StatementResultPage>('redshift-statement-results', jsonCodec<StatementResultPage>());

/** Read one statement result page through a typed Amazon Redshift Query. */
export class StatementResultsFlow implements Flow<GetStatementResultInput> {
  private readonly complete = new CompleteStatementResultPage();
  private readonly read;

  /** Bind the static provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Amazon Redshift connection name differs');
    this.read = newGetStatementResultStep<GetStatementResultInput>({
      stepType: 'CallGetStatementResult',
      annotations: { groupId: 'provider', groupLabel: 'Amazon Redshift', explanation: 'Read one statement result page from Amazon Redshift.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      fetched: new StepRef('CompleteGetStatementResult'),
    });
  }

  /** Return the stable example Flow identity. */ getFlowType(): string { return 'AmazonRedshiftStatementResultsTypeScript'; }
  /** Register the provider and completion Steps. */
  getSteps(): StepList<GetStatementResultInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Persist the selected result page result. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [functionDetailsAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected result page and complete the Flow."
class CompleteStatementResultPage implements Step<QueryResult<StatementResultPage>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteGetStatementResult'; }
  /** Persist and return the selected result page. */
  execute(context: Context, result: QueryResult<StatementResultPage>): StepDecision {
    functionDetailsAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
