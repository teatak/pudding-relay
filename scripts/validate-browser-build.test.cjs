const assert = require('node:assert/strict');
const { createHash } = require('node:crypto');
const { spawnSync } = require('node:child_process');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { test } = require('node:test');

function fixture(t) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'pudding-browser-build-test-'));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  fs.mkdirSync(path.join(root, 'assets'));
  const entry = 'assets/remote-current.js';
  const bundle = 'const message = "remote_protocol_mismatch";';
  fs.writeFileSync(path.join(root, entry), bundle);
  fs.writeFileSync(path.join(root, 'index.html'), `<script type="module" src="./${entry}"></script>`);
  const manifest = { formatVersion: 1, browserApiProtocol: 3, coreApiProtocol: 16, appVersion: '0.4.1', desktopCommit: 'a'.repeat(40), entry: { path: entry, sha256: createHash('sha256').update(bundle).digest('hex') } };
  const save = () => fs.writeFileSync(path.join(root, 'browser-build.json'), JSON.stringify(manifest));
  save();
  return { root, entry, manifest, save, run: () => spawnSync(process.execPath, [path.join(__dirname, 'validate-browser-build.cjs'), root], { encoding: 'utf8' }) };
}

test('image build accepts the exact compiled browser entry and its manifest', t => {
  const f = fixture(t), result = f.run();
  assert.equal(result.status, 0, result.stderr);
  assert.match(result.stdout, /Browser API 3, core API 16/);
});

test('image build rejects stale entries, modified JavaScript and invalid API metadata', t => {
  const f = fixture(t);
  f.manifest.entry.path = 'assets/remote-previous.js'; f.save();
  assert.notEqual(f.run().status, 0);
  f.manifest.entry.path = f.entry; f.save();
  fs.appendFileSync(path.join(f.root, f.entry), '\n// modified after manifest generation');
  assert.notEqual(f.run().status, 0);
  f.manifest.browserApiProtocol = 0; f.save();
  assert.notEqual(f.run().status, 0);
});

test('image build requires the manifest and a Remote browser entry', t => {
  const f = fixture(t);
  fs.unlinkSync(path.join(f.root, 'browser-build.json'));
  assert.notEqual(f.run().status, 0);
  f.save(); fs.writeFileSync(path.join(f.root, f.entry), 'ordinary desktop entry');
  assert.notEqual(f.run().status, 0);
});
