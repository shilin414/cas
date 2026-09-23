import { readFileSync, writeFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { resolve } from 'node:path';

export function normalizeBasePath(value) {
  if (typeof value !== 'string') throw new Error('basePath must be a string');
  if (value === '' || value === '/') return '/';
  const path = value.replace(/^\//, '').replace(/\/$/, '');
  if (!/^[A-Za-z0-9_-]+(?:\/[A-Za-z0-9_-]+)*$/.test(path)) {
    throw new Error('APP_BASE_PATH/basePath must contain only slash-separated letters, digits, underscores or hyphens');
  }
  return `/${path}/`;
}

export function readDeployment(env = process.env) {
  if (env.APP_BASE_PATH !== undefined) return { basePath: normalizeBasePath(env.APP_BASE_PATH) };
  const file = env.DEPLOYMENT_CONFIG || fileURLToPath(new URL('../../deployment.json', import.meta.url));
  const config = JSON.parse(readFileSync(file, 'utf8'));
  return { basePath: normalizeBasePath(env.APP_BASE_PATH ?? config.basePath) };
}

// Upstream names differ between dev (`api`/`stream` on localhost) and
// Kubernetes (`xiaoan-api`/`xiaoan-stream` in eboat-ai-ns). Fail closed on
// malformed values: this output is included verbatim by Nginx.
function upstreamVariable(env, name, fallback) {
  const value = env[name] || fallback;
  if (!/^[A-Za-z0-9_.-]+:(\d{1,4}|[1-5]\d{4}|6[0-4]\d{3}|65[0-4]\d{2}|655[0-2]\d|6553[0-5])$/.test(value)) {
    throw new Error(`${name} must look like host:port (port 1-65535), got ${JSON.stringify(value)}`);
  }
  return value;
}

export function renderNginx(basePath, env = process.env) {
  const base = normalizeBasePath(basePath);
  const prefix = base.slice(0, -1);
  const apiUpstream = upstreamVariable(env, 'NGINX_API_UPSTREAM', 'api:8080');
  const streamUpstream = upstreamVariable(env, 'NGINX_STREAM_UPSTREAM', 'stream:8081');
  return `# Generated from deployment.json; do not edit this build output.
map $http_upgrade $connection_upgrade {
  default upgrade;
  '' close;
}
server {
  listen 80;
  # Keep canonical redirects relative behind TLS termination / published ports.
  absolute_redirect off;
  server_name _;
  root /usr/share/nginx/html;
  index index.html;
  client_max_body_size 20m;
${prefix ? `  location = ${prefix} { return 308 ${base}$is_args$args; }
  location / { return 404; }` : ''}
  location ${base} {
    try_files $uri $uri/ ${base}index.html;
  }
  location ${base}assets/ { try_files $uri =404; }
  # Public, versioned model assets must never fall back to the SPA HTML.
  location ${base}ocr-assets/ { try_files $uri =404; }
  # A 20 MiB file needs multipart header/boundary headroom. Other APIs retain 20m.
  location ${base}api/v2/admin/ai-models/ {
    client_max_body_size 21m;
    proxy_pass http://${apiUpstream}/api/v2/admin/ai-models/;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header X-Forwarded-Prefix ${base};
  }
  location = ${base}api { return 308 ${base}api/$is_args$args; }
  location ${base}api/ {
    proxy_pass http://${apiUpstream}/api/;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header X-Forwarded-Prefix ${base};
  }
  # Only stream requests go to the gateway; cancel/detail stay on the API pool.
  location ~ ^${base}api/v2/runs/[^/]+/stream$ {
    rewrite ^${base}(.*)$ /$1 break;
    proxy_pass http://${streamUpstream};
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header Connection "";
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header X-Forwarded-Prefix ${base};
    proxy_buffering off;
    proxy_cache off;
    proxy_read_timeout 3600s;
    add_header X-Accel-Buffering no always;
  }
  location ${base}ws {
    proxy_pass http://${apiUpstream}/ws;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection $connection_upgrade;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header X-Forwarded-Prefix ${base};
    proxy_read_timeout 3600s;
  }
}
`;
}

// Called by Docker after the Vite build; both consume the same config and env.
if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const { basePath } = readDeployment();
  writeFileSync('nginx.generated.conf', renderNginx(basePath));
  writeFileSync('deployment.generated.json', JSON.stringify({ basePath }));
}
