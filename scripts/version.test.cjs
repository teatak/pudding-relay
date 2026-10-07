const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { execFileSync, spawnSync } = require('node:child_process');
const { test } = require('node:test');
const source = path.resolve(__dirname, '..');
function fixture(t) {
  const parent = fs.mkdtempSync(path.join(os.tmpdir(), 'pudding-version-test-'));
  t.after(() => fs.rmSync(parent, { recursive: true, force: true }));
  const root = path.join(parent, 'repo'); fs.mkdirSync(path.join(root, 'scripts'), { recursive: true });
  for (const name of ['version-lib.sh', 'version.sh', 'release.sh']) fs.copyFileSync(path.join(source, 'scripts', name), path.join(root, 'scripts', name));
  fs.writeFileSync(path.join(root, 'VERSION'), '0.1.0\n');
  return { parent, root, run: (script, arg, env) => spawnSync('sh', [path.join(root, 'scripts', script), arg], { cwd: root, env: env || process.env, encoding: 'utf8' }) };
}

test('patch/minor/major update the single version file and reject malformed versions without changes', t => {
  const f = fixture(t);
  for (const [kind, expected] of [['patch', '0.1.1'], ['minor', '0.2.0'], ['major', '1.0.0']]) {
    const result = f.run('version.sh', kind); assert.equal(result.status, 0, result.stderr);
    assert.equal(fs.readFileSync(path.join(f.root, 'VERSION'), 'utf8'), `${expected}\n`);
  }
  for (const value of ['1.2', '01.2.3', '1.2.3;touch invalid', 'latest']) {
    fs.writeFileSync(path.join(f.root, 'VERSION'), value);
    assert.notEqual(f.run('version.sh', 'patch').status, 0);
    assert.equal(fs.readFileSync(path.join(f.root, 'VERSION'), 'utf8'), value);
  }
});

test('release publishes current version once, increments patch and keeps source/tag/version consistent', t => {
  const f = fixture(t); const bin = path.join(f.parent, 'bin'); fs.mkdirSync(bin);
  for (const name of ['docker', 'make']) fs.writeFileSync(path.join(bin, name), '#!/bin/sh\nexit 0\n', { mode: 0o755 });
  fs.writeFileSync(path.join(f.root, 'scripts', 'build-image.sh'), '#!/bin/sh\nset -eu\n[ "${FAIL_PUBLISH:-}" != 1 ] || exit 1\ncat "$(dirname "$0")/../VERSION" >> "$RELEASE_LOG"\n', { mode: 0o755 });
  const git = (...args) => execFileSync('git', args, { cwd: f.root, encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] }).trim();
  git('init', '-b', 'main'); git('config', 'user.email', 'fixture@example.com'); git('config', 'user.name', 'Fixture');
  git('add', '.'); git('commit', '-m', 'initial');
  const origin = path.join(f.parent, 'origin.git'); execFileSync('git', ['init', '--bare', origin], { stdio: 'ignore' });
  git('remote', 'add', 'origin', origin); git('push', '-u', 'origin', 'main');
  const env = { ...process.env, PATH: `${bin}${path.delimiter}${process.env.PATH}`, RELEASE_LOG: path.join(f.parent, 'released') };
  let result = f.run('release.sh', 'current', env); assert.equal(result.status, 0, result.stderr);
  assert.equal(git('show', 'v0.1.0:VERSION'), '0.1.0');
  result = f.run('release.sh', 'current', env); assert.notEqual(result.status, 0);
  assert.equal(fs.readFileSync(env.RELEASE_LOG, 'utf8'), '0.1.0\n');
  result = f.run('release.sh', 'patch', env); assert.equal(result.status, 0, result.stderr);
  assert.equal(git('show', 'v0.1.1:VERSION'), '0.1.1');
  assert.equal(git('show', 'origin/main:VERSION'), '0.1.1');
  result = f.run('release.sh', 'patch', { ...env, FAIL_PUBLISH: '1' }); assert.notEqual(result.status, 0);
  assert.equal(git('tag', '--list', 'v0.1.2'), '');
  assert.equal(git('show', 'origin/main:VERSION'), '0.1.1');
  result = f.run('release.sh', 'patch', env); assert.equal(result.status, 0, result.stderr);
  assert.equal(git('show', 'v0.1.2:VERSION'), '0.1.2');
  assert.equal(fs.readFileSync(env.RELEASE_LOG, 'utf8'), '0.1.0\n0.1.1\n0.1.2\n');
  fs.writeFileSync(path.join(f.root, 'uncommitted'), 'source change');
  result = f.run('release.sh', 'patch', env); assert.notEqual(result.status, 0);
  assert.equal(fs.readFileSync(path.join(f.root, 'VERSION'), 'utf8'), '0.1.2\n');
});
