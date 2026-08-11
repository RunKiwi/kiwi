import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { parseToolArgs, parseNumberedFile, languageOf, editDiff } from "./toolContent.ts";

describe("parseToolArgs", () => {
  it("pulls out an edit's before and after", () => {
    const args = parseToolArgs(
      JSON.stringify({ path: "pkg/loop/loop.go", old_string: "a := 1", new_string: "a := 2" }),
    );
    assert.equal(args.path, "pkg/loop/loop.go");
    assert.equal(args.oldString, "a := 1");
    assert.equal(args.newString, "a := 2");
  });

  it("pulls out a read's path and a run's command", () => {
    assert.equal(parseToolArgs(JSON.stringify({ path: "main.go" })).path, "main.go");
    assert.equal(parseToolArgs(JSON.stringify({ command: "go test ./..." })).command, "go test ./...");
  });

  // The input is head-truncated on the way in, so half a JSON object is the
  // normal case rather than a corruption worth showing an error for.
  it("survives input that is not JSON", () => {
    assert.deepEqual(parseToolArgs('{"path":"main.go'), {});
    assert.deepEqual(parseToolArgs(undefined), {});
    assert.deepEqual(parseToolArgs(""), {});
  });
});

describe("parseNumberedFile", () => {
  // read_file returns "<lineno>\t<text>" per line. The numbers are the gutter,
  // not part of the code — highlighting them as source would colour them as
  // integers and shift every line.
  it("splits the gutter from the source", () => {
    const parsed = parseNumberedFile("12\tfunc main() {\n13\t\tprintln(1)\n14\t}\n");
    assert.ok(parsed);
    assert.equal(parsed.startLine, 12);
    assert.equal(parsed.code, "func main() {\n\tprintln(1)\n}");
  });

  // A truncated read ends with a sentence telling the model how to continue.
  // It is prose, not code, and must not end up inside the highlighted block.
  it("separates the truncation note from the code", () => {
    const parsed = parseNumberedFile(
      "1\tpackage main\n\n... (showing lines 1-1 of 400; read further with offset=2)\n",
    );
    assert.ok(parsed);
    assert.equal(parsed.code, "package main");
    assert.match(parsed.note ?? "", /showing lines 1-1 of 400/);
  });

  it("returns null for output that is not a numbered file", () => {
    assert.equal(parseNumberedFile("edited pkg/loop/loop.go"), null);
    assert.equal(parseNumberedFile("(empty file)"), null);
    assert.equal(parseNumberedFile(""), null);
  });

  // Tabs inside the source must survive: leading indentation is the one thing
  // a reader uses to follow Go or Python at a glance.
  it("keeps tabs that belong to the code", () => {
    const parsed = parseNumberedFile("1\t\t\tdeeply indented");
    assert.equal(parsed?.code, "\t\tdeeply indented");
  });
});

describe("languageOf", () => {
  it("maps the extensions this codebase actually produces", () => {
    assert.equal(languageOf("pkg/loop/loop.go"), "go");
    assert.equal(languageOf("src/components/Thing.tsx"), "tsx");
    assert.equal(languageOf("lib/api.ts"), "typescript");
    assert.equal(languageOf("scripts/run.py"), "python");
    assert.equal(languageOf("migrations/0029_x.up.sql"), "sql");
    assert.equal(languageOf("Dockerfile"), "docker");
    assert.equal(languageOf("deploy/bootstrap.sh"), "shell");
  });

  // An unknown extension must still render, just without colour. Guessing a
  // language would highlight the wrong tokens, which reads as a bug in the file.
  it("falls back to plain text", () => {
    assert.equal(languageOf("notes.xyz"), "text");
    assert.equal(languageOf(""), "text");
    assert.equal(languageOf(undefined), "text");
  });
});

describe("editDiff", () => {
  it("marks what was removed and what replaced it", () => {
    const lines = editDiff("a := 1\nb := 2", "a := 2\nb := 2");
    const kinds = lines.map((l) => l.kind);
    assert.ok(kinds.includes("del"), "the old line should be marked removed");
    assert.ok(kinds.includes("add"), "the new line should be marked added");
    // The unchanged line is context, shown so the change has somewhere to sit.
    assert.ok(kinds.includes("ctx"), "unchanged lines should be context");
  });

  it("numbers both sides the way a diff does", () => {
    const lines = editDiff("one\ntwo", "one\nTWO");
    const ctx = lines.find((l) => l.kind === "ctx");
    assert.equal(ctx?.oldNo, 1);
    assert.equal(ctx?.newNo, 1);
    const del = lines.find((l) => l.kind === "del");
    assert.equal(del?.oldNo, 2);
    assert.equal(del?.newNo, undefined, "a removed line has no line on the new side");
    const add = lines.find((l) => l.kind === "add");
    assert.equal(add?.newNo, 2);
    assert.equal(add?.oldNo, undefined);
  });

  it("handles a pure insertion and a pure deletion", () => {
    assert.deepEqual(
      editDiff("", "added").map((l) => l.kind),
      ["add"],
    );
    assert.deepEqual(
      editDiff("gone", "").map((l) => l.kind),
      ["del"],
    );
  });

  it("returns nothing to draw when the strings match", () => {
    const lines = editDiff("same", "same");
    assert.ok(lines.every((l) => l.kind === "ctx"));
  });
});
