// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  Attribute, StepList, StepRef, gracefulComplete, jsonCodec,
  type Context, type Flow, type PersistenceSchema, type Step, type StepDecision,
} from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import {
  newGetEmailIdentityStep, type Connection, type EmailIdentityDetails, type GetEmailIdentityInput,
} from '@superdurable/connector-amazon-ses';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'amazon-ses';
const functionDetailsAttribute = new Attribute<EmailIdentityDetails>('ses-identity-lookup', jsonCodec<EmailIdentityDetails>());

/** Read one identity metadata through a typed Amazon SES Query. */
export class IdentityLookupFlow implements Flow<GetEmailIdentityInput> {
  private readonly complete = new CompleteEmailIdentityDetails();
  private readonly read;

  /** Bind the static provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Amazon SES connection name differs');
    this.read = newGetEmailIdentityStep<GetEmailIdentityInput>({
      stepType: 'CallGetEmailIdentity',
      annotations: { groupId: 'provider', groupLabel: 'Amazon SES', explanation: 'Read one identity metadata from Amazon SES.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteGetEmailIdentity'),
    });
  }

  /** Return the stable example Flow identity. */ getFlowType(): string { return 'AmazonSESIdentityLookupTypeScript'; }
  /** Register the provider and completion Steps. */
  getSteps(): StepList<GetEmailIdentityInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Persist the selected identity metadata result. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [functionDetailsAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected identity metadata and complete the Flow."
class CompleteEmailIdentityDetails implements Step<QueryResult<EmailIdentityDetails>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteGetEmailIdentity'; }
  /** Persist and return the selected identity metadata. */
  execute(context: Context, result: QueryResult<EmailIdentityDetails>): StepDecision {
    functionDetailsAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
