# syntax=docker/dockerfile:1
# ---- Stage 1: build the Vue frontend ----
FROM node:20-alpine AS frontend
WORKDIR /app/web
COPY web/package.json web/package-lock.json* ./
RUN npm install
COPY web/ ./
RUN npm run build
# frontend outputs to ../webstatic/dist (embedded by Go via //go:embed)

# ---- Stage 2: build the Go binary ----
FROM golang:1.26-alpine AS builder
WORKDIR /src
# copy go module files first for layer caching
COPY go.mod go.sum ./
RUN go mod download
# copy everything (frontend dist must exist before go build)
COPY --from=frontend /app/webstatic/dist ./webstatic/dist
COPY . .
RUN CGO_ENABLED=0 go build -o /out/agentworld .

# ---- Stage 3: runtime ----
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /out/agentworld /app/agentworld
# runtime data volume (sqlite db + logs)
VOLUME ["/data"]
ENV DB_DRIVER=sqlite \
    DB_PATH=/data/agentworld.db \
    LOG_DIR=/data/logs \
    PORT=18080
EXPOSE 18080
CMD ["/app/agentworld"]
