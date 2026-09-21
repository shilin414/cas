import { test } from 'node:test';
import assert from 'node:assert/strict';
import { normalizeBasePath, readDeployment, renderNginx } from './deployment.mjs';

for (const [input, expected] of [['xiaoan-platform', '/xiaoan-platform/'], ['/other/nested/', '/other/nested/'], ['/', '/'], ['', '/']]) {
  test(`normalize ${JSON.stringify(input)}`, () => assert.equal(normalizeBasePath(input), expected));
}
for (const value of ['//evil', '/a/../b', '/a?x', '/a#x', '/a%2fb', '/a b', '/a;foo', null]) {
  test(`reject unsafe ${JSON.stringify(value)}`, () => assert.throws(() => normalizeBasePath(value)));
}
for (const base of ['/xiaoan-platform/', '/other/nested/', '/']) {
  test(`nginx matches the build base ${base}`, () => {
    const config = renderNginx(base);
    assert.ok(config.includes('absolute_redirect off;'), 'canonical redirects must preserve external scheme and published port');
    assert.ok(config.includes(`location ${base}api/`));
    assert.ok(config.includes(`try_files $uri $uri/ ${base}index.html`));
    assert.ok(config.includes('proxy_pass http://api:8080/api/;'));
    assert.ok(config.includes(`location ~ ^${base}api/v2/runs/[^/]+/stream$`));
    assert.ok(config.includes('proxy_buffering off;'));
    assert.ok(config.includes('proxy_set_header Upgrade $http_upgrade;'));
    assert.ok(config.includes(`location ${base}assets/ { try_files $uri =404; }`));
    assert.equal((config.match(/location \/ \{/g) || []).length, 1);
  });
}
test('shared file can be overridden explicitly', () => {
  assert.equal(readDeployment({APP_BASE_PATH: '/another/', DEPLOYMENT_CONFIG: '/missing/deployment.json'}).basePath, '/another/');
});
for (const base of ['/xiaoan-platform/', '/other/nested/', '/']) {
  test(`OCR resources fail closed and model multipart has headroom at ${base}`, () => {
    const config = renderNginx(base);
    assert.ok(config.includes(`location ${base}ocr-assets/ { try_files $uri =404; }`));
    assert.ok(config.includes(`location ${base}api/v2/admin/ai-models/ {\n    client_max_body_size 21m;`));
    assert.ok(config.includes('proxy_pass http://api:8080/api/v2/admin/ai-models/;'));
    assert.ok(config.includes('client_max_body_size 20m;'), 'unrelated APIs keep their existing limit');
  });
}
