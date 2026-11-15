// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

/** Time, jitter, and waiting for the poll runner; tests replace them, as Go tests replace package variables. */
export interface PollClock {
  /** Current time in milliseconds since the Unix epoch. */ now(): number;
  /** Uniform random number in [0, 1). */ jitter(): number;
  /** Resolve after delayMs or a wake-up, and reject as soon as signal aborts. */ waitForPoll(signal: AbortSignal, delayMs: number, wake: PollWakeSignal): Promise<void>;
}

/** A coalescing wake-up a runner's wait listens for. */
export class PollWakeSignal {
  #waiters: Array<() => void> = [];
  #isPending = false;

  /** Wake the current wait, or the next one when none is waiting. */
  notify(): void {
    if (this.#waiters.length === 0) { this.#isPending = true; return; }
    const waiters = this.#waiters;
    this.#waiters = [];
    for (const wake of waiters) wake();
  }

  /** Register a waiter; returns a function that removes it. A pending notification wakes it at once. */
  listen(wake: () => void): () => void {
    if (this.#isPending) { this.#isPending = false; wake(); return () => undefined; }
    this.#waiters.push(wake);
    return () => { this.#waiters = this.#waiters.filter(waiter => waiter !== wake); };
  }
}

/** Longest delay setTimeout honours; a longer one fires at once, so a wait is cut to it and the runner waits again. */
const maxTimerDelayMs = 2 ** 31 - 1;

/** The production clock. */
export const systemPollClock: PollClock = {
  now: () => Date.now(),
  jitter: () => Math.random(),
  waitForPoll: (signal, delayMs, wake) => new Promise((resolve, reject) => {
    if (signal.aborted) { reject(signal.reason); return; }
    let stopListening = (): void => undefined;
    const finish = () => { clearTimeout(timer); signal.removeEventListener('abort', onAbort); stopListening(); resolve(); };
    const onAbort = () => { clearTimeout(timer); stopListening(); reject(signal.reason); };
    const timer = setTimeout(finish, Math.min(Math.max(delayMs, 0), maxTimerDelayMs));
    signal.addEventListener('abort', onAbort, { once: true });
    stopListening = wake.listen(finish);
  }),
};

let activePollClock: PollClock = systemPollClock;

/** The clock runners use now. */
export function currentPollClock(): PollClock { return activePollClock; }

/** Replace the clock for tests and return a function that restores the previous one. Not exported by the package. */
export function replacePollClockForTests(clock: PollClock): () => void {
  const previous = activePollClock;
  activePollClock = clock;
  return () => { activePollClock = previous; };
}
