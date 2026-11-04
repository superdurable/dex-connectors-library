// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { MutationResult } from '@superdurable/connectors-sdk';
import { newCreateMessageStep, type Connection, type CreateMessageInput, type Message } from '@superdurable/connector-discord';

/** Static Discord Web connection name shared by Worker and starter. */
export const ConnectionName = 'discord-announcements';
/** Safe message request retained for inspection. */
export const announcementRequest = new Attribute<CreateMessageInput>('discord-announcement-request', jsonCodec<CreateMessageInput>());
/** Created message retained for inspection. */
export const announcementMessage = new Attribute<Message>('discord-announcement-message', jsonCodec<Message>());

/** Send one nonce-deduplicated, mention-safe announcement. */
export class ChannelAnnouncementFlow implements Flow<CreateMessageInput> {
  private readonly prepare = new PrepareAnnouncement();
  private readonly complete = new CompleteAnnouncement();
  /** Bind the configured Discord connection. */
  constructor(private readonly connection: Connection) { if (!connection) throw new TypeError('Discord connection is required'); }
  /** Stable Flow identity. */ getFlowType(): string { return 'DiscordChannelAnnouncementFlow'; }
  /** Register preparation, provider, and completion Steps. */
  getSteps(): StepList<CreateMessageInput> {
    return StepList.startStep(this.prepare).otherSteps(
      newCreateMessageStep({
        stepType: 'CreateDiscordAnnouncement',
        annotations: { groupId: 'discord-announcement', groupLabel: 'Discord Announcement', explanation: 'Create one mention-safe Discord message.' },
        connection: this.connection, connectionName: ConnectionName,
        mapToOperationInput: input => input,
        created: this.complete,
      }), this.complete,
    );
  }
  /** Register persisted Attributes. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [announcementRequest, announcementMessage] }; }
}

/** Validate the channel ID before creating a message. */
// dex:group group-id:discord-announcement group-label:"Discord Announcement"
// dex:explanation text:"Validate the destination channel before creating the announcement."
export class PrepareAnnouncement implements Step<CreateMessageInput> {
  /** Stable Step identity. */ getStepType(): string { return 'PrepareDiscordAnnouncement'; }
  /** Persist safe input and start the provider Step. */
  execute(context: Context, input: CreateMessageInput): StepDecision {
    if (!/^[0-9]{17,20}$/.test(input.channelId) || !input.content?.trim()) return forceFail('Discord channelId and content are required');
    announcementRequest.set(context, input);
    return goTo(new StepRef('CreateDiscordAnnouncement'), input);
  }
}

/** Persist and return the created announcement. */
// dex:group group-id:discord-announcement group-label:"Discord Announcement"
// dex:explanation text:"Retain the created announcement and complete the Flow."
export class CompleteAnnouncement implements Step<MutationResult<Message>> {
  /** Stable Step identity. */ getStepType(): string { return 'CompleteDiscordAnnouncement'; }
  /** Retain safe result fields and complete. */
  execute(context: Context, result: MutationResult<Message>): StepDecision {
    announcementMessage.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
