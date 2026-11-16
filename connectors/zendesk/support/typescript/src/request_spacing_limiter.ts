// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

/**
 * Starts at most one request per spacing, in arrival order, so a burst of polls stays inside Zendesk's per-minute
 * incremental export limit instead of drawing a 429. One limiter is shared by every ticketChanged binding of a
 * client.
 */
export class RequestSpacingLimiter {
  #nextStartMs = 0;

  /** Space request starts by spacingMs milliseconds. */
  constructor(private readonly spacingMs: number) {}

  /**
   * Resolve at the caller's reserved start time, or reject with the signal's reason once it aborts. A cancelled
   * wait keeps its reservation, which only delays the next request.
   */
  async waitForRequestStart(signal: AbortSignal): Promise<void> {
    signal.throwIfAborted();
    const nowMs = performance.now();
    const startMs = Math.max(nowMs, this.#nextStartMs);
    this.#nextStartMs = startMs + this.spacingMs;
    const delayMs = startMs - nowMs;
    if (delayMs <= 0) return;
    await new Promise<void>((resolve, reject) => {
      const onAbort = () => { clearTimeout(timer); reject(signal.reason); };
      const timer = setTimeout(() => { signal.removeEventListener('abort', onAbort); resolve(); }, delayMs);
      signal.addEventListener('abort', onAbort, { once: true });
    });
  }
}
