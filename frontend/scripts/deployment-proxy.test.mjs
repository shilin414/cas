import { test } from 'node:test';
import assert from 'node:assert/strict';
import http from 'node:http';
import { createHash } from 'node:crypto';
import { fileURLToPath } from 'node:url';
import { createServer } from 'vite';

// Real Vite middleware + real HTTP/SSE/WebSocket transport; no business DB or messages.
for (const base of ['/xiaoan-platform/', '/other/nested/', '/']) {
  test(`Vite serves and rewrites ${base}`, { timeout: 20000 }, async () => {
    const requests = [];
    const sockets = new Set();
    const upstream = http.createServer((req, res) => {
      requests.push(req.url);
      if (req.url.includes('/stream')) {
        res.writeHead(200, {'content-type': 'text/event-stream'});
        res.write('event: ready\ndata: {"ok":true}\n\n');
        const timer = setTimeout(() => res.end(), 2000);
        res.on('close', () => clearTimeout(timer));
      } else {
        res.writeHead(200, {'content-type': 'application/json'});
        res.end(JSON.stringify({path: req.url}));
      }
    });
    upstream.on('connection', socket => { sockets.add(socket); socket.on('close', () => sockets.delete(socket)); });
    upstream.on('upgrade', (req, socket) => {
      requests.push(req.url);
      const accept = createHash('sha1').update(req.headers['sec-websocket-key'] + '258EAFA5-E914-47DA-95CA-C5AB0DC85B11').digest('base64');
      socket.write(`HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: ${accept}\r\n\r\n`);
      socket.write(Buffer.from([0x81, 2, 0x6f, 0x6b]));
      socket.on('data', () => socket.end(Buffer.from([0x88, 0])));
      socket.on('error', () => {});
    });
    await new Promise(resolve => upstream.listen(0, '127.0.0.1', resolve));
    const target = `http://127.0.0.1:${upstream.address().port}`;
    const keys = ['APP_BASE_PATH', 'VITE_API_TARGET', 'VITE_WS_TARGET'];
    const original = keys.map(key => process.env[key]);
    process.env.APP_BASE_PATH = base;
    process.env.VITE_API_TARGET = target;
    process.env.VITE_WS_TARGET = target;
    let server;
    try {
      server = await createServer({
        root: fileURLToPath(new URL('../', import.meta.url)),
        configFile: fileURLToPath(new URL('../vite.config.ts', import.meta.url)),
        logLevel: 'silent',
        server: {host: '127.0.0.1', port: 0, watch: null, preTransformRequests: false},
        optimizeDeps: { noDiscovery: true, include: [] },
      });
      await server.listen();
      const origin = `http://127.0.0.1:${server.httpServer.address().port}`;
      for (const page of ['login/admin', 'share/test-token', 'auth/feishu/callback?code=test']) {
        const response = await fetch(`${origin}${base}${page}`, {headers: {accept: 'text/html'}});
        assert.equal(response.status, 200);
        assert.ok((await response.text()).includes(`${base}src/main.tsx`));
      }
      const response = await fetch(`${origin}${base}api/v2/runs/123?mode=test`);
      assert.deepEqual(await response.json(), {path: '/api/v2/runs/123?mode=test'});
      const stream = await fetch(`${origin}${base}api/v2/runs/123/stream`, {signal: AbortSignal.timeout(1500)});
      assert.equal(stream.headers.get('x-accel-buffering'), 'no');
      assert.match(new TextDecoder().decode((await stream.body.getReader().read()).value), /event: ready/);
      const ws = new WebSocket(`${origin.replace('http:', 'ws:')}${base}ws/jobs/test`);
      const message = await new Promise((resolve, reject) => {
        ws.onmessage = event => resolve(event.data);
        ws.onerror = reject;
        setTimeout(() => reject(new Error('WebSocket timeout')), 2000).unref();
      });
      assert.equal(message, 'ok');
      ws.close();
      assert.ok(requests.includes('/ws/jobs/test'));
      assert.ok(requests.includes('/api/v2/runs/123/stream'));
    } finally {
      for (const socket of sockets) socket.destroy();
      await server?.close();
      upstream.closeAllConnections();
      await new Promise(resolve => upstream.close(resolve));
      keys.forEach((key, index) => original[index] === undefined ? delete process.env[key] : process.env[key] = original[index]);
    }
  });
}
