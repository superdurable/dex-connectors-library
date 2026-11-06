// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetVideoStep, type Connection, type GetVideoInput, type Video } from '@superdurable/connector-google-youtube';

/** Static connection selected in Dex Web. */
export const ConnectionName = 'youtube-video-reader';
/** Persisted video response. */
export const videoResult = new Attribute<Video>('youtube-video-result', jsonCodec<Video>());

/** Read one YouTube video through a Dex Step. */
export class VideoLookupFlow implements Flow<GetVideoInput> {
  private readonly prepare = new PrepareVideoLookup();
  private readonly complete = new CompleteVideoLookup();
  /** Bind the static provider connection. */ constructor(private readonly connection: Connection) {}
  /** Stable Flow identity. */ getFlowType(): string { return 'YouTubeVideoLookupFlow'; }
  /** Register the query and completion Steps. */
  getSteps(): StepList<GetVideoInput> {
    return StepList.startStep(this.prepare).otherSteps(
      newGetVideoStep({ stepType: 'GetYouTubeVideo', annotations: { groupId: 'youtube-video', groupLabel: 'YouTube Video', explanation: 'Read the selected video from YouTube.' }, connection: this.connection, connectionName, mapToOperationInput: input => input, found: this.complete }),
      this.complete,
    );
  }
  /** Register the persisted provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [videoResult] }; }
}
const connectionName = ConnectionName;

// dex:group group-id:youtube-video group-label:"YouTube Video"
// dex:explanation text:"Validate the video ID before invoking the provider."
/** Validate a video selection. */
export class PrepareVideoLookup implements Step<GetVideoInput> {
  /** Stable Step identity. */ getStepType(): string { return 'PrepareYouTubeVideoLookup'; }
  /** Start the provider query. */ execute(_context: Context, input: GetVideoInput): StepDecision {
    if (!/^[A-Za-z0-9_-]{11}$/.test(input.videoId)) return forceFail('YouTube video ID is invalid');
    return goTo(new StepRef('GetYouTubeVideo'), input);
  }
}

// dex:group group-id:youtube-video group-label:"YouTube Video"
// dex:explanation text:"Persist the video details and complete the Flow."
/** Complete the Flow with one video. */
export class CompleteVideoLookup implements Step<QueryResult<Video>> {
  /** Stable Step identity. */ getStepType(): string { return 'CompleteYouTubeVideoLookup'; }
  /** Persist only provider public data. */ execute(context: Context, result: QueryResult<Video>): StepDecision {
    videoResult.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
