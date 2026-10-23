#!/usr/bin/env node
// Prints Markdown release notes for the commits since the previous release tag,
// grouped by conventional-commit type.
//
//   node scripts/release-notes.mjs [--to <rev>] [--from <tag>] [--compare-to <ref>]
//
// --to defaults to HEAD; --from defaults to the newest v* tag reachable from
// --to (all history when there is none). --compare-to names the end of the
// "full changelog" link, e.g. the tag about to be created. In GitHub Actions,
// commit hashes link to the repository.
import { execFileSync } from "node:child_process";

const args = process.argv.slice(2);
const arg = (name) => {
  const i = args.indexOf(`--${name}`);
  return i >= 0 ? args[i + 1] : undefined;
};
const git = (...a) => execFileSync("git", a, { encoding: "utf8", stdio: ["ignore", "pipe", "ignore"] }).trim();

const to = arg("to") ?? "HEAD";
let from = arg("from");
if (from === undefined) {
  try {
    from = git("describe", "--tags", "--abbrev=0", "--match", "v[0-9]*", to);
  } catch {
    from = ""; // no earlier release: include everything
  }
}

const range = from ? `${from}..${to}` : to;
const log = git("log", "--no-merges", "--format=%H%x1f%s%x1e", range);
const commits = log
  .split("\x1e")
  .map((r) => r.trim())
  .filter(Boolean)
  .map((r) => {
    const [hash, subject] = r.split("\x1f");
    return { hash, subject };
  });

// Same header shape the version calculation uses (SPEC: conventional commits).
const header = /^(?<type>[a-z]+)(?:\((?<scope>[^)]+)\))?(?<breaking>!)?:\s*(?<desc>.+)$/;
const sections = [
  ["breaking", "Breaking changes"],
  ["feat", "Features"],
  ["fix", "Fixes"],
  ["perf", "Performance"],
  ["refactor", "Refactoring"],
  ["revert", "Reverts"],
  ["docs", "Documentation"],
  ["style", "Style"],
  ["test", "Tests"],
  ["build", "Build"],
  ["ci", "CI"],
  ["chore", "Chores"],
  ["other", "Other changes"],
];
const known = new Set(sections.map(([k]) => k));
const groups = new Map(sections.map(([k]) => [k, []]));

const server = process.env.GITHUB_SERVER_URL;
const repo = process.env.GITHUB_REPOSITORY;
const ref = (hash) => {
  const short = hash.slice(0, 7);
  return server && repo ? `[\`${short}\`](${server}/${repo}/commit/${hash})` : `\`${short}\``;
};

for (const c of commits) {
  const m = header.exec(c.subject);
  if (!m?.groups) {
    groups.get("other").push(`- ${c.subject} (${ref(c.hash)})`);
    continue;
  }
  const { type, scope, breaking, desc } = m.groups;
  const line = `- ${scope ? `**${scope}:** ` : ""}${desc} (${ref(c.hash)})`;
  groups.get(breaking ? "breaking" : known.has(type) ? type : "other").push(line);
}

const out = [];
for (const [key, title] of sections) {
  const lines = groups.get(key);
  if (lines.length) out.push(`## ${title}`, "", ...lines, "");
}
if (!out.length) out.push("No changes.", "");
if (from && server && repo) out.push(`**Full changelog:** ${server}/${repo}/compare/${from}...${arg("compare-to") ?? to}`, "");
process.stdout.write(out.join("\n"));
