import { describe, it } from "node:test";
import assert from "node:assert";
import { orbStateForPhase, jobOrbState } from "./orbState.ts";

describe("orbStateForPhase", () => {
  it("maps the file loop's phases", () => {
    assert.strictEqual(orbStateForPhase("initial_test"), "solving");
    assert.strictEqual(orbStateForPhase("actor"), "composing");
    assert.strictEqual(orbStateForPhase("critic"), "breathing");
    assert.strictEqual(orbStateForPhase("test"), "solving");
  });

  it("maps the live progress phases that carry a command", () => {
    assert.strictEqual(orbStateForPhase("clone"), "connecting");
    assert.strictEqual(orbStateForPhase("install: npm ci"), "working");
    assert.strictEqual(orbStateForPhase("test: go test ./..."), "solving");
  });

  it("narrows session tool phases to reading vs writing vs running", () => {
    assert.strictEqual(orbStateForPhase("actor:read_file"), "searching");
    assert.strictEqual(orbStateForPhase("actor:grep"), "searching");
    assert.strictEqual(orbStateForPhase("actor:list_files"), "searching");
    assert.strictEqual(orbStateForPhase("actor:edit_file"), "composing");
    assert.strictEqual(orbStateForPhase("actor:write_file"), "composing");
    assert.strictEqual(orbStateForPhase("actor:run"), "working");
  });

  it("falls back to the actor's own state for an unknown tool", () => {
    // A new tool must not make the orb go generic — it is still the actor.
    assert.strictEqual(orbStateForPhase("actor:rename_symbol"), "composing");
  });

  it("maps the remaining session phases", () => {
    assert.strictEqual(orbStateForPhase("implementer"), "composing");
    assert.strictEqual(orbStateForPhase("compaction"), "weaving");
  });

  it("animates rather than going blank on an unknown phase", () => {
    assert.strictEqual(orbStateForPhase("teleporting"), "working");
    assert.strictEqual(orbStateForPhase("teleporting: fast"), "working");
  });

  it("handles absent and empty input", () => {
    assert.strictEqual(orbStateForPhase(undefined), "working");
    assert.strictEqual(orbStateForPhase(null), "working");
    assert.strictEqual(orbStateForPhase(""), "working");
  });

  it("tolerates surrounding whitespace", () => {
    assert.strictEqual(orbStateForPhase("  critic  "), "breathing");
    assert.strictEqual(orbStateForPhase("actor: read_file"), "searching");
  });
});

describe("jobOrbState", () => {
  it("returns null when nothing is running, so no orb is drawn", () => {
    assert.strictEqual(jobOrbState([]), null);
    assert.strictEqual(jobOrbState([{ status: "QUEUED" }]), null);
    assert.strictEqual(jobOrbState([{ status: "SUCCEEDED", phase: "test" }]), null);
  });

  it("uses the first executing task's phase", () => {
    assert.strictEqual(
      jobOrbState([
        { status: "SUCCEEDED", phase: "actor" },
        { status: "RUNNING", phase: "actor:read_file" },
      ]),
      "searching",
    );
  });

  it("treats LEASED as executing, since that is how a leased task reports", () => {
    assert.strictEqual(jobOrbState([{ status: "LEASED", phase: "critic" }]), "breathing");
  });

  it("still animates a running task that has reported no phase yet", () => {
    assert.strictEqual(jobOrbState([{ status: "RUNNING" }]), "working");
  });
});
