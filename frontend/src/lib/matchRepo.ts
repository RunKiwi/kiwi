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

  // Exact matches first — a loose substring match must never win over an
  // exact one just because it happens to sit earlier in the list.
  const exact = normalized.find(
    ({ full, name, normUrl }) =>
      normUrl === normClean ||
      full === normClean ||
      name === normClean ||
      normClean.endsWith("/" + name) ||
      full.endsWith("/" + normClean) ||
      normUrl.endsWith("/" + normClean)
  );
  if (exact) {
    return exact.item.url || `https://github.com/${exact.item.full_name || exact.item.name}`;
  }

  // Loose fallback: trust it only when exactly one connected repo matches,
  // so one repo's name being a substring of another's (acme/app vs
  // acme/app-backend) can't silently select the wrong repository.
  const loose = normalized.filter(({ full, name }) => name.length > 0 && (normClean.includes(name) || full.includes(normClean)));
  if (loose.length === 1) {
    return loose[0].item.url || `https://github.com/${loose[0].item.full_name || loose[0].item.name}`;
  }

  return clean.includes("://") || clean.includes("@") ? clean : `https://github.com/${clean}`;
}
