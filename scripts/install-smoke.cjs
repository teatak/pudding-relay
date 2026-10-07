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
if (!image) throw new Error('Set PUDDING_RELAY_TEST_IMAGE to the built Relay image.');
const root = fs.mkdtempSync(path.join(os.tmpdir(), 'pudding-relay-install-smoke-'));
const registry = `${path.basename(root)}-registry`;
let installed = false;
let desktopSocket;
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
  const env = { ...process.env, PORT: String(relayPort), IMAGE: reference, NETWORK: '' };
  const invoke = async overrides => run('sh', [installer], { cwd: root, env: { ...env, ...overrides }, maxBuffer: 10 * 1024 * 1024 });
  delete env.INSTALL_DIR;
  delete env.BIND_ADDRESS;
  delete env.PUBLIC_URL;
  delete env.TRUSTED_PROXIES;
  installed = true;
  const first = await invoke({});
  assert.equal(fs.existsSync(path.join(root, 'pudding-relay')), false);
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
  assert.equal(config.services.relay.ports[0].host_ip, '0.0.0.0');
  assert.equal(config.services.relay.ports[0].target, 9623);
  assert.equal(config.services.relay.ports[0].published, String(relayPort));
  await docker('run', '--rm', '--entrypoint', 'sh', image, '-c', 'test ! -d /assets');
  checks.push('domain-free HTTP install, CLI/API version, health/admin, no bundled desktop UI and all-interface host publishing');
  const response = await fetch(`${endpoint}/admin/api/desktops`, { method: 'POST', headers, body: JSON.stringify({ desktopID: 'desktop_install_smoke', label: 'Install smoke' }) });
  assert.equal(response.status, 201); const grant = await response.json();
  assert.ok(grant.token);
  for (const route of ['/pair', '/assets/app.js']) {
    const offline = await fetch(`${endpoint}/d/desktop_install_smoke${route}`);
    assert.equal(offline.status, 503);
    assert.deepEqual(await offline.json(), { error: 'desktop offline' });
  }
  const desktopAssets = new Map([
    ['/pair', { type: 'text/html; charset=utf-8', body: '<!doctype html><base href="/d/desktop_install_smoke/"><script src="./assets/app.js"></script><p>Desktop UI v1</p>', cache: 'no-store' }],
    ['/assets/app.js', { type: 'text/javascript; charset=utf-8', body: '// desktop UI v1', cache: 'public, max-age=3600' }],
  ]);
  // This fixture represents the desktop gateway; the built image contains no UI.
  const pendingResponses = new Set();
  desktopSocket = new WebSocket(endpoint.replace('http:', 'ws:') + '/tunnel', 'pudding-relay.v1');
  await new Promise((resolve, reject) => {
    const send = frame => desktopSocket.send(JSON.stringify(frame));
    desktopSocket.addEventListener('error', reject, { once: true });
    desktopSocket.addEventListener('open', () => send({ type: 'hello', protocol: 1, desktopID: grant.desktopID, token: grant.token }));
    desktopSocket.addEventListener('message', event => {
      try {
        const frame = JSON.parse(event.data);
        if (frame.type === 'hello') {
          assert.equal(frame.desktopID, grant.desktopID);
          resolve();
        } else if (frame.type === 'request') {
          const asset = desktopAssets.get(frame.path);
          assert.ok(asset, `unexpected desktop request: ${frame.path}`);
          send({ type: 'response', id: frame.id, status: 200, headers: { 'Content-Type': asset.type, 'Cache-Control': asset.cache } });
          if (frame.method === 'HEAD') send({ type: 'response_end', id: frame.id });
          else {
            pendingResponses.add(frame.id);
            send({ type: 'response_data', id: frame.id, data: Buffer.from(asset.body).toString('base64') });
          }
        } else if (frame.type === 'ack' && frame.direction === 'response') {
          assert.ok(pendingResponses.delete(frame.id));
          send({ type: 'response_end', id: frame.id });
        } else if (frame.type === 'cancel') pendingResponses.delete(frame.id);
      } catch (error) { reject(error); desktopSocket.close(); }
    });
  });
  for (const build of [1, 2]) {
    if (build === 2) {
      desktopAssets.get('/pair').body = desktopAssets.get('/pair').body.replace('v1', 'v2');
      desktopAssets.get('/assets/app.js').body = '// desktop UI v2';
    }
    for (const [route, asset] of desktopAssets) {
      const response = await fetch(`${endpoint}/d/desktop_install_smoke${route}`, { signal: AbortSignal.timeout(10000) });
      assert.equal(response.status, 200);
      assert.equal(response.headers.get('content-type'), asset.type);
      assert.equal(response.headers.get('cache-control'), asset.cache);
      assert.equal(await response.text(), asset.body);
    }
  }
  const head = await fetch(`${endpoint}/d/desktop_install_smoke/assets/app.js`, { method: 'HEAD', signal: AbortSignal.timeout(10000) });
  assert.equal(head.status, 200);
  assert.equal(await head.text(), '');
  const socketClosed = new Promise(resolve => desktopSocket.addEventListener('close', resolve, { once: true }));
  desktopSocket.close();
  await socketClosed;
  desktopSocket = undefined;
  checks.push('desktop-owned pages/assets/HEAD pass through the tunnel; desktop UI updates require no relay restart');
  const container = (await docker('compose', '--project-directory', root, '-f', path.join(root, 'compose.yaml'), 'ps', '-q', 'relay')).trim();
  const registryBefore = await docker('exec', container, 'cat', '/data/registrations.json');
  assert.equal(registryBefore.includes(grant.token), false);
  const envBefore = fs.readFileSync(path.join(root, '.env'), 'utf8');
  assert.equal(envBefore.includes('PUBLIC_URL='), false);
  assert.equal(envBefore.includes('TRUSTED_PROXIES='), false);
  assert.equal(JSON.stringify(config).includes('TRUSTED_PROXIES'), false);
  fs.appendFileSync(path.join(root, '.env'), 'PUBLIC_URL=https://old.example\nTRUSTED_PROXIES=172.21.0.0/16\n');
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
  desktopSocket?.close();
  if (installed && fs.existsSync(path.join(root, 'compose.yaml'))) {
    await docker('compose', '--project-directory', root, '-f', path.join(root, 'compose.yaml'), 'down', '--volumes').catch(error => { console.error(error.message); process.exitCode = 1; });
  }
  await docker('rm', '-f', registry).catch(() => {});
  fs.rmSync(root, { recursive: true, force: true });
});
