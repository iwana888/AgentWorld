# syntax=docker/dockerfile:1
# GooseGame single-file deployment image.
#
# Build (run from the repo root):
#   docker build -t agentworld-goose -f worlds/goosegame/Dockerfile .
# Run:
#   docker run -p 19090:19090 -v goose-data:/data agentworld-goose
# Open: http://localhost:19090  (frontend + API same origin, single binary)
#
# ---- Stage 1: build the Vue frontend ----
FROM node:20-alpine AS frontend
WORKDIR /app/worlds/goosegame/web
COPY worlds/goosegame/web/package.json worlds/goosegame/web/package-lock.json* ./
RUN npm install
COPY worlds/goosegame/web/ ./
# frontend outputs to ../webstatic/dist (embedded by Go via //go:embed)
RUN npm run build

# ---- Stage 2: build the Go binary ----
FROM golang:1.26-alpine AS builder
WORKDIR /src
# copy go module files first for layer caching
COPY go.mod go.sum ./
RUN go mod download
# copy everything (frontend dist must exist before go build)
COPY . .
RUN CGO_ENABLED=0 go build -o /out/goose ./worlds/goosegame/cmd/goose

# ---- Stage 3: runtime ----
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /out/goose /app/goose
# runtime data volume (sqlite db)
VOLUME ["/data"]
ENV GOOSE_DB=/data/goosegame.db \
    GOOSE_OBS_ADDR=:19090 \
    GOOSE_INTERVAL=5s
EXPOSE 19090
CMD ["/app/goose"]
