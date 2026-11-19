// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListTagsStep, type Connection, type ListTagsInput, type TagsResult } from '@superdurable/connector-weiban';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'weiban';
const resultAttribute = new Attribute<TagsResult>('weiban-result', jsonCodec<TagsResult>());

/** Query Weiban through a typed connector Step. */
export class WeibanInspectionFlow implements Flow<ListTagsInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Weiban connection name differs');
    this.read = newListTagsStep<ListTagsInput>({
      stepType: 'CallListTags',
      annotations: { groupId: 'provider', groupLabel: 'Weiban', explanation: 'Read the first page of authorized Weiban tags.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      listed: new StepRef('CompleteWeiban'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'WeibanInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<ListTagsInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<TagsResult>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteWeiban'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<TagsResult>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
