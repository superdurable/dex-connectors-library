// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  Attribute, StepList, StepRef, gracefulComplete, jsonCodec,
  type Context, type Flow, type PersistenceSchema, type Step, type StepDecision,
} from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import {
  newGetFunctionStep, type Connection, type FunctionDetails, type GetFunctionInput,
} from '@superdurable/connector-amazon-lambda';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'amazon-lambda';
const functionDetailsAttribute = new Attribute<FunctionDetails>('lambda-function-lookup', jsonCodec<FunctionDetails>());

/** Read one function metadata through a typed Amazon Lambda Query. */
export class FunctionLookupFlow implements Flow<GetFunctionInput> {
  private readonly complete = new CompleteFunctionDetails();
  private readonly read;

  /** Bind the static provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Amazon Lambda connection name differs');
    this.read = newGetFunctionStep<GetFunctionInput>({
      stepType: 'CallGetFunction',
      annotations: { groupId: 'provider', groupLabel: 'Amazon Lambda', explanation: 'Read one function metadata from Amazon Lambda.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteGetFunction'),
    });
  }

  /** Return the stable example Flow identity. */ getFlowType(): string { return 'AmazonLambdaFunctionLookupTypeScript'; }
  /** Register the provider and completion Steps. */
  getSteps(): StepList<GetFunctionInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Persist the selected function metadata result. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [functionDetailsAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected function metadata and complete the Flow."
class CompleteFunctionDetails implements Step<QueryResult<FunctionDetails>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteGetFunction'; }
  /** Persist and return the selected function metadata. */
  execute(context: Context, result: QueryResult<FunctionDetails>): StepDecision {
    functionDetailsAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
