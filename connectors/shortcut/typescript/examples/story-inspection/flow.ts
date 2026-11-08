// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  Attribute, StepList, StepRef, gracefulComplete, jsonCodec,
  type Context, type Flow, type PersistenceSchema, type Step, type StepDecision,
} from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import {
  newGetStoryStep, type Connection, type StoryMetadata, type GetStoryInput,
} from '@superdurable/connector-shortcut';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'shortcut';
const storyAttribute = new Attribute<StoryMetadata>('shortcut-story', jsonCodec<StoryMetadata>());

/** Read one story through a typed Shortcut Query. */
export class StoryInspectionFlow implements Flow<GetStoryInput> {
  private readonly complete = new CompleteStoryInspection();
  private readonly read;

  /** Bind the static provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Shortcut connection name differs');
    this.read = newGetStoryStep<GetStoryInput>({
      stepType: 'CallGetStory',
      annotations: { groupId: 'provider', groupLabel: 'Shortcut', explanation: 'Read one story from Shortcut.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteShortcut'),
    });
  }

  /** Return the stable example Flow identity. */ getFlowType(): string { return 'ShortcutStoryInspectionTypeScript'; }
  /** Register the provider and completion Steps. */
  getSteps(): StepList<GetStoryInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Persist the selected story result. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [storyAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected story and complete the Flow."
class CompleteStoryInspection implements Step<QueryResult<StoryMetadata>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteShortcut'; }
  /** Persist and return the selected story. */
  execute(context: Context, result: QueryResult<StoryMetadata>): StepDecision {
    storyAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
