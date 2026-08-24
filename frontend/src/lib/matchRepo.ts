import type { GithubRepo } from "./api.ts";

// Resolve a repo name, short name, full name, or URL to a connected repo's URL.
export function matchRepo(target: string, list: GithubRepo[]): string {
  if (!target) return "";
  const clean = target.trim();
  const normClean = clean.toLowerCase().replace(/\.git$/, "").replace(/^https?:\/\/github\.com\//, "").replace(/^git@github\.com:/, "");

  const normalized = list.map((item) => {
    const full = (item.full_name || "").toLowerCase().replace(/\.git$/, "");
    const name = (item.name || "").toLowerCase();
    const normUrl = (item.url || "").toLowerCase().replace(/\.git$/, "").replace(/^https?:\/\/github\.com\//, "").replace(/^git@github\.com:/, "");
    return { item, full, name, normUrl };
  });

  const resolve = (entry: (typeof normalized)[number]) =>
    entry.item.url || `https://github.com/${entry.item.full_name || entry.item.name}`;

  // Strict identity first: a full name or URL match always wins.
  const strict = normalized.find(
    ({ full, normUrl }) => normUrl === normClean || full === normClean,
  );
  if (strict) return resolve(strict);

  // Short-name / suffix matches only when exactly one repo qualifies.
  const bySuffix = normalized.filter(
    ({ full, name, normUrl }) =>
      (name.length > 0 && (name === normClean || normClean.endsWith("/" + name))) ||
      full.endsWith("/" + normClean) ||
      normUrl.endsWith("/" + normClean),
  );
  if (bySuffix.length === 1) return resolve(bySuffix[0]);

  // Loose fallback: trust it only when exactly one connected repo matches,
  // so one repo's name being a substring of another's (acme/app vs
  // acme/app-backend) can't silently select the wrong repository.
  const loose = normalized.filter(({ full, name }) => name.length > 0 && (normClean.includes(name) || full.includes(normClean)));
  if (loose.length === 1) {
    return resolve(loose[0]);
  }

  return clean.includes("://") || clean.includes("@") ? clean : `https://github.com/${clean}`;
}
