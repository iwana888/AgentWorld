# syntax=docker/dockerfile:1
# Economy World 单文件部署镜像。
#
# 构建（在项目根目录执行）：
#   docker build -t agentworld-economy -f worlds/economy/Dockerfile .
# 运行：
#   docker run -p 19100:19100 -v economy-data:/data agentworld-economy
# 访问：http://localhost:19100 （前端 + API 同源，单个二进制）
#
# ---- Stage 1: build the Vue frontend ----
FROM node:20-alpine AS frontend
WORKDIR /app/worlds/economy/web
COPY worlds/economy/web/package.json worlds/economy/web/package-lock.json* ./
RUN npm install
COPY worlds/economy/web/ ./
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
# 确保前端产物被 webstatic 包 go:embed 到
RUN CGO_ENABLED=0 go build -o /out/economy ./worlds/economy/cmd/economy

# ---- Stage 3: runtime ----
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /out/economy /app/economy
# runtime data volume (sqlite db)
VOLUME ["/data"]
ENV ECO_DB=/data/economy.db \
    ECO_OBS_ADDR=:19100 \
    ECO_INTERVAL=5s \
    ECO_TICK=8s \
    ECO_MAX_HUMANS=100
EXPOSE 19100
CMD ["/app/economy"]
