import { describe, it } from "node:test";
import assert from "node:assert";
import { matchRepo } from "./matchRepo.ts";
import type { GithubRepo } from "./api.ts";

function repo(full_name: string): GithubRepo {
  return { full_name, name: full_name.split("/")[1], url: `https://github.com/${full_name}`, private: false, default_branch: "main" };
}

describe("matchRepo", () => {
  it("does not let a substring match steal the wrong repo when an exact match exists elsewhere", () => {
    const list = [repo("acme/app"), repo("acme/app-backend")];
    const got = matchRepo("acme/app-backend", list);
    assert.strictEqual(got, "https://github.com/acme/app-backend");
  });

  it("matches on exact full name regardless of list order", () => {
    const list = [repo("acme/app-backend"), repo("acme/app")];
    const got = matchRepo("acme/app", list);
    assert.strictEqual(got, "https://github.com/acme/app");
  });

  it("falls back to a loose substring match when it is unambiguous", () => {
    const list = [repo("acme/app")];
    const got = matchRepo("some-org/app", list);
    assert.strictEqual(got, "https://github.com/acme/app");
  });

  it("does not guess when the loose fallback is ambiguous across multiple repos", () => {
    const list = [repo("acme/app"), repo("other/app")];
    const got = matchRepo("nowhere/app-thing", list);
    // Neither repo is trustworthy here; falls through to treating input as a URL/path.
    assert.strictEqual(got, "https://github.com/nowhere/app-thing");
  });

  it("returns empty string for empty input", () => {
    assert.strictEqual(matchRepo("", []), "");
  });
});
