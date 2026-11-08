// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListClientsStep, type Connection, type ListClientsInput, type ClientsList } from '@superdurable/connector-campaignmonitor';

/** Static Dex Web connection for the Campaign Monitor example. */
export const ConnectionName = 'campaignmonitor';
const resultAttribute = new Attribute<ClientsList>('campaignmonitor-clients-list', jsonCodec<ClientsList>());

/** List clients visible to the connected Campaign Monitor account. */
export class ClientDiscoveryFlow implements Flow<ListClientsInput> {
  private readonly complete = new CompleteClientDiscovery();
  private readonly read;
  /** Bind the required static connection. */ constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Campaign Monitor connection name differs');
    this.read = newListClientsStep<ListClientsInput>({
      stepType: 'CallListClients',
      annotations: { groupId: 'provider', groupLabel: 'Campaign Monitor', explanation: 'List clients accessible to the connected account.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, listed: this.complete,
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'CampaignMonitorClientDiscovery'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<ListClientsInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Register the persisted provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the client list and complete the Flow."
class CompleteClientDiscovery implements Step<QueryResult<ClientsList>> {
  readonly inputCodec = jsonCodec<QueryResult<ClientsList>>();
  getStepType(): string { return 'CompleteListClients'; }
  execute(context: Context, result: QueryResult<ClientsList>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
