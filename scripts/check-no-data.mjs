#!/usr/bin/env node
/**
 * No user or customer data in git (hard rule 6, SchoolAI/sai-internal-apps docs/decisions/2026-10-28-2256-no-user-data-in-repos.md).
 *
 * One self-contained file, Node built-ins only, so app repos can vendor it unchanged as scripts/check-no-data.mjs
 * (templates/app-repo). The copy there must stay byte-identical to tools/lib/data-scan.mjs; a test enforces it.
 *
 * It flags two things in tracked (or staged) files:
 *   1. data files by name: spreadsheets, exports, dumps, databases, JSON under data/ or exports/ folders;
 *   2. content patterns: personal email addresses, US phone numbers, street addresses, ACH routing and bank account
 *      numbers, and credential prefixes (HubSpot pat-, OpenAI and Anthropic sk-, Slack xox, GitHub, AWS, Notion, 1Password).
 * It reports counts and file:line only, never a matched value, so its output is safe to paste into a PR or issue.
 *
 *   node scripts/check-no-data.mjs              # every tracked file (CI)
 *   node scripts/check-no-data.mjs --staged     # what's staged for the next commit (pre-commit hook)
 *   node scripts/check-no-data.mjs --json [dir]
 *
 * Allowed: paths with `synthetic`, `.example.` or a `reference/` folder skip the data-file check (content is still
 * scanned); a line containing `data-scan: allow` is skipped; `.data-scan-allow` lists path globs to skip entirely, one
 * per line, each with a `# reason`. Both allowances show in the PR diff, so a code owner reviews them.
 * Exit 0 clean, 1 findings, 2 usage or git error.
 */
import { execFileSync } from "node:child_process";
import { readFileSync, statSync } from "node:fs";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

export const DATA_FILE = /\.(csv|tsv|xlsx?|xlsm|numbers|parquet|avro|sqlite3?|db|mdb|accdb|dump|bak|vcf|mbox|eml|pst)$|\.(sql|json|jsonl|ndjson)\.(gz|zip)$|(^|\/)(data|exports?|dumps?|backups?)\/.*\.(json|jsonl|ndjson|sql)$/i;
export const DATA_ALLOWED = /synthetic|\.example\.|(^|\/)reference\//i;

const SKIP_PATH = /(^|\/)(node_modules|vendor|\.venv|venv|dist|build|out|\.next|coverage|\.git)\//;
const LOCKFILE = /(^|\/)(package-lock\.json|npm-shrinkwrap\.json|pnpm-lock\.yaml|yarn\.lock|bun\.lockb?|poetry\.lock|Pipfile\.lock|uv\.lock|Cargo\.lock|go\.sum|composer\.lock|Gemfile\.lock)$/;
const BINARY = /\.(png|jpe?g|gif|webp|ico|bmp|tiff?|svg|pdf|zip|gz|tgz|bz2|7z|rar|woff2?|ttf|otf|eot|mp[34]|mov|wav|m4a|webm|avif|heic|psd|sketch|fig|wasm|so|dylib|dll|exe|bin|jar|class|pyc|lockb|xlsx|xls|xlsm|ods|numbers|parquet|feather|avro|sqlite3?|db|pst)$/i;
const MAX_BYTES = 20 * 1024 * 1024;
const ALLOW_LINE = /data-scan:\s*allow/;

// ---------------------------------------------------------------------------------------------------------------
// Patterns. Each returns the 0-based character offsets of real-looking matches in one line.

const EMAIL = /[A-Za-z0-9._%+-]+@((?:[A-Za-z0-9-]+\.)+([A-Za-z]{2,}))\b/g;
/** Role and machine mailboxes aren't people. */
const ROLE_LOCAL = /^(no-?reply|do-?not-?reply|donotreply|support|info|hello|help|admin|security|team|sales|contact|privacy|billing|notifications?|postmaster|abuse|webmaster|mailer-daemon|git|bot|dependabot(\[bot\])?|github-actions(\[bot\])?)$/i;
/**
 * Reserved, machine and SchoolAI staff domains. Staff work addresses appear in config (owners, allowlists) and are
 * governed by the registry, not this rule; customer and personal addresses are what this catches.
 */
const NOT_PERSON_DOMAIN = /(^|\.)(example\.(com|org|net)|example|test|invalid|localhost|local|internal|schoolai\.com|users\.noreply\.github\.com|noreply\.github\.com|sentry\.io|sentry-next\.wixpress\.com)$/i;
/** Obviously invented domains in fixtures (acme.com, district.org) aren't evidence of real people. */
const SYNTHETIC_LABEL = /^([a-z]|acme|test|testing|district|school|company|domain|email|mail|corp|customer|user|users|foo|bar|baz|sample|fake|demo|mock|dummy|placeholder|someone|somewhere|anon|generated|synthetic)$/i;
const FILE_TLD = /^(png|jpe?g|gif|svg|webp|js|mjs|cjs|ts|tsx|jsx|css|scss|json|md|html|py|vue|ya?ml|txt|map)$/i;

const PHONE = /(?<![\d.\w/#-])(?:\+?1[-.\s]?)?\(?([2-9]\d{2})\)?[-.\s]([2-9]\d{2})[-.\s](\d{4})(?![\d-]|\.\d)/g;

const STREET = "Street|St|Avenue|Ave|Road|Rd|Boulevard|Blvd|Drive|Dr|Lane|Ln|Way|Court|Ct|Place|Pl|Parkway|Pkwy|Circle|Cir|Highway|Hwy|Terrace|Ter|Trail|Trl";
const ADDRESS = new RegExp(`\\b\\d{1,6}[ \\t]+(?:[NSEW]\\.?[ \\t]+)?(?:[A-Z][A-Za-z'-]+\\.?[ \\t]+){1,4}(?:${STREET})\\b|\\bP\\.?[ \\t]?O\\.?[ \\t]+Box[ \\t]+\\d+`, "g");
const ADDRESS_FAKE = /\b(Sample|Example|Fake|Test|Placeholder|Synthetic|Demo|Fictional)\b/;

const ROUTING_CONTEXT = /\b(routing|aba|rtn|ach)\b/i;
const NINE = /(?<![\d.])\d{9}(?![\d.])/g;
const BANK_ACCOUNT = /\b(?:bank[ _-]?)?(?:account|acct)[ _-]?(?:number|no|num|#)["']?\s*[:=,]\s*["']?\d{6,17}\b/gi;

/** Credential prefixes. The same set the fleet audit uses for the providers SchoolAI apps call. */
export const TOKENS = [
  ["hubspot-token", /\bpat-(?:na|eu)\d-[0-9a-f-]{30,}/g],
  ["anthropic-key", /\bsk-ant-[A-Za-z0-9_-]{20,}/g],
  ["openai-key", /\bsk-(?!ant-)(?:proj-|svcacct-|admin-)?[A-Za-z0-9_-]{20,}/g],
  ["slack-token", /\bxox[abposr]-[A-Za-z0-9-]{10,}/g],
  ["github-token", /\b(?:gh[pousr]_[A-Za-z0-9]{30,}|github_pat_[A-Za-z0-9_]{30,})/g],
  ["aws-access-key", /\b(?:AKIA|ASIA)[0-9A-Z]{16}\b/g],
  ["notion-token", /\b(?:secret_[A-Za-z0-9]{43}|ntn_[A-Za-z0-9]{40,})\b/g],
  ["1password-token", /\bops_[A-Za-z0-9_-]{40,}/g],
];
const PLACEHOLDER = /x{5,}|X{5,}|\*{4,}|0{12,}|your[_-]|example|placeholder|changeme|dummy|fake|sample|redacted|<[^>]*>|\$\{|\{\{|\.\.\./i;

function abaValid(d) {
  if (/^0+$/.test(d)) return false;
  const n = [...d].map(Number);
  return (3 * (n[0] + n[3] + n[6]) + 7 * (n[1] + n[4] + n[7]) + (n[2] + n[5] + n[8])) % 10 === 0;
}

/** Kinds found on one line, with no values. */
export function scanLine(line) {
  const kinds = [];
  for (const m of line.matchAll(EMAIL)) {
    const [local] = m[0].split("@");
    const domain = m[1].toLowerCase();
    const label = domain.split(".").at(-2) ?? "";
    if (FILE_TLD.test(m[2]) || ROLE_LOCAL.test(local) || NOT_PERSON_DOMAIN.test(domain) || SYNTHETIC_LABEL.test(label)) continue;
    kinds.push("email");
  }
  for (const m of line.matchAll(PHONE)) {
    if (m[2] === "555" || /^(\d)\1{9}$/.test(m[1] + m[2] + m[3])) continue; // 555 exchanges are fictional
    kinds.push("phone");
  }
  for (const m of line.matchAll(ADDRESS)) if (!ADDRESS_FAKE.test(m[0])) kinds.push("street-address");
  if (ROUTING_CONTEXT.test(line)) for (const m of line.matchAll(NINE)) if (abaValid(m[0])) kinds.push("routing-number");
  for (const _ of line.matchAll(BANK_ACCOUNT)) kinds.push("bank-account");
  for (const [id, re] of TOKENS) for (const m of line.matchAll(re)) if (!PLACEHOLDER.test(m[0])) kinds.push(id);
  return kinds;
}

/** Every hit in a text, as { line, kind } (1-based lines). */
export function scanText(text) {
  const hits = [];
  const lines = text.split("\n");
  for (let i = 0; i < lines.length; i++) {
    if (ALLOW_LINE.test(lines[i])) continue;
    for (const kind of new Set(scanLine(lines[i]))) hits.push({ line: i + 1, kind });
  }
  return hits;
}

/** Path globs from .data-scan-allow (`*` within a segment, `**` across segments; a trailing `/` means the folder). */
export function parseAllow(text) {
  return (text ?? "").split("\n").map((l) => l.replace(/#.*/, "").trim()).filter(Boolean).map((g) => {
    const src = g.replace(/\/$/, "/**").replace(/[.+^${}()|[\]\\]/g, "\\$&").replace(/\*\*/g, "\u0000").replace(/\*/g, "[^/]*").replace(/\u0000/g, ".*").replace(/\?/g, "[^/]");
    return new RegExp(`^${src}$`);
  });
}

/**
 * Scan a list of repo-relative files. `read(f)` returns the text or null (missing, too large, binary).
 * Returns { dataFiles: [path], hits: [{ file, line, kind }], scanned, allowed }.
 */
export function scanFiles(files, read, { allow = [] } = {}) {
  const dataFiles = [];
  const hits = [];
  let scanned = 0, allowed = 0;
  for (const f of files) {
    if (SKIP_PATH.test(f) || f === ".data-scan-allow") continue;
    if (allow.some((re) => re.test(f))) { allowed++; continue; }
    if (DATA_FILE.test(f) && !DATA_ALLOWED.test(f)) dataFiles.push(f);
    if (LOCKFILE.test(f) || BINARY.test(f)) continue;
    const text = read(f);
    if (text == null || text.includes("\0")) continue;
    scanned++;
    for (const h of scanText(text)) hits.push({ file: f, ...h });
  }
  return { dataFiles, hits, scanned, allowed };
}

/** Counts by kind, most first. */
export function countKinds(hits) {
  const c = {};
  for (const h of hits) c[h.kind] = (c[h.kind] ?? 0) + 1;
  return Object.fromEntries(Object.entries(c).sort((a, b) => b[1] - a[1]));
}

/** One-line summary with counts only, for check messages and PR bodies. */
export function summary({ dataFiles, hits }) {
  if (!dataFiles.length && !hits.length) return "clean";
  const parts = [];
  if (dataFiles.length) parts.push(`${dataFiles.length} data file(s)`);
  if (hits.length) parts.push(`${hits.length} pattern(s) in ${new Set(hits.map((h) => h.file)).size} file(s): ${Object.entries(countKinds(hits)).map(([k, n]) => `${n} ${k}`).join(", ")}`);
  return parts.join("; ");
}

// ---------------------------------------------------------------------------------------------------------------
// Command line

function gitLines(dir, args) {
  return execFileSync("git", ["-C", dir, ...args], { encoding: "utf8", maxBuffer: 256 << 20 }).split("\0").filter(Boolean);
}

export function scanRepo(dir, { staged = false } = {}) {
  const files = staged ? gitLines(dir, ["diff", "--cached", "--name-only", "-z", "--diff-filter=ACMR"]) : gitLines(dir, ["ls-files", "-z"]);
  const read = staged
    ? (f) => { try { return execFileSync("git", ["-C", dir, "show", `:${f}`], { encoding: "utf8", maxBuffer: MAX_BYTES + 1 }); } catch { return null; } }
    : (f) => { try { return statSync(join(dir, f)).size > MAX_BYTES ? null : readFileSync(join(dir, f), "utf8"); } catch { return null; } };
  let allowText = null;
  try { allowText = staged ? execFileSync("git", ["-C", dir, "show", ":.data-scan-allow"], { encoding: "utf8", stdio: ["ignore", "pipe", "ignore"] }) : readFileSync(join(dir, ".data-scan-allow"), "utf8"); } catch { /* none */ }
  return { files: files.length, ...scanFiles(files, read, { allow: parseAllow(allowText) }) };
}

function main(argv) {
  const staged = argv.includes("--staged");
  const json = argv.includes("--json");
  const dir = resolve(argv.find((a) => !a.startsWith("--")) ?? ".");
  let r;
  try { r = scanRepo(dir, { staged }); } catch (e) { console.error(`check-no-data: ${e.message.split("\n")[0]}`); return 2; }
  if (json) { console.log(JSON.stringify({ ...r, counts: countKinds(r.hits) }, null, 2)); return r.dataFiles.length || r.hits.length ? 1 : 0; }
  const scope = staged ? "staged" : "tracked";
  if (!r.dataFiles.length && !r.hits.length) { console.log(`check-no-data: ${r.scanned} ${scope} file(s) clean`); return 0; }
  console.error(`check-no-data: ${summary(r)} (hard rule 6: no user or customer data or credentials in git)`);
  for (const f of r.dataFiles.slice(0, 50)) console.error(`  ${f} (data file)`);
  for (const h of r.hits.slice(0, 100)) console.error(`  ${h.file}:${h.line} (${h.kind})`);
  if (r.hits.length > 100) console.error(`  …and ${r.hits.length - 100} more (--json lists all)`);
  console.error([
    "Values are never printed. Fix:",
    "  personal data: remove it and read it from its system of record at runtime; tests use generated data in a `synthetic` path.",
    "  a credential: it's leaked once committed. Revoke it at the provider first, then read it from an environment variable.",
    "  not real (a public office number, a placeholder): add `data-scan: allow` on that line, or the path to .data-scan-allow with a # reason.",
    "Guide: SchoolAI/sai-internal-apps skills/handle-data/SKILL.md",
  ].join("\n"));
  return 1;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) process.exit(main(process.argv.slice(2)));
