// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Poll schedules: interval, jitter, and backoff.

use std::time::Duration;

use crate::errors::ValidationError;

const DEFAULT_JITTER_FRACTION: f64 = 0.1;
const DEFAULT_MAXIMUM_BACKOFF: Duration = Duration::from_secs(15 * 60);
const DEFAULT_MAXIMUM_PAGES_PER_POLL: u32 = 10;

/// How often a runner polls and how it backs off.
#[derive(Clone, Copy, Debug, PartialEq)]
pub struct PollSchedule {
    /// Wait between the end of one poll of a poller and the start of its next; at least one
    /// second.
    pub interval: Duration,
    /// Spreads each wait uniformly within ±interval×fraction and delays each poller's first poll
    /// by a uniform [0, interval×fraction]. `None` means 0.1, zero disables jitter, and the
    /// maximum is 0.5.
    pub jitter_fraction: Option<f64>,
    /// Caps the wait after consecutive failures; zero means 15 minutes, or the interval when
    /// that is longer; otherwise at least the interval.
    pub maximum_backoff: Duration,
    /// Pages one poll reads before yielding to other pollers; zero means 10.
    pub maximum_pages_per_poll: u32,
}

impl PollSchedule {
    /// Creates a schedule with default jitter, backoff, and page limit.
    pub fn new(interval: Duration) -> Self {
        Self {
            interval,
            jitter_fraction: None,
            maximum_backoff: Duration::ZERO,
            maximum_pages_per_poll: 0,
        }
    }

    /// Returns the schedule with every default filled in.
    ///
    /// # Errors
    ///
    /// Returns [`ValidationError`] for an interval under one second, jitter outside 0 to 0.5,
    /// or a maximum backoff under the interval.
    pub fn resolve(&self) -> Result<PollSchedule, ValidationError> {
        if self.interval < Duration::from_secs(1) {
            return Err(ValidationError::new(
                "poll interval must be at least one second",
            ));
        }
        let jitter_fraction = self.jitter_fraction.unwrap_or(DEFAULT_JITTER_FRACTION);
        if !(0.0..=0.5).contains(&jitter_fraction) {
            return Err(ValidationError::new(
                "poll jitter fraction must be 0 to 0.5",
            ));
        }
        let maximum_backoff = if self.maximum_backoff.is_zero() {
            DEFAULT_MAXIMUM_BACKOFF.max(self.interval)
        } else {
            self.maximum_backoff
        };
        if maximum_backoff < self.interval {
            return Err(ValidationError::new(
                "poll maximum backoff must be at least the interval",
            ));
        }
        let maximum_pages_per_poll = if self.maximum_pages_per_poll == 0 {
            DEFAULT_MAXIMUM_PAGES_PER_POLL
        } else {
            self.maximum_pages_per_poll
        };
        Ok(PollSchedule {
            interval: self.interval,
            jitter_fraction: Some(jitter_fraction),
            maximum_backoff,
            maximum_pages_per_poll,
        })
    }

    /// Returns min(interval × 2^(failures-1), maximum backoff) for failures starting at 1.
    pub fn backoff(&self, failures: u32) -> Duration {
        let mut delay = self.interval;
        let mut completed = 1;
        while completed < failures && delay < self.maximum_backoff {
            delay *= 2;
            completed += 1;
        }
        delay.min(self.maximum_backoff)
    }

    /// Spreads `delay` uniformly within ±delay×jitter using `unit_random` in [0, 1).
    pub fn jittered(&self, delay: Duration, unit_random: f64) -> Duration {
        let fraction = self.jitter_fraction.unwrap_or(0.0);
        if fraction == 0.0 {
            return delay;
        }
        delay.mul_f64((1.0 + fraction * (2.0 * unit_random - 1.0)).max(0.0))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn resolves_defaults_and_backs_off() {
        let schedule = PollSchedule::new(Duration::from_secs(60))
            .resolve()
            .expect("valid schedule");
        assert_eq!(schedule.jitter_fraction, Some(0.1));
        assert_eq!(schedule.maximum_backoff, Duration::from_secs(900));
        assert_eq!(schedule.maximum_pages_per_poll, 10);
        assert_eq!(schedule.backoff(1), Duration::from_secs(60));
        assert_eq!(schedule.backoff(3), Duration::from_secs(240));
        assert_eq!(schedule.backoff(10), Duration::from_secs(900));
        assert_eq!(
            schedule.jittered(Duration::from_secs(60), 0.5),
            Duration::from_secs(60)
        );
        assert!(
            PollSchedule::new(Duration::from_millis(500))
                .resolve()
                .is_err()
        );
    }
}
