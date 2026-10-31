FROM node:22-alpine AS builder
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
# Vite inlines VITE_* values at build time, so they must be build arguments.
ARG VITE_API_URL=http://localhost:8000/api/v1
ARG VITE_AXYRA_URL=http://localhost:5173
ENV VITE_API_URL=$VITE_API_URL \
    VITE_AXYRA_URL=$VITE_AXYRA_URL
RUN npm run build

FROM nginx:alpine
COPY --from=builder /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
