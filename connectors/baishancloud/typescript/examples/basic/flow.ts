// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newDescribeDomainStep, type Connection, type DescribeDomainInput, type DomainDetail } from '@superdurable/connector-baishancloud';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'baishancloud';
const resultAttribute = new Attribute<DomainDetail>('baishancloud-result', jsonCodec<DomainDetail>());

/** Query BaishanCloud through a typed connector Step. */
export class BaishanCloudInspectionFlow implements Flow<DescribeDomainInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('BaishanCloud connection name differs');
    this.read = newDescribeDomainStep<DescribeDomainInput>({
      stepType: 'CallDescribeDomain',
      annotations: { groupId: 'provider', groupLabel: 'BaishanCloud', explanation: 'Read provider data through BaishanCloud.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteBaishanCloud'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'BaishanCloudInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<DescribeDomainInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<DomainDetail>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteBaishanCloud'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<DomainDetail>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
