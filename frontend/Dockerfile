# Build from repository root: docker build -f frontend/Dockerfile .
FROM node:24-alpine AS build
WORKDIR /app/frontend
COPY deployment.json /app/deployment.json
COPY frontend/package*.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build
# Put static files at their public path. Root deployment works without special cases.
RUN node --input-type=module -e "import {readFileSync,mkdirSync,cpSync} from 'node:fs'; const {basePath}=JSON.parse(readFileSync('deployment.generated.json','utf8')); const dest='/site'+basePath; mkdirSync(dest,{recursive:true}); cpSync('dist',dest,{recursive:true});"

FROM nginx:1.27-alpine
COPY --from=build /site/ /usr/share/nginx/html/
COPY --from=build /app/frontend/nginx.generated.conf /etc/nginx/conf.d/default.conf
