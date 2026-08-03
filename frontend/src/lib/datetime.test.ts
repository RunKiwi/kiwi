import { describe, it } from "node:test";
import assert from "node:assert";
import {
  shortTime,
  formatDuration,
  durationBetween,
  formatCost,
  formatTokens,
} from "./datetime.ts";

// A fixed "now" so weekday and year boundaries are deterministic.
const NOW = new Date("2026-08-03T14:30:00");

describe("shortTime", () => {
  it("shows only the clock for today", () => {
    const out = shortTime("2026-08-03T09:15:00", NOW);
    assert.match(out, /9:15/);
    assert.ok(!/Aug/.test(out), `today should not carry a date: ${out}`);
  });

  it("names yesterday rather than showing a bare time", () => {
    // The original bug in miniature: a bare "9:15 AM" on a job from another
    // day is indistinguishable from one an hour ago.
    assert.match(shortTime("2026-08-02T09:15:00", NOW), /^Yesterday /);
  });

  it("uses the weekday inside a week", () => {
    const out = shortTime("2026-07-30T09:15:00", NOW);
    assert.match(out, /^(Mon|Tue|Wed|Thu|Fri|Sat|Sun)/);
  });

  it("uses a date beyond a week, and adds the year only when it differs", () => {
    const thisYear = shortTime("2026-03-01T09:15:00", NOW);
    assert.match(thisYear, /Mar/);
    assert.ok(!/2026/.test(thisYear), `same year should omit the year: ${thisYear}`);

    assert.match(shortTime("2025-03-01T09:15:00", NOW), /2025/);
  });

  it("returns empty for an unparseable value rather than 'Invalid Date'", () => {
    assert.equal(shortTime("not-a-date", NOW), "");
    assert.equal(shortTime("", NOW), "");
  });
});

describe("formatDuration", () => {
  it("keeps sub-second precision", () => {
    // An instant phase is meaningful — rounding it to 0s would hide it.
    assert.equal(formatDuration(312), "312ms");
  });

  it("scales through seconds, minutes and hours", () => {
    assert.equal(formatDuration(8_000), "8s");
    assert.equal(formatDuration(90_000), "1m 30s");
    assert.equal(formatDuration(120_000), "2m");
    assert.equal(formatDuration(3_600_000), "1h");
    assert.equal(formatDuration(3_840_000), "1h 4m");
  });

  it("returns empty for nonsense input", () => {
    assert.equal(formatDuration(-1), "");
    assert.equal(formatDuration(NaN), "");
  });
});

describe("durationBetween", () => {
  it("measures the gap", () => {
    assert.equal(durationBetween("2026-08-03T14:00:00Z", "2026-08-03T14:04:04Z"), "4m 4s");
  });

  it("returns empty when an endpoint is missing, unparseable, or out of order", () => {
    assert.equal(durationBetween(undefined, "2026-08-03T14:00:00Z"), "");
    assert.equal(durationBetween("2026-08-03T14:00:00Z", undefined), "");
    assert.equal(durationBetween("nope", "2026-08-03T14:00:00Z"), "");
    // A finish before a start is clock skew, not a negative duration.
    assert.equal(durationBetween("2026-08-03T14:05:00Z", "2026-08-03T14:00:00Z"), "");
  });
});

describe("formatCost", () => {
  it("does not round a real cost down to free", () => {
    // A session round genuinely costs fractions of a cent; "$0.00" reads as
    // free and would make the spend view look broken.
    assert.equal(formatCost(0.0655), "$0.07");
    assert.equal(formatCost(0.0004), "$0.0004");
    assert.notEqual(formatCost(0.0004), "$0.00");
  });

  it("uses two decimals at or above a cent", () => {
    assert.equal(formatCost(1.5), "$1.50");
    assert.equal(formatCost(0.01), "$0.01");
  });

  it("distinguishes an actual zero", () => {
    assert.equal(formatCost(0), "$0.00");
  });
});

describe("formatTokens", () => {
  it("abbreviates past a thousand", () => {
    assert.equal(formatTokens(940), "940");
    assert.equal(formatTokens(1500), "1.5k");
    assert.equal(formatTokens(15_801), "16k");
    assert.equal(formatTokens(2_400_000), "2.4M");
  });

  it("returns empty for nonsense input", () => {
    assert.equal(formatTokens(-5), "");
    assert.equal(formatTokens(NaN), "");
  });
});
