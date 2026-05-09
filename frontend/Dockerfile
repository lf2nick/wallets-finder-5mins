# wf5m frontend — multistage: vite build → nginx serve
#
# Stage 1: build React/Vite to dist/
# Stage 2: nginx serve dist/ + reverse proxy /api/* to wf5m-backend:8080

FROM node:20-alpine AS builder
WORKDIR /app
COPY package.json ./
RUN npm install
COPY . .
RUN npm run build

FROM nginx:1.27-alpine
COPY --from=builder /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/nginx.conf
EXPOSE 80
