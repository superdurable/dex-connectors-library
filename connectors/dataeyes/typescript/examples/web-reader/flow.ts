// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newReadWebPageStep, type Connection, type ReadWebPageInput, type ReadWebPageOutput } from '@superdurable/connector-dataeyes';

/** Static Dex Web connection used by the example. */ export const ConnectionName = 'dataeyes';
/** Persisted public web page. */ export const webPageAttribute = new Attribute<ReadWebPageOutput>('dataeyes-web-reader', jsonCodec<ReadWebPageOutput>());

/** Read a public page through the connected DataEyes Reader. */
export class DataEyesWebPageFlow implements Flow<ReadWebPageInput> {
  private readonly complete = new CompleteWebPage();
  private readonly read;
  /** Bind one static DataEyes connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('DataEyes connection name differs');
    this.read = newReadWebPageStep<ReadWebPageInput>({ stepType: 'CallReadWebPage',
      annotations: { groupId: 'provider', groupLabel: 'DataEyes', explanation: 'Read the supplied public HTTPS page as Markdown.' },
      connection, connectionName: ConnectionName, mapToOperationInput: input => input,
      found: new StepRef('CompleteDataEyesWebPage'),
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'DataEyesWebPageTypeScript'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<ReadWebPageInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Register the persisted page result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [webPageAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the web page result and complete the Flow."
class CompleteWebPage implements Step<QueryResult<ReadWebPageOutput>> {
  /** Stable Step identity. */ getStepType(): string { return 'CompleteDataEyesWebPage'; }
  /** Persist the provider result without credentials. */
  execute(context: Context, result: QueryResult<ReadWebPageOutput>): StepDecision { webPageAttribute.set(context, result.value); return gracefulComplete(result.value); }
}
