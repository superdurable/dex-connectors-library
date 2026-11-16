// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

const nanosecondsPerSecond = 1_000_000_000n;
const secondsPerDay = 86_400n;
/** Unix nanoseconds of Go's zero time, 0001-01-01T00:00:00Z. */
const goZeroUnixNanoseconds = -62_135_596_800n * nanosecondsPerSecond;
const timestampPattern = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.(\d{1,9}))?(Z|([+-])(\d{2}):(\d{2}))$/;

/** An instant with Go's nanosecond precision and the UTC offset it was written with, as Go's time.Time keeps them. */
export class GoInstant {
  /** Go's zero time, 0001-01-01T00:00:00Z. */
  static readonly zero = new GoInstant(goZeroUnixNanoseconds, 0);

  /** Hold unixNanoseconds since the Unix epoch, written with offsetMinutes east of UTC. */
  constructor(readonly unixNanoseconds: bigint, readonly offsetMinutes: number) {}

  /** Return the UTC instant of a Date, at its millisecond precision. */
  static fromDate(date: Date): GoInstant { return new GoInstant(BigInt(date.getTime()) * 1_000_000n, 0); }

  /** Report Go's IsZero: the instant is 0001-01-01T00:00:00Z in any offset. */
  isZero(): boolean { return this.unixNanoseconds === goZeroUnixNanoseconds; }
  /** Report Go's Before, which compares instants and ignores offsets. */
  isBefore(other: GoInstant): boolean { return this.unixNanoseconds < other.unixNanoseconds; }
  /** The same instant written in UTC, as Go's UTC method returns it. */
  utc(): GoInstant { return new GoInstant(this.unixNanoseconds, 0); }
  /** The instant nanoseconds later, keeping the offset, as Go's Add does. */
  addNanoseconds(nanoseconds: bigint): GoInstant { return new GoInstant(this.unixNanoseconds + nanoseconds, this.offsetMinutes); }
  /** The instant as a Date, truncated to milliseconds. */
  toDate(): Date { return new Date(Number(floorDivide(this.unixNanoseconds, 1_000_000n))); }

  /** Format as Go's time.Time MarshalJSON does: RFC 3339 with trailing fraction zeros trimmed. */
  format(): string {
    const local = this.unixNanoseconds + BigInt(this.offsetMinutes) * 60n * nanosecondsPerSecond;
    const seconds = floorDivide(local, nanosecondsPerSecond), nanoseconds = local - seconds * nanosecondsPerSecond;
    const days = floorDivide(seconds, secondsPerDay), secondOfDay = Number(seconds - days * secondsPerDay);
    const [year, month, day] = civilFromDays(days);
    let text = `${pad(year, 4)}-${pad(month, 2)}-${pad(day, 2)}T${pad(Math.floor(secondOfDay / 3600), 2)}:${pad(Math.floor(secondOfDay / 60) % 60, 2)}:${pad(secondOfDay % 60, 2)}`;
    const fraction = nanoseconds.toString().padStart(9, '0').replace(/0+$/, '');
    if (fraction) text += '.' + fraction;
    if (this.offsetMinutes === 0) return text + 'Z';
    const offset = Math.abs(this.offsetMinutes);
    return `${text}${this.offsetMinutes > 0 ? '+' : '-'}${pad(Math.floor(offset / 60), 2)}:${pad(offset % 60, 2)}`;
  }

  /** Format the UTC instant as a CQL date, yyyy-MM-dd HH:mm, as Go's "2006-01-02 15:04" layout does. */
  formatCQLMinute(): string { return this.utc().format().slice(0, 16).replace('T', ' '); }
}

/** Parse an RFC 3339 timestamp as Go's time.Time UnmarshalJSON does; returns undefined for one Go rejects. */
export function parseGoTime(text: string): GoInstant | undefined {
  const match = timestampPattern.exec(text);
  if (!match) return undefined;
  const [year, month, day, hour, minute, second] = match.slice(1, 7).map(Number) as [number, number, number, number, number, number];
  const monthDays = [31, isLeapYear(year) ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
  if (month < 1 || month > 12 || day < 1 || day > monthDays[month - 1]! || hour > 23 || minute > 59 || second > 59) return undefined;
  let offsetMinutes = 0;
  if (match[8] !== 'Z') {
    const zoneHour = Number(match[10]), zoneMinute = Number(match[11]);
    if (zoneHour > 23 || zoneMinute > 59) return undefined;
    offsetMinutes = (zoneHour * 60 + zoneMinute) * (match[9] === '+' ? 1 : -1);
  }
  const seconds = daysFromCivil(year, month, day) * secondsPerDay + BigInt(hour * 3600 + minute * 60 + second);
  const nanoseconds = BigInt((match[7] ?? '').padEnd(9, '0'));
  return new GoInstant((seconds - BigInt(offsetMinutes * 60)) * nanosecondsPerSecond + nanoseconds, offsetMinutes);
}

/** Parse with Go's time.Parse and the RFC3339Nano layout, which also accepts a one-digit hour. */
export function parseGoTimeLayout(text: string): GoInstant | undefined {
  return parseGoTime(text.length > 11 && text[10] === 'T' && text[12] === ':' ? text.slice(0, 11) + '0' + text.slice(11) : text);
}

function floorDivide(value: bigint, divisor: bigint): bigint {
  const quotient = value / divisor;
  return value % divisor !== 0n && (value < 0n) !== (divisor < 0n) ? quotient - 1n : quotient;
}
function pad(value: number, width: number): string { return String(value).padStart(width, '0'); }
function isLeapYear(year: number): boolean { return year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0); }

/** Days since 1970-01-01 of a proleptic Gregorian date, including year 0. */
function daysFromCivil(year: number, month: number, day: number): bigint {
  const shiftedYear = month <= 2 ? year - 1 : year;
  const era = Math.floor(shiftedYear / 400), yearOfEra = shiftedYear - era * 400;
  const dayOfYear = Math.floor((153 * (month + (month > 2 ? -3 : 9)) + 2) / 5) + day - 1;
  const dayOfEra = yearOfEra * 365 + Math.floor(yearOfEra / 4) - Math.floor(yearOfEra / 100) + dayOfYear;
  return BigInt(era * 146_097 + dayOfEra - 719_468);
}

function civilFromDays(dayCount: bigint): [number, number, number] {
  const days = Number(dayCount) + 719_468;
  const era = Math.floor(days / 146_097), dayOfEra = days - era * 146_097;
  const yearOfEra = Math.floor((dayOfEra - Math.floor(dayOfEra / 1460) + Math.floor(dayOfEra / 36_524) - Math.floor(dayOfEra / 146_096)) / 365);
  const dayOfYear = dayOfEra - (365 * yearOfEra + Math.floor(yearOfEra / 4) - Math.floor(yearOfEra / 100));
  const monthIndex = Math.floor((5 * dayOfYear + 2) / 153);
  const day = dayOfYear - Math.floor((153 * monthIndex + 2) / 5) + 1;
  const month = monthIndex + (monthIndex < 10 ? 3 : -9);
  return [yearOfEra + era * 400 + (month <= 2 ? 1 : 0), month, day];
}
