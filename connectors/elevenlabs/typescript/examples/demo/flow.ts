// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListVoicesStep, type Connection, type ListVoicesInput, type ListVoicesOutput } from '@superdurable/connector-elevenlabs';

/** Static Dex Web connection name shared by the Worker and starter. */
export const ConnectionName = 'elevenlabs';

/** Public voice page retained for Flow inspection. */
export const voicePage = new Attribute<ListVoicesOutput>('elevenlabs-voice-page', jsonCodec<ListVoicesOutput>());

/** List one bounded page of ElevenLabs voices. */
export class ElevenLabsDemoFlow implements Flow<ListVoicesInput> {
  private readonly prepare = new PrepareVoiceRequest();
  private readonly complete = new CompleteVoiceRequest();

  /** Bind the named connection during Worker registration. */
  constructor(private readonly connection: Connection) { if (!connection) throw new TypeError('ElevenLabs connection is required'); }
  /** Stable Flow identity. */ getFlowType(): string { return 'ElevenLabsTypeScriptDemo'; }
  /** Register provider call and completion Steps. */
  getSteps(): StepList<ListVoicesInput> {
    return StepList.startStep(this.prepare).otherSteps(
      newListVoicesStep({
        stepType: 'CallElevenLabsListVoices',
        annotations: { groupId: 'elevenlabs', groupLabel: 'ElevenLabs', explanation: 'List one bounded page of voices.' },
        connection: this.connection, connectionName: ConnectionName,
        mapToOperationInput: input => input,
        listed: this.complete,
      }),
      this.complete,
    );
  }
  /** Register the persisted result Attribute. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [voicePage] }; }
}

// dex:group group-id:elevenlabs group-label:"ElevenLabs"
// dex:explanation text:"Pass the typed page request to the provider Step."
/** Start the provider query with public pagination input. */
export class PrepareVoiceRequest implements Step<ListVoicesInput> {
  /** Stable Step identity. */ getStepType(): string { return 'PrepareElevenLabsVoiceRequest'; }
  /** Route the start input into listVoices. */
  execute(_context: Context, input: ListVoicesInput): StepDecision { return goTo(new StepRef('CallElevenLabsListVoices'), input); }
}

// dex:group group-id:elevenlabs group-label:"ElevenLabs"
// dex:explanation text:"Persist the returned voice page and complete the Flow."
/** Persist the successful provider result and complete. */
export class CompleteVoiceRequest implements Step<QueryResult<ListVoicesOutput>> {
  /** Stable Step identity. */ getStepType(): string { return 'CompleteElevenLabsVoiceRequest'; }
  /** Store only the returned public voice page. */
  execute(context: Context, result: QueryResult<ListVoicesOutput>): StepDecision {
    voicePage.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
