# xiaoan-ui — Nginx + prebuilt dist only. NO Node build stage here.
# Jenkins compiles the frontend with the company Node container (nodedkbuild node22140),
# producing frontend/dist, frontend/nginx.generated.conf and frontend/deployment.generated.json
# (the latter two are emitted by `npm run build` -> scripts/deployment.mjs).
# Build AFTER the Jenkins frontend stage, from the repository root:
#   docker build -f frontend/Dockerfile frontend
# so that dist/ and nginx.generated.conf are in the context.
#
# BASE_PATH must equal the value passed to the build as APP_BASE_PATH
# (and deployment.json's basePath). Keep them in sync — one deployment, one path.

FROM harbor.hengan.com:8086/lib/nginx:latest
ARG BASE_PATH=/xiaoan-platform/

# Static files live under their basePath; Nginx root already serves index.html.
COPY dist/ /usr/share/nginx/html${BASE_PATH}/
# Generated config proxies /xiaoan-platform/api -> xiaoan-api, SSE -> xiaoan-stream,
# /ws -> xiaoan-api (upstreams selected at build time via NGINX_*_UPSTREAM env).
COPY nginx.generated.conf /etc/nginx/conf.d/default.conf
