// Real Docker installation using a disposable loopback registry, directory and volume.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const http = require('node:http');
const { execFile } = require('node:child_process');
const { promisify } = require('node:util');
const run = promisify(execFile);
const installer = path.resolve(__dirname, '../install.sh');
const image = process.env.PUDDING_RELAY_TEST_IMAGE;
if (!image) throw new Error('Set PUDDING_RELAY_TEST_IMAGE to the built distribution image.');
const root = fs.mkdtempSync(path.join(os.tmpdir(), 'pudding-relay-install-smoke-'));
const registry = `${path.basename(root)}-registry`;
let installed = false;
const checks = [];
async function docker(...args) { return (await run('docker', args, { maxBuffer: 10 * 1024 * 1024 })).stdout; }
async function port() {
  const reserve = http.createServer();
  await new Promise(resolve => reserve.listen(0, '127.0.0.1', resolve));
  const selected = reserve.address().port;
  await new Promise(resolve => reserve.close(resolve));
  return selected;
}
async function waitFor(predicate, label) {
  const until = Date.now() + 30000;
  while (Date.now() < until) {
    if (await predicate()) return;
    await new Promise(resolve => setTimeout(resolve, 100));
  }
  throw new Error(`Timed out: ${label}`);
}
async function main() {
  const registryPort = await port(); const relayPort = await port();
  await docker('run', '-d', '--name', registry, '-p', `127.0.0.1:${registryPort}:5000`, 'registry:3');
  await waitFor(async () => { try { return (await fetch(`http://127.0.0.1:${registryPort}/v2/`)).ok; } catch { return false; } }, 'local registry');
  const reference = `localhost:${registryPort}/pudding-relay:smoke`;
  await docker('tag', image, reference);
  await docker('push', reference);
  const env = { ...process.env, INSTALL_DIR: root, TRUSTED_PROXIES: '', PORT: String(relayPort), IMAGE: reference, BIND_ADDRESS: '127.0.0.1', NETWORK: '' };
  const invoke = async overrides => run('sh', [installer], { env: { ...env, ...overrides }, maxBuffer: 10 * 1024 * 1024 });
  delete env.PUBLIC_URL;
  installed = true;
  const first = await invoke({});
  const secret = fs.readFileSync(path.join(root, 'secrets/admin-secret'), 'utf8');
  assert.equal(first.stdout.includes(secret.trim()), false);
  const endpoint = `http://127.0.0.1:${relayPort}`;
  const headers = { Authorization: `Bearer ${secret.trim()}`, Origin: endpoint, 'Content-Type': 'application/json' };
  assert.equal((await fetch(`${endpoint}/healthz`)).status, 200);
  const expectedVersion = fs.readFileSync(path.resolve(__dirname, '../VERSION'), 'utf8').trim();
  assert.equal((await (await fetch(`${endpoint}/version`)).json()).version, expectedVersion);
  assert.ok((await docker('run', '--rm', image, '--version')).startsWith(`pudding-relay ${expectedVersion} (`));
  assert.match(await (await fetch(`${endpoint}/admin`)).text(), /Pudding/);
  const config = JSON.parse(await docker('compose', '--project-directory', root, '-f', path.join(root, 'compose.yaml'), 'config', '--format', 'json'));
  assert.equal(config.services.relay.read_only, true);
  assert.equal(config.services.relay.ports[0].host_ip, '127.0.0.1');
  checks.push('domain-free HTTP install, CLI/API version, health/admin, bundled UI, loopback binding and non-root image');
  const response = await fetch(`${endpoint}/admin/api/desktops`, { method: 'POST', headers, body: JSON.stringify({ desktopID: 'desktop_install_smoke', label: 'Install smoke' }) });
  assert.equal(response.status, 201); const grant = await response.json();
  assert.ok(grant.token);
  const htmlResponse = await fetch(`${endpoint}/d/desktop_install_smoke/pair`);
  assert.equal(htmlResponse.status, 200);
  const html = await htmlResponse.text();
  assert.match(html, /<base href="\/d\/desktop_install_smoke\/"/);
  assert.doesNotMatch(html, /__PUDDING_REMOTE_BASE__/);
  const scriptPath = html.match(/<script[^>]+src="([^"]+)"/)[1];
  const scriptResponse = await fetch(new URL(scriptPath, `${endpoint}/d/desktop_install_smoke/`));
  assert.equal(scriptResponse.status, 200); assert.ok((await scriptResponse.text()).length > 0);
  assert.equal((await fetch(`${endpoint}/d/desktop_install_smoke/licenses/PUDDING-LICENSE.txt`)).status, 200);
  checks.push('registered offline desktop serves bundled deep links, JavaScript and license');
  const container = (await docker('compose', '--project-directory', root, '-f', path.join(root, 'compose.yaml'), 'ps', '-q', 'relay')).trim();
  const registryBefore = await docker('exec', container, 'cat', '/data/registrations.json');
  assert.equal(registryBefore.includes(grant.token), false);
  const envBefore = fs.readFileSync(path.join(root, '.env'), 'utf8');
  assert.equal(envBefore.includes('PUBLIC_URL='), false);
  fs.appendFileSync(path.join(root, '.env'), 'PUBLIC_URL=https://old.example\n');
  await invoke({});
  assert.equal(fs.readFileSync(path.join(root, 'secrets/admin-secret'), 'utf8'), secret);
  assert.equal(fs.readFileSync(path.join(root, '.env'), 'utf8'), envBefore);
  const getDesktops = async () => (await (await fetch(`${endpoint}/admin/api/desktops`, { headers })).json()).desktops;
  assert.equal((await getDesktops())[0].desktopID, grant.desktopID);
  checks.push('reinstall retains admin secret, configuration and registered desktop');
  await run('make', ['upgrade'], { cwd: root, maxBuffer: 10 * 1024 * 1024 });
  await run('make', ['stop'], { cwd: root, maxBuffer: 10 * 1024 * 1024 });
  await run('make', ['start'], { cwd: root, maxBuffer: 10 * 1024 * 1024 });
  assert.equal((await getDesktops())[0].desktopID, grant.desktopID);
  const finalContainer = (await docker('compose', '--project-directory', root, '-f', path.join(root, 'compose.yaml'), 'ps', '-q', 'relay')).trim();
  assert.equal(await docker('exec', finalContainer, 'cat', '/data/registrations.json'), registryBefore);
  const info = JSON.parse(await docker('inspect', finalContainer))[0];
  assert.equal(info.Config.User, '65532:65532');
  assert.equal(info.HostConfig.ReadonlyRootfs, true);
  assert.deepEqual(info.HostConfig.CapDrop, ['ALL']);
  checks.push('upgrade/stop/start preserve canonical registry and hardened container settings');
  console.log(JSON.stringify({ passed: true, checks, image }, null, 2));
}
main().catch(error => { console.error(error.message); process.exitCode = 1; }).finally(async () => {
  if (installed && fs.existsSync(path.join(root, 'compose.yaml'))) {
    await docker('compose', '--project-directory', root, '-f', path.join(root, 'compose.yaml'), 'down', '--volumes').catch(error => { console.error(error.message); process.exitCode = 1; });
  }
  await docker('rm', '-f', registry).catch(() => {});
  fs.rmSync(root, { recursive: true, force: true });
});
