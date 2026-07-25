import { describe, it } from "node:test";
import assert from "node:assert";
import { serializeTask } from "./serialize";

describe("serializeTask", () => {
  it("serializes plain prose correctly", () => {
    const doc = {
      type: "doc",
      content: [
        {
          type: "paragraph",
          content: [
            { type: "text", text: "Fix the thing" }
          ]
        },
        {
          type: "paragraph",
          content: [
            { type: "text", text: "Make it better" }
          ]
        }
      ]
    };
    const result = serializeTask(doc);
    assert.deepStrictEqual(result, {
      task: "Fix the thing\n\nMake it better"
    });
  });

  it("serializes a document with one of each chip correctly", () => {
    const doc = {
      type: "doc",
      content: [
        {
          type: "paragraph",
          content: [
            { type: "text", text: "Please fix " },
            { type: "refMention", attrs: { kind: "file", value: "src/main.go" } },
            { type: "text", text: " in branch " },
            { type: "refMention", attrs: { kind: "branch", value: "feature/auth" } },
            { type: "text", text: " and update " },
            { type: "refMention", attrs: { kind: "symbol", value: "AuthHandler" } }
          ]
        },
        {
          type: "paragraph",
          content: [
            { type: "text", text: "Reference job " },
            { type: "workMention", attrs: { kind: "task", value: "job_123", label: "job_123", mode: "Reference" } },
            { type: "text", text: " and issue " },
            { type: "workMention", attrs: { kind: "issue", value: "15", label: "15" } }
          ]
        },
        {
          type: "paragraph",
          content: [
            { type: "text", text: "Run " },
            { type: "actionMention", attrs: { kind: "test", value: "go test ./...", label: "go test ./..." } },
            { type: "text", text: " using model " },
            { type: "actionMention", attrs: { kind: "model", value: "claude-opus-4-8", label: "claude-opus-4-8" } },
            { type: "text", text: " with workers " },
            { type: "actionMention", attrs: { kind: "workers", value: "3", label: "3 workers" } }
          ]
        }
      ]
    };
    const result = serializeTask(doc);
    
    // Check fields
    assert.deepStrictEqual(result.files, ["src/main.go"]);
    assert.strictEqual(result.ref, "feature/auth");
    assert.deepStrictEqual(result.reference_job_ids, ["job_123"]);
    assert.strictEqual(result.reference_mode, "manual");
    assert.strictEqual(result.test_cmd, "go test ./...");
    assert.strictEqual(result.model, "claude-opus-4-8");
    assert.strictEqual(result.max_workers, 3);

    // Check task string
    const expectedTask = `Please fix @src/main.go in branch @feature/auth and update @AuthHandler\n\nReference job #job_123 and issue #15\n\nRun /go test ./... using model /claude-opus-4-8 with workers /3 workers`;
    assert.strictEqual(result.task, expectedTask);
  });
});
