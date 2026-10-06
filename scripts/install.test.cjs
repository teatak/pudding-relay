const assert = require('node:assert/strict');
const { spawnSync } = require('node:child_process');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { test } = require('node:test');
const installer = path.resolve(__dirname, '../install.sh');

function fixture(t) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'pudding-install-test-'));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  const bin = path.join(root, 'bin'); fs.mkdirSync(bin);
  fs.writeFileSync(path.join(bin, 'docker'), `#!/usr/bin/env node
const fs=require('node:fs');const args=process.argv.slice(2);
fs.appendFileSync(process.env.DOCKER_CALLS,JSON.stringify(args)+'\\n');
if(args[0]==='pull'&&process.env.FAIL_PULL==='1')process.exit(1);
if(args[0]==='run'&&process.env.FAIL_ASSETS==='1')process.exit(1);
if(args[0]==='network'&&process.env.FAIL_NETWORK==='1')process.exit(1);
`, { mode: 0o755 });
  const directory = path.join(root, 'installed relay');
  const env = { ...process.env, PATH: `${bin}${path.delimiter}${process.env.PATH}`, INSTALL_DIR: directory, PUBLIC_URL: 'https://relay.example.com', DOCKER_CALLS: path.join(root, 'calls') };
  for (const key of ['IMAGE', 'PORT', 'BIND_ADDRESS', 'NETWORK', 'FAIL_PULL', 'FAIL_ASSETS', 'FAIL_NETWORK']) delete env[key];
  const run = (overrides = {}) => spawnSync('sh', [installer], { env: { ...env, ...overrides }, encoding: 'utf8' });
  return { root, directory, env, run, calls: () => fs.readFileSync(env.DOCKER_CALLS, 'utf8').trim().split('\n').map(line => JSON.parse(line)) };
}

test('install/reinstall preserves admin credentials and configuration, treating .env as data', t => {
  const f = fixture(t);
  let result = f.run({ PORT: '19080', NETWORK: 'existing-proxy' }); assert.equal(result.status, 0, result.stderr);
  const secretPath = path.join(f.directory, 'secrets/admin-secret');
  const secret = fs.readFileSync(secretPath, 'utf8'); assert.match(secret, /^[a-f0-9]{64}\n$/);
  assert.equal(result.stdout.includes(secret.trim()), false);
  assert.equal(fs.statSync(path.dirname(secretPath)).mode & 0o777, 0o700);
  assert.equal(fs.statSync(secretPath).mode & 0o777, 0o444);
  const configPath = path.join(f.directory, '.env');
  fs.appendFileSync(configPath, `UNRELATED=$(touch ${path.join(f.root,'unexpected')})\n# keep comment\n`);
  const rerunEnv = { ...f.env }; delete rerunEnv.PUBLIC_URL;
  result = spawnSync('sh', [installer], { env: rerunEnv, encoding: 'utf8' }); assert.equal(result.status, 0, result.stderr);
  assert.equal(fs.readFileSync(secretPath, 'utf8'), secret);
  assert.equal(fs.existsSync(path.join(f.root, 'unexpected')), false);
  const config = fs.readFileSync(configPath, 'utf8');
  assert.match(config, /PORT=19080\n/); assert.match(config, /NETWORK=existing-proxy\n/); assert.match(config, /# keep comment/); assert.match(config, /UNRELATED=/);
  assert.equal(fs.statSync(configPath).mode & 0o777, 0o600);
  assert.match(fs.readFileSync(path.join(f.directory, 'compose.yaml'), 'utf8'), /external: true/);
  result = f.run({ PORT: '19081', NETWORK: '' }); assert.equal(result.status, 0, result.stderr);
  assert.match(fs.readFileSync(configPath, 'utf8'), /PORT=19081\n/);
  assert.match(fs.readFileSync(configPath, 'utf8'), /NETWORK=\n/);
  assert.doesNotMatch(fs.readFileSync(path.join(f.directory, 'compose.yaml'), 'utf8'), /external: true/);
  assert.equal(fs.readFileSync(secretPath, 'utf8'), secret);
});

test('failed pulls and missing browser resources do not replace a working installation', t => {
  const f = fixture(t); const result = f.run(); assert.equal(result.status, 0, result.stderr);
  const before = Object.fromEntries(['.env', 'compose.yaml', 'makefile', 'secrets/admin-secret'].map(file => [file, fs.readFileSync(path.join(f.directory, file))]));
  for (const failure of [{ FAIL_PULL: '1' }, { FAIL_ASSETS: '1' }]) {
    const result = f.run({ ...failure, PUBLIC_URL: 'https://other.example.com', PORT: '19081' }); assert.notEqual(result.status, 0);
    for (const [file, bytes] of Object.entries(before)) assert.deepEqual(fs.readFileSync(path.join(f.directory, file)), bytes);
  }
});

test('noninteractive installs require a valid HTTPS origin and valid configuration before pulling', t => {
  const f = fixture(t);
  for (const override of [{ PUBLIC_URL: '' }, { PUBLIC_URL: 'http://relay.example.com' }, { PUBLIC_URL: 'https://example.com/path' }, { PUBLIC_URL: 'https://fixture-user:fixture-secret@example.com' }, { PUBLIC_URL: 'https://example.com?x=1' }, { PORT: '0' }, { PORT: '65536' }, { NETWORK: 'missing', FAIL_NETWORK: '1' }, { IMAGE: '-bad' }]) {
    const result = f.run(override); assert.notEqual(result.status, 0, JSON.stringify(override));
    assert.equal(fs.existsSync(path.join(f.directory, '.env')), false);
    assert.equal(fs.existsSync(path.join(f.directory, 'secrets/admin-secret')), false);
  }
  assert.ok(f.calls().every(args => args[0] !== 'pull'));
});
