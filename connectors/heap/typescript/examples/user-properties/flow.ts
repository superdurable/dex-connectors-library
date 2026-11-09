// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { MutationResult } from '@superdurable/connectors-sdk';
import { newSetUserPropertiesStep, type Connection, type SetUserPropertiesInput, type UserPropertiesResult } from '@superdurable/connector-heap';

/** Static Dex Web connection used by Worker and starter. */
export const ConnectionName = 'heap';
const outcomeAttribute = new Attribute<UserPropertiesResult>('heap-user-properties-outcome', jsonCodec<UserPropertiesResult>());

/** Update one user's Heap properties and persist acceptance. */
export class UserPropertiesFlow implements Flow<SetUserPropertiesInput> {
  private readonly start = new StartProperties();
  private readonly accepted = new AcceptProperties();

  /** Bind the named Heap connection. */
  constructor(private readonly connection: Connection) {
    if (!connection) throw new TypeError('Heap connection is required');
  }
  /** Stable Dex Flow identity. */ getFlowType(): string { return 'HeapUserPropertiesTypeScript'; }
  /** Register start, provider, and completion Steps. */
  getSteps(): StepList<SetUserPropertiesInput> {
    return StepList.startStep(this.start).otherSteps(
      newSetUserPropertiesStep({
        stepType: 'SetHeapUserProperties',
        annotations: { groupId: 'user-properties', groupLabel: 'User properties', explanation: 'Replace selected properties for one stable Heap identity.' },
        connection: this.connection, connectionName: ConnectionName,
        mapToOperationInput: input => input,
        accepted: this.accepted,
      }),
      this.accepted,
    );
  }
  /** Register the persisted acceptance Attribute. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [outcomeAttribute] }; }
}

/** Dispatch the typed request to the provider Step. */
// dex:group group-id:user-properties group-label:"User properties"
// dex:explanation text:"Pass the typed identity and properties to Heap."
export class StartProperties implements Step<SetUserPropertiesInput> {
  /** Stable Dex Step identity. */ getStepType(): string { return 'StartHeapUserPropertiesTypeScript'; }
  /** Dispatch one property update. */
  execute(_context: Context, input: SetUserPropertiesInput): StepDecision {
    return goTo(new StepRef('SetHeapUserProperties'), input);
  }
}

/** Complete the Flow after Heap accepts the update. */
// dex:group group-id:user-properties group-label:"User properties"
// dex:explanation text:"Record provider acceptance and complete the Flow."
export class AcceptProperties implements Step<MutationResult<UserPropertiesResult>> {
  /** Stable Dex Step identity. */ getStepType(): string { return 'HeapUserPropertiesAcceptedTypeScript'; }
  /** Persist only the acceptance result. */
  execute(context: Context, result: MutationResult<UserPropertiesResult>): StepDecision {
    outcomeAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
