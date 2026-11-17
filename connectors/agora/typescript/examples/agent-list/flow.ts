// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListRunningAgentsStep, type Connection, type ListRunningAgentsInput, type RunningAgents } from '../../src/index.js';

/** Static Dex Web connection for the Agora example. */
export const ConnectionName = 'agora';
const agentsResult = new Attribute<RunningAgents>('agora-running-agents-result', jsonCodec<RunningAgents>());

/** Read one page of running Agora Conversational AI agents. */
export class RunningAgentsFlow implements Flow<ListRunningAgentsInput> {
  private readonly start = new StartAgentList();
  private readonly complete = new CompleteAgentList();
  /** Bind the static project connection. */
  constructor(private readonly connection: Connection) { if (connection.reference.name !== ConnectionName) throw new TypeError('Agora connection name differs'); }
  /** Preserve the Flow identity. */ getFlowType(): string { return 'AgoraRunningAgents'; }
  /** Register the provider and terminal Steps. */
  getSteps(): StepList<ListRunningAgentsInput> {
    return StepList.startStep(this.start).otherSteps(newListRunningAgentsStep({
      stepType: 'CallAgora',
      annotations: { groupId: 'agora', groupLabel: 'Agora', explanation: 'Read one page of currently running Conversational AI agents.' },
      connection: this.connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, listed: new StepRef('CompleteAgoraAgentList'),
    }), this.complete);
  }
  /** Register the durable agent page. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [agentsResult] }; }
}

// dex:group group-id:agora group-label:"Agora"
// dex:explanation text:"Start the Agora running agent query."
class StartAgentList implements Step<ListRunningAgentsInput> {
  getStepType(): string { return 'StartAgoraAgentList'; }
  execute(_context: Context, input: ListRunningAgentsInput): StepDecision { return goTo(new StepRef('CallAgora'), input); }
}

// dex:group group-id:agora group-label:"Agora"
// dex:explanation text:"Persist the agent page and complete the Flow."
class CompleteAgentList implements Step<QueryResult<RunningAgents>> {
  getStepType(): string { return 'CompleteAgoraAgentList'; }
  execute(context: Context, result: QueryResult<RunningAgents>): StepDecision { agentsResult.set(context, result.value); return gracefulComplete(result.value); }
}
