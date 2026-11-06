// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListMeetingsStep, type Connection, type ListMeetingsInput, type MeetingPage } from '../../src/index.js';

/** Static Dex Web connection name shared with the Go booked-meeting example. */
export const ConnectionName = 'zoom-scheduler';
/** Persist the requested meeting page across retries. */
export const meetingRequest = new Attribute<ListMeetingsInput>('zoom-meeting-list-request', jsonCodec<ListMeetingsInput>());
/** Persist the safe provider page for Flow inspection. */
export const meetingPage = new Attribute<MeetingPage>('zoom-meeting-list-page', jsonCodec<MeetingPage>());

/** List one page of the authorized user's meetings. */
export class MeetingListFlow implements Flow<ListMeetingsInput> {
  private readonly prepare = new PrepareMeetingList();
  private readonly complete = new CompleteMeetingList();
  /** Bind the static Zoom connection. */ constructor(private readonly connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Zoom meeting list requires its static connection');
  }
  /** Stable Flow type. */ getFlowType() { return 'ZoomMeetingListFlow'; }
  /** Register the start, provider, and completion Steps. */
  getSteps(): StepList<ListMeetingsInput> {
    return StepList.startStep(this.prepare).otherSteps(newListMeetingsStep({
      stepType: 'ListZoomMeetings',
      annotations: { groupId: 'zoom-meetings', groupLabel: 'Zoom meetings', explanation: 'List one page of the authorized user’s Zoom meetings.' },
      connection: this.connection, connectionName: ConnectionName, mapToOperationInput: input => input, listed: this.complete,
    }), this.complete);
  }
  /** Register both durable Attributes. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [meetingRequest, meetingPage] }; }
}

// dex:group group-id:zoom-meetings group-label:"Zoom meetings"
// dex:explanation text:"Persist the requested page before listing the authorized user's Zoom meetings."
class PrepareMeetingList implements Step<ListMeetingsInput> {
  getStepType() { return 'PrepareZoomMeetingList'; }
  execute(context: Context, input: ListMeetingsInput): StepDecision { meetingRequest.set(context, input); return goTo(new StepRef('ListZoomMeetings'), input); }
}

// dex:group group-id:zoom-meetings group-label:"Zoom meetings"
// dex:explanation text:"Persist the safe meeting page and complete the Flow."
class CompleteMeetingList implements Step<QueryResult<MeetingPage>> {
  getStepType() { return 'CompleteZoomMeetingList'; }
  execute(context: Context, result: QueryResult<MeetingPage>): StepDecision { meetingPage.set(context, result.value); return gracefulComplete(result.value); }
}
