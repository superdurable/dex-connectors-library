// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Go-compatible RFC 3339 timestamps that keep their wire precision and offset.

use std::fmt;
use std::time::{SystemTime, UNIX_EPOCH};

use serde::{Deserialize, Deserializer, Serialize, Serializer};

use crate::errors::ValidationError;

/// Offset-preserving Go `time.Time` JSON value with up to nine fractional digits.
///
/// The canonical form matches Go RFC3339Nano: a non-UTC numeric offset is kept, `+00:00`
/// becomes `Z`, whole seconds omit the fraction, and trailing fractional zeroes are trimmed.
/// The zero time is `0001-01-01T00:00:00Z`.
#[derive(Clone, Debug, Eq, Hash, PartialEq)]
pub struct Rfc3339NanoTimestamp {
    wire: String,
    unix_nanoseconds: i128,
}

/// Canonical Go zero `time.Time`.
pub const ZERO_TIMESTAMP_WIRE: &str = "0001-01-01T00:00:00Z";

impl Rfc3339NanoTimestamp {
    /// Validates a Go `time.Time` JSON string and canonicalizes it without rounding.
    ///
    /// # Errors
    ///
    /// Returns [`ValidationError`] for an invalid date, clock time, fraction, or offset.
    pub fn from_wire(value: &str) -> Result<Self, ValidationError> {
        let parts = GoTimeParts::parse(value)?;
        Ok(Self {
            wire: parts.canonical(),
            unix_nanoseconds: parts.unix_nanoseconds(),
        })
    }

    /// Returns the Go zero time `0001-01-01T00:00:00Z`.
    pub fn zero() -> Self {
        Self::from_unix_nanoseconds_utc(GoTimeParts::zero_unix_nanoseconds())
    }

    /// Formats the current time in UTC with nanosecond precision.
    pub fn now_utc() -> Self {
        Self::from_system_time_utc(SystemTime::now())
    }

    /// Formats a system time in UTC with nanosecond precision.
    pub fn from_system_time_utc(value: SystemTime) -> Self {
        let unix_nanoseconds = match value.duration_since(UNIX_EPOCH) {
            Ok(after) => i128::try_from(after.as_nanos()).unwrap_or(i128::MAX),
            Err(before) => -i128::try_from(before.duration().as_nanos()).unwrap_or(i128::MAX),
        };
        Self::from_unix_nanoseconds_utc(unix_nanoseconds)
    }

    fn from_unix_nanoseconds_utc(unix_nanoseconds: i128) -> Self {
        let seconds = unix_nanoseconds.div_euclid(1_000_000_000);
        let fraction = unix_nanoseconds.rem_euclid(1_000_000_000);
        let days = seconds.div_euclid(86_400);
        let seconds_of_day = seconds.rem_euclid(86_400);
        let (year, month, day) = civil_from_days(days);
        let mut wire = format!(
            "{year:04}-{month:02}-{day:02}T{:02}:{:02}:{:02}",
            seconds_of_day / 3600,
            (seconds_of_day / 60) % 60,
            seconds_of_day % 60
        );
        if fraction != 0 {
            let digits = format!("{fraction:09}");
            wire.push('.');
            wire.push_str(digits.trim_end_matches('0'));
        }
        wire.push('Z');
        Self {
            wire,
            unix_nanoseconds,
        }
    }

    /// Returns the canonical wire string.
    pub fn as_wire_str(&self) -> &str {
        &self.wire
    }

    /// Returns whether this instant is Go's zero time, regardless of offset.
    pub fn is_zero(&self) -> bool {
        self.unix_nanoseconds == GoTimeParts::zero_unix_nanoseconds()
    }

    /// Returns nanoseconds since the Unix epoch.
    pub fn unix_nanoseconds(&self) -> i128 {
        self.unix_nanoseconds
    }
}

impl Default for Rfc3339NanoTimestamp {
    fn default() -> Self {
        Self::zero()
    }
}

impl fmt::Display for Rfc3339NanoTimestamp {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str(&self.wire)
    }
}

impl Serialize for Rfc3339NanoTimestamp {
    fn serialize<S: Serializer>(&self, serializer: S) -> Result<S::Ok, S::Error> {
        serializer.serialize_str(&self.wire)
    }
}

impl<'de> Deserialize<'de> for Rfc3339NanoTimestamp {
    fn deserialize<D: Deserializer<'de>>(deserializer: D) -> Result<Self, D::Error> {
        let wire = String::deserialize(deserializer)?;
        Self::from_wire(&wire).map_err(serde::de::Error::custom)
    }
}

/// Formats a timestamp in its canonical Go RFC3339Nano form.
pub fn format_rfc3339_nano(value: &Rfc3339NanoTimestamp) -> String {
    value.wire.clone()
}

struct GoTimeParts {
    year: i64,
    month: u32,
    day: u32,
    hour: u32,
    minute: u32,
    second: u32,
    fraction: String,
    offset_minutes: i64,
    offset_text: String,
}

impl GoTimeParts {
    fn parse(value: &str) -> Result<Self, ValidationError> {
        let invalid = || ValidationError::new("timestamp must be a Go RFC 3339 time");
        let bytes = value.as_bytes();
        if bytes.len() < 19 || !value.is_ascii() {
            return Err(invalid());
        }
        let digits = |range: std::ops::Range<usize>| -> Result<u32, ValidationError> {
            let text = value.get(range).ok_or_else(invalid)?;
            if text.is_empty() || !text.bytes().all(|byte| byte.is_ascii_digit()) {
                return Err(invalid());
            }
            text.parse::<u32>().map_err(|_| invalid())
        };
        if bytes[4] != b'-' || bytes[7] != b'-' || bytes[10] != b'T' {
            return Err(invalid());
        }
        let year = i64::from(digits(0..4)?);
        let month = digits(5..7)?;
        let day = digits(8..10)?;
        let hour_end = if bytes.get(12) == Some(&b':') {
            12
        } else if bytes.get(13) == Some(&b':') {
            13
        } else {
            return Err(invalid());
        };
        let hour = digits(11..hour_end)?;
        let minute = digits(hour_end + 1..hour_end + 3)?;
        if bytes.get(hour_end + 3) != Some(&b':') {
            return Err(invalid());
        }
        let second = digits(hour_end + 4..hour_end + 6)?;
        let mut index = hour_end + 6;
        let mut fraction = String::new();
        if matches!(bytes.get(index), Some(b'.') | Some(b',')) {
            index += 1;
            let start = index;
            while bytes.get(index).is_some_and(u8::is_ascii_digit) {
                index += 1;
            }
            if index == start {
                return Err(invalid());
            }
            fraction = value[start..index].to_string();
        }
        let offset = &value[index..];
        let (offset_minutes, offset_text) = if offset == "Z" {
            (0, "Z".to_string())
        } else {
            let offset_bytes = offset.as_bytes();
            if offset_bytes.len() != 6
                || !matches!(offset_bytes[0], b'+' | b'-')
                || offset_bytes[3] != b':'
            {
                return Err(invalid());
            }
            let hours = digits(index + 1..index + 3)?;
            let minutes = digits(index + 4..index + 6)?;
            if hours > 24 || minutes > 60 {
                return Err(ValidationError::new(
                    "timestamp has an invalid timezone offset",
                ));
            }
            let magnitude = i64::from(hours) * 60 + i64::from(minutes);
            if magnitude == 0 {
                (0, "Z".to_string())
            } else if offset_bytes[0] == b'-' {
                (-magnitude, offset.to_string())
            } else {
                (magnitude, offset.to_string())
            }
        };
        if !(1..=12).contains(&month) || day == 0 || day > days_in_month(year, month) {
            return Err(ValidationError::new("timestamp has an invalid date"));
        }
        if hour > 23 || minute > 59 || second > 59 {
            return Err(ValidationError::new("timestamp has an invalid clock time"));
        }
        // Go truncates digits beyond nanoseconds, so they are accepted and dropped.
        Ok(Self {
            year,
            month,
            day,
            hour,
            minute,
            second,
            fraction,
            offset_minutes,
            offset_text,
        })
    }

    fn canonical(&self) -> String {
        let fraction = self.fraction[..self.fraction.len().min(9)].trim_end_matches('0');
        let mut wire = format!(
            "{:04}-{:02}-{:02}T{:02}:{:02}:{:02}",
            self.year, self.month, self.day, self.hour, self.minute, self.second
        );
        if !fraction.is_empty() {
            wire.push('.');
            wire.push_str(fraction);
        }
        wire.push_str(&self.offset_text);
        wire
    }

    fn unix_nanoseconds(&self) -> i128 {
        let days = days_from_civil(self.year, self.month, self.day);
        let seconds = i128::from(days) * 86_400
            + i128::from(self.hour) * 3600
            + i128::from(self.minute) * 60
            + i128::from(self.second)
            - i128::from(self.offset_minutes) * 60;
        let mut fraction_digits = self.fraction.clone();
        fraction_digits.truncate(9);
        while fraction_digits.len() < 9 {
            fraction_digits.push('0');
        }
        let fraction = fraction_digits.parse::<i128>().unwrap_or(0);
        seconds * 1_000_000_000 + fraction
    }

    fn zero_unix_nanoseconds() -> i128 {
        i128::from(days_from_civil(1, 1, 1)) * 86_400 * 1_000_000_000
    }
}

fn is_leap_year(year: i64) -> bool {
    (year % 4 == 0 && year % 100 != 0) || year % 400 == 0
}

fn days_in_month(year: i64, month: u32) -> u32 {
    match month {
        2 if is_leap_year(year) => 29,
        2 => 28,
        4 | 6 | 9 | 11 => 30,
        _ => 31,
    }
}

// Howard Hinnant's proleptic Gregorian day algorithms.
fn days_from_civil(year: i64, month: u32, day: u32) -> i64 {
    let shifted_year = if month <= 2 { year - 1 } else { year };
    let era = shifted_year.div_euclid(400);
    let year_of_era = shifted_year - era * 400;
    let month_index = i64::from((month + 9) % 12);
    let day_of_year = (153 * month_index + 2) / 5 + i64::from(day) - 1;
    let day_of_era = year_of_era * 365 + year_of_era / 4 - year_of_era / 100 + day_of_year;
    era * 146_097 + day_of_era - 719_468
}

#[expect(
    clippy::cast_possible_truncation,
    clippy::cast_sign_loss,
    reason = "month and day results are bounded by the civil calendar"
)]
fn civil_from_days(days: i128) -> (i128, u32, u32) {
    let shifted = days + 719_468;
    let era = shifted.div_euclid(146_097);
    let day_of_era = shifted - era * 146_097;
    let year_of_era =
        (day_of_era - day_of_era / 1460 + day_of_era / 36_524 - day_of_era / 146_096) / 365;
    let day_of_year = day_of_era - (365 * year_of_era + year_of_era / 4 - year_of_era / 100);
    let month_index = (5 * day_of_year + 2) / 153;
    let day = (day_of_year - (153 * month_index + 2) / 5 + 1) as u32;
    let month = if month_index < 10 {
        month_index + 3
    } else {
        month_index - 9
    } as u32;
    let year = year_of_era + era * 400 + i128::from(month <= 2);
    (year, month, day)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn canonicalizes_go_time_json() {
        let cases = [
            ("2026-10-29T12:30:45.123000Z", "2026-10-29T12:30:45.123Z"),
            ("2026-10-29T12:30:45+00:00", "2026-10-29T12:30:45Z"),
            (
                "2026-10-29T12:30:45.123456789+08:00",
                "2026-10-29T12:30:45.123456789+08:00",
            ),
            ("0001-01-01T00:00:00Z", "0001-01-01T00:00:00Z"),
        ];
        for (input, expected) in cases {
            let parsed = Rfc3339NanoTimestamp::from_wire(input).expect("valid timestamp");
            assert_eq!(parsed.as_wire_str(), expected);
        }
        assert!(Rfc3339NanoTimestamp::from_wire("2026-02-30T00:00:00Z").is_err());
        assert!(Rfc3339NanoTimestamp::from_wire("2026-10-29T24:00:00Z").is_err());
        assert!(Rfc3339NanoTimestamp::from_wire("2026-10-29T12:00:00").is_err());
    }

    #[test]
    fn zero_and_now_are_utc() {
        assert_eq!(
            Rfc3339NanoTimestamp::zero().as_wire_str(),
            ZERO_TIMESTAMP_WIRE
        );
        assert!(Rfc3339NanoTimestamp::zero().is_zero());
        let epoch = Rfc3339NanoTimestamp::from_system_time_utc(UNIX_EPOCH);
        assert_eq!(epoch.as_wire_str(), "1970-01-01T00:00:00Z");
        let later = Rfc3339NanoTimestamp::from_system_time_utc(
            UNIX_EPOCH + std::time::Duration::new(1_793_276_245, 123_000_000),
        );
        assert_eq!(later.as_wire_str(), "2026-10-29T12:17:25.123Z");
        assert!(Rfc3339NanoTimestamp::now_utc().as_wire_str().ends_with('Z'));
    }
}
