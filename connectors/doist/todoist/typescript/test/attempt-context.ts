// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

/** A Dex attempt context that keeps the last heartbeat across Execute attempts. */
export class AttemptContext {
  marker: unknown;
  readonly runId = 'run';
  readonly flowId = 'todoist-resend-flow';
  readonly flowStartedAt = new Date('1970-01-01T00:00:01Z');
  readonly deadline = new Date(Date.now() + 30_000);
  readonly cancellationSignal = new AbortController().signal;
  constructor(public attempt = 1, marker?: unknown) { this.marker = marker; }
  hasLastHeartbeatValue(): boolean { return this.marker !== undefined; }
  getLastHeartbeatValue(): unknown { return this.marker; }
  async recordHeartbeat(value: unknown): Promise<void> { this.marker = value; }
}
