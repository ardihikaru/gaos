#!/usr/bin/env node
/**
 * Build deploy/seed/hisv3/product.json and cases.json from a checkout of automation-test-hisv3.
 *
 *   node deploy/seed/hisv3/build-seed.mjs <automation-test-hisv3 checkout> [<qa-automation-tools checkout>] [<out dir>]
 *
 * Node >= 22 (the source repo's own toolchain). Deterministic and read-only on both checkouts: the same
 * commits give the same bytes, so `node build-seed.mjs <src> <qa> /tmp/out && diff -r /tmp/out deploy/seed/hisv3`
 * is the check that the committed files were generated from the commits they name (design §10, P3).
 *
 * The parsing rules are those of the source repo's scripts/test-catalog.mjs: STORY_LINE, FEATURE_ID, the
 * 10-column case row and the 12-column feature row, so a case round-trips unchanged.
 *
 * SANITISING. Story, feature and case text reaches every agent turn (through 20-stories.md), so each field is
 * made one plain line: backticks become ', angle brackets and line breaks go, a leading # is stripped. The
 * SERVER repeats this and screens for secrets (dashboard/seed.go); this is a convenience, not the control.
 * The script FAILS, writing nothing, if any text matches a secret pattern.
 */
import fs from 'node:fs';
import path from 'node:path';
import { execFileSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const [srcArg, qaArg, outArg] = process.argv.slice(2);
if (!srcArg) {
  console.error('usage: build-seed.mjs <automation-test-hisv3 checkout> [<qa-automation-tools checkout>] [<out dir>]');
  process.exit(2);
}
const root = path.resolve(srcArg);
const outDir = path.resolve(outArg ?? here);
const catDir = path.join(root, 'docs/test-catalog');
const modDir = path.join(root, 'docs/modules');

const STORY_LINE = /^- \*\*(US-[A-Z]+-\d{2})\*\*(.*?)\s+—\s+(.*)$/;
const FEATURE_ID = /[A-D]\d{2}\.\d{2,3}(?:\.\d{2})?/g;
const STATUSES = ['planned', 'automated', 'blocked', 'manual-only', 'wont'];
const MAX_SCENARIO = 600;
const MAX_SPEC = 240;

// The same families as dashboard/egress.go's detector table.
const SECRETS = [
  /-----BEGIN (?:RSA |EC |DSA |OPENSSH |PGP )?PRIVATE KEY-----/,
  /\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b/,
  /^[ \t]*(?:export[ \t]+)?[A-Z0-9_]*(?:PASSWORD|PASSWD|SECRET|TOKEN|API_?KEY|PRIVATE_KEY|ACCESS_KEY)[A-Z0-9_]*[ \t]*[:=][ \t]*\S{6,}/im,
  /\bgaos_[0-9A-Za-z]{32,}\b/,
  /\bsk-ant-[A-Za-z0-9_-]{16,}/,
  /\bsk-(?:proj-)?[A-Za-z0-9]{20,}/,
  /\b(?:AKIA|ASIA|AGPA|AIDA)[0-9A-Z]{16}\b/,
  /\bgh[pousr]_[A-Za-z0-9]{36,}\b/,
  /\bAIza[0-9A-Za-z_-]{35}\b/,
  /\bxox[abprs]-[0-9A-Za-z-]{10,}\b/,
  /\b[a-z][a-z0-9+.-]*:\/\/[^\s/:@]+:[^\s/@]{3,}@/,
];

const clean = (s) =>
  s
    .replace(/`/g, "'")
    .replace(/[<>]/g, '')
    .replace(/\s+/g, ' ')
    .trim()
    .replace(/^[# ]+/, '');
const cut = (s, n) => {
  const b = Buffer.from(s, 'utf8');
  if (b.length <= n) return s;
  return b.subarray(0, n - 3).toString('utf8').replace(/�+$/, '') + '...';
};
const cells = (line) =>
  line
    .split('|')
    .slice(1, -1)
    .map((c) => c.trim());
const git = (dir, ...args) => {
  try {
    return execFileSync('git', ['-C', dir, ...args], { encoding: 'utf8' }).trim();
  } catch {
    return '';
  }
};

// ---- modules and features
const modules = [];
const featureModule = new Map();
for (const file of fs.readdirSync(modDir).filter((f) => /^[A-D]-.+\.md$/.test(f)).sort()) {
  let mod = null;
  for (const line of fs.readFileSync(path.join(modDir, file), 'utf8').split('\n')) {
    const h = line.match(/^### ([A-D]\d{2}) — (.+)$/);
    if (h) {
      mod = { key: h[1], title: clean(h[2]), features: [], statuses: new Set() };
      modules.push(mod);
      continue;
    }
    if (!line.startsWith('|') || !mod) continue;
    const c = cells(line);
    const idm = c[0]?.match(/^(?:<a id="[^"]*"><\/a>)?([A-D]\d{2}\.\d{2,3}(?:\.\d{2})?)$/);
    if (!idm || c.length !== 12) continue;
    const status = c[4].match(/`([A-Z-]+)`/)?.[1] ?? 'UNKNOWN';
    mod.statuses.add(status);
    mod.features.push(cut(`${idm[1]} · ${clean(c[1])} · ${clean(c[2])} [${status}]`, 300));
    featureModule.set(idm[1], mod.key);
  }
}

// ---- epics, stories, cases
const stories = new Map();
const epics = new Map();
const cases = [];
const files = fs
  .readdirSync(catDir, { recursive: true })
  .map((f) => f.split(path.sep).join('/'))
  .filter((f) => f.endsWith('.md') && f !== 'README.md' && !path.basename(f).startsWith('_') && f.includes('/'))
  .sort();

for (const file of files) {
  const lines = fs.readFileSync(path.join(catDir, file), 'utf8').split('\n');
  const title = clean((lines.find((l) => l.startsWith('# ')) ?? file).replace(/^#\s+/, ''));
  let code = null;
  const votes = new Map();
  for (const line of lines) {
    const sm = line.match(STORY_LINE);
    if (!sm) continue;
    const [, id, mid] = sm;
    const features = /\(unmapped:/.test(mid) ? [] : [...new Set(mid.match(FEATURE_ID) ?? [])];
    stories.set(id, { id, features });
    code ??= id.split('-')[1];
    for (const f of features) {
      const m = featureModule.get(f);
      if (m) votes.set(m, (votes.get(m) ?? 0) + 1);
    }
  }
  if (!code) continue;
  const module = [...votes.entries()].sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))[0]?.[0] ?? '';
  epics.set(code, { code, title: cut(title, 200), service: file.split('/')[0], source: `docs/test-catalog/${file}`, module });

  for (const line of lines) {
    if (!/^\|\s*[☐☑]\s*\|/.test(line)) continue;
    const c = cells(line);
    if (c.length !== 10) continue;
    const [, id, story, scenario, polarity, level, pri, status, spec, reason] = c;
    if (!STATUSES.includes(status)) continue;
    const features = stories.get(story)?.features ?? [];
    const implemented = status === 'automated';
    const specPath = implemented ? ([...spec.matchAll(/`([^`]+)`/g)].map((m) => m[1])[0] ?? '') : '';
    cases.push({
      caseId: id,
      epic: code,
      module: features.map((f) => featureModule.get(f)).find(Boolean) ?? module,
      features,
      userStory: story,
      scenario: cut(clean(scenario), MAX_SCENARIO),
      polarity,
      level,
      pri,
      decision: status === 'automated' ? 'planned' : status,
      reason: status === 'automated' ? '' : clean(reason),
      implemented,
      spec: cut(specPath, MAX_SPEC),
    });
  }
}
cases.sort((a, b) => a.caseId.localeCompare(b.caseId));

// ---- assemble
const automationSha = git(root, 'rev-parse', 'HEAD');
const qaSha = qaArg ? git(path.resolve(qaArg), 'rev-parse', 'HEAD') : '';
const defaults = JSON.parse(fs.readFileSync(path.join(here, 'defaults.json'), 'utf8'));
for (const s of defaults.sources) {
  if (s.ref === '__AUTOMATION_SHA__') s.ref = automationSha;
  if (s.ref === '__QATOOLS_SHA__') s.ref = qaSha;
  if (!s.ref) throw new Error(`no commit for knowledge source ${s.url}: pass the qa-automation-tools checkout`);
}
const source = { repo: 'developersismedika/automation-test-hisv3', commit: automationSha };
const product = {
  source,
  ...defaults,
  modules: modules.map((m) => ({
    key: m.key,
    title: m.title,
    pri: m.statuses.has('PROD') || m.statuses.has('DEV-DONE') ? 'must' : 'should',
    features: m.features,
  })),
  epics: [...epics.values()].sort((a, b) => a.code.localeCompare(b.code)),
};
const casesDoc = { source, cases };

const texts = JSON.stringify([product, casesDoc]);
for (const re of SECRETS) {
  const m = texts.match(re);
  if (m) {
    console.error(`refusing to write: the text matches a secret pattern (${re.source.slice(0, 40)}...)`);
    process.exit(1);
  }
}
fs.mkdirSync(outDir, { recursive: true });
fs.writeFileSync(path.join(outDir, 'product.json'), JSON.stringify(product, null, 1) + '\n');
fs.writeFileSync(path.join(outDir, 'cases.json'), JSON.stringify(casesDoc) + '\n');
const n = (k) => cases.filter((c) => c.decision === k).length;
console.error(
  `modules ${modules.length}, features ${modules.reduce((a, m) => a + m.features.length, 0)}, epics ${epics.size}, ` +
    `cases ${cases.length} (planned ${n('planned')}, blocked ${n('blocked')}, manual-only ${n('manual-only')}, wont ${n('wont')}), ` +
    `implemented ${cases.filter((c) => c.implemented).length}, commit ${automationSha.slice(0, 7)}`,
);
