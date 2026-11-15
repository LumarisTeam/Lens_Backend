#!/usr/bin/env bash
#
# 服务器单例部署：从镜像仓库拉取 CI 预构建镜像，复用本目录的 compose 定义起一个容器。
#
# 为什么是"拉"而不是"构建"：CI（.github/workflows/build-image.yml）已经在
# ubuntu-latest 上完成 vet/test 并构建、推送了镜像；服务器只负责拉取与重启，
# 不必在服务器上跑一次完整的 Go 构建（拉 golang 基础镜像 + 编译全部依赖）。
#
# 镜像来源：CI 把同一批 tag 双推两份——ghcr 作归档，腾讯云 TCR 供国内服务器拉取
# （ghcr 的镜像层走 pkg-containers.githubusercontent.com，在国内基本拉不动）。
# 默认走 TCR；要用 ghcr 就 USE_GHCR=1，或用 IMAGE 直接给完整镜像名。
#
# 用法：
#   ./build_from_ghcr.sh
#   ./build_from_ghcr.sh ccr.ccs.tencentyun.com/lumaris/lens_backend:<commit-sha>   # 指定版本，也是回滚方式
#   APP_PORT=8080 ./build_from_ghcr.sh
#   IMAGE=... APP_PORT=... COMPOSE_PROJECT_NAME=... ./build_from_ghcr.sh
#   USE_GHCR=1 ./build_from_ghcr.sh       # 改从 ghcr.io 拉（国内一般拉不动）
#   ENV_FILE=/etc/lens-backend/backend.env ./build_from_ghcr.sh   # 配置放仓库外
#   TAKE_OVER=1 ./build_from_ghcr.sh     # 同名容器不是 compose 起的时，先删掉它再起
#   PULL_USERNAME=<id> PULL_TOKEN=<token> ./build_from_ghcr.sh     # 私有仓库凭据，用完即登出
#   sh build_from_ghcr.sh                 # /bin/sh 是 dash 时同样可用（脚本会自己切到 bash）
#
# 同目录必须有：
#   docker-compose.production.yml（或 docker-compose.yml / docker-compose.yaml）
#   .env                         （或通过 ENV_FILE 指向别处的文件，见仓库根 .env.example）
#
# 注意：容器名是 lens-backend。旧部署如果还跑着 README 历史版本里的 bug-feedback
# 容器，它会占着同一个宿主机端口，本脚本会在启动前明确报出来，先
# `docker rm -f bug-feedback` 再重跑。

# 服务器上最常见的调用方式是 `sh build_from_ghcr.sh`，而 Debian/Ubuntu 的 /bin/sh 是 dash：
# 它既没有 pipefail，也没有 [[ ]] / (( )) / $SECONDS / $BASH_SOURCE，会在下面那行 set
# 直接以 "Illegal option -o pipefail" 退出，连参数校验都轮不到。
# 检测到当前不是 bash 就用 bash 重新执行自己，让 `sh x.sh` 与 `./x.sh` 完全等价。
if [ -z "${BASH_VERSION:-}" ]; then
  exec bash "$0" "$@"
fi

set -euo pipefail

CONTAINER_NAME="lens-backend"
SERVICE_NAME="lens-backend"

if [[ "${USE_GHCR:-0}" == "1" ]]; then
  DEFAULT_IMAGE="ghcr.io/lumaristeam/lens_backend:latest"
else
  DEFAULT_IMAGE="ccr.ccs.tencentyun.com/lumaris/lens_backend:latest"
fi
IMAGE="${IMAGE:-${1:-$DEFAULT_IMAGE}}"

# 镜像仓库域名直接从镜像名里取：登录、登出都用它，换 registry 时不必再改别处。
REGISTRY_HOST="${IMAGE%%/*}"

APP_PORT="${APP_PORT:-8080}"
READY_TIMEOUT="${READY_TIMEOUT:-60}"

# 生产配置默认取脚本同目录的 .env；也可以指向仓库外的绝对路径。
ENV_FILE="${ENV_FILE:-.env}"

# compose 的 project 名默认取目录名。这里显式固定，否则固定 container_name 会与旧 project 撞名。
COMPOSE_PROJECT_NAME="${COMPOSE_PROJECT_NAME:-lens-backend}"

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# 显式导出：compose 插值优先级是 shell 环境 > 同目录 .env
export IMAGE APP_PORT ENV_FILE COMPOSE_PROJECT_NAME

# ------------------------------------------------------------------ 前置检查

command -v docker >/dev/null 2>&1 || { echo "错误：未找到 docker。" >&2; exit 1; }
docker compose version >/dev/null 2>&1 || { echo "错误：需要 docker compose v2 插件（docker compose，而非旧版 docker-compose）。" >&2; exit 1; }

if [[ ! "$APP_PORT" =~ ^[0-9]+$ ]] || (( APP_PORT < 1 || APP_PORT > 65535 )); then
  echo "错误：APP_PORT 必须是 1 到 65535 之间的端口号，收到：'$APP_PORT'" >&2
  exit 1
fi

# production 文件排在最前：万一有人把本脚本和仓库根目录的开发 compose 放在一起，
# 也不会误起 postgres/redis 那套本地依赖。
COMPOSE_FILE=""
for candidate in docker-compose.production.yml docker-compose.yml docker-compose.yaml; do
  if [[ -f "$candidate" ]]; then COMPOSE_FILE="$candidate"; break; fi
done
if [[ -z "$COMPOSE_FILE" ]]; then
  echo "错误：$SCRIPT_DIR 下找不到 docker-compose.production.yml 或 docker-compose.yml。" >&2
  exit 1
fi

if [[ ! -f "$ENV_FILE" ]]; then
  cat >&2 <<'ENV_HELP'
错误：未找到 .env。compose 的 env_file 指向它，缺失时 docker compose 会直接失败。

请在当前目录创建 .env（或设置 ENV_FILE 指向别处），至少包含：
  DB_DSN=postgres://user:pass@host:port/db          # 必需，缺失或连不上时进程直接退出
  REDIS_ADDR=host:port                              # 必需，反馈中心缓存/校验码/限流都依赖它
  S3_ACCESS_KEY=...                                 # 必需，COS SecretId
  S3_SECRET_KEY=...                                 # 必需，COS SecretKey
  S3_BUCKET=...                                     # 必需，桶名（含 appid）
  ADMIN_API_TOKEN=<openssl rand -hex 32>            # 必需，/api/admin 的鉴权令牌
  FEEDBACK_SECRET_KEY=<openssl rand -hex 32>        # 必需，至少 32 字节随机值
  APP_ENV=production                                # 生产必改，否则 gin 为 DebugMode

完整说明见仓库根目录的 .env.example。注意 .env 不要提交进 git（.gitignore 已排除它）。
ENV_HELP
  exit 1
fi

# 固定 container_name 被既有容器占用时，compose 只抛一句
# "Conflict. The container name ... is already in use"，既不说原因也不说怎么办，
# 所以这里提前把两种情形分开讲清楚：
#   有 compose 标签但 project 不同 —— compose 不会接管别的 project 的容器
#   完全没有 compose 标签          —— 是 `docker run` 起的（README 历史部署路径）
if docker container inspect "$CONTAINER_NAME" >/dev/null 2>&1; then
  owner="$(docker container inspect -f '{{index .Config.Labels "com.docker.compose.project"}}' "$CONTAINER_NAME" 2>/dev/null || true)"
  [[ "$owner" == "<no value>" ]] && owner=""

  if [[ -z "$owner" ]]; then
    if [[ "${TAKE_OVER:-0}" == "1" ]]; then
      echo "==> 移除既有容器 ${CONTAINER_NAME}（docker run 创建，不受 compose 管理）"
      docker rm -f "$CONTAINER_NAME" >/dev/null
    else
      cat >&2 <<TAKEOVER
错误：容器 $CONTAINER_NAME 已存在，但它不带 compose 标签，也就是由 docker run 直接创建的。

compose 不会接管这种容器，直接 up 就会报：
  Conflict. The container name "/$CONTAINER_NAME" is already in use

删它之前先确认它确实是可停的旧容器：
  docker inspect -f '{{.Config.Image}}' $CONTAINER_NAME
  docker ps --filter name=$CONTAINER_NAME

确认无误后二选一：
  docker rm -f $CONTAINER_NAME     # 然后重跑本脚本
  TAKE_OVER=1 $0                   # 让本脚本替你删掉再起
TAKEOVER
      exit 1
    fi
  elif [[ "$owner" != "$COMPOSE_PROJECT_NAME" ]]; then
    cat >&2 <<CONFLICT
错误：容器 $CONTAINER_NAME 已存在，但属于另一个 compose project「${owner}」。

compose 不会接管别的 project 的容器。二选一：
  docker rm -f $CONTAINER_NAME          # 让本脚本接管
  COMPOSE_PROJECT_NAME=$owner $0   # 沿用那个 project
CONFLICT
    exit 1
  fi
fi

# 旧部署可能不是同名容器，而是 README 历史版本的 bug-feedback：它不撞容器名，
# 但会占着同一个宿主机端口，compose 启动时只会抛 "port is already allocated"。
# 这里提前把占用者点出来，省得对着 compose 的报错猜。
port_owner="$(docker ps --filter "publish=${APP_PORT}" --format '{{.Names}}' | head -n 1 || true)"
if [[ -n "$port_owner" && "$port_owner" != "$CONTAINER_NAME" ]]; then
  cat >&2 <<PORT_CONFLICT
错误：宿主机端口 ${APP_PORT} 已被容器 ${port_owner} 占用。

如果它是迁移前的旧服务（历史上叫 bug-feedback），先停掉并删除旧容器：
  docker rm -f ${port_owner}
再重跑本脚本；如果那个服务还要保留，就用 APP_PORT=<另一个端口> 给 Lens 换端口。
PORT_CONFLICT
  exit 1
fi

# ------------------------------------------------------------------ 镜像仓库登录

# 镜像若为私有，需要凭据。优先用传入的 token（用完即登出）；否则沿用本机已有的
# docker 凭据（此前手动 docker login <registry> 过就行）。
if [[ -n "${PULL_TOKEN:-}" ]]; then
  : "${PULL_USERNAME:?设置了 PULL_TOKEN 就必须同时设置 PULL_USERNAME}"
  trap 'docker logout "$REGISTRY_HOST" >/dev/null 2>&1 || true' EXIT
  printf '%s' "$PULL_TOKEN" | docker login "$REGISTRY_HOST" --username "$PULL_USERNAME" --password-stdin
fi

# ------------------------------------------------------------------ 拉取与启动

# 先记下当前在跑的镜像，末尾用它给出准确的回滚命令
previous_image="$(docker container inspect -f '{{.Config.Image}}' "$CONTAINER_NAME" 2>/dev/null || true)"

echo "==> 拉取镜像 $IMAGE"
docker compose -f "$COMPOSE_FILE" pull "$SERVICE_NAME"

echo "==> 启动容器（project=${COMPOSE_PROJECT_NAME}）"
docker compose -f "$COMPOSE_FILE" up -d --no-build --remove-orphans "$SERVICE_NAME"

# ------------------------------------------------------------------ 就绪等待

# 启动阶段的失败大多发生在服务监听之前：DB_DSN 缺失或库连不上、Redis ping 失败、
# FEEDBACK_SECRET_KEY 长度不够、COS 客户端初始化失败——这些都会让进程直接 os.Exit(1)。
# 所以不只看容器在不在跑，还要等 /healthz 真的返回 200。
# /healthz 会 ping 一次 PostgreSQL，因此这里等的是「能对外服务」，而不只是「进程没退出」。
# 镜像基于 alpine，busybox 自带 wget；容器以非 root 用户运行，访问本机端口无需提权。
echo "==> 等待服务就绪（最多 ${READY_TIMEOUT}s）"
deadline=$(( SECONDS + READY_TIMEOUT ))
ready=0
while (( SECONDS < deadline )); do
  running="$(docker container inspect -f '{{.State.Running}}' "$CONTAINER_NAME" 2>/dev/null || echo false)"
  if [[ "$running" != "true" ]]; then
    echo "错误：容器 $CONTAINER_NAME 已退出。日志：" >&2
    docker logs --tail 60 "$CONTAINER_NAME" >&2 || true
    exit 1
  fi

  if docker exec "$CONTAINER_NAME" wget -q -O /dev/null http://127.0.0.1:8080/healthz 2>/dev/null; then
    ready=1
    break
  fi
  sleep 1
done

if (( ! ready )); then
  echo "错误：${READY_TIMEOUT}s 内 /healthz 没有返回成功。最近日志：" >&2
  docker logs --tail 60 "$CONTAINER_NAME" >&2 || true
  exit 1
fi

# ------------------------------------------------------------------ 收尾

# 只清 dangling 镜像（不带 -a，不会动还有 tag 的镜像）。
docker image prune --force >/dev/null

digest="$(docker image inspect --format '{{index .RepoDigests 0}}' "$IMAGE" 2>/dev/null || true)"
health="$(docker container inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}无{{end}}' "$CONTAINER_NAME" 2>/dev/null || true)"

echo
echo "==> 部署完成"
echo "    容器 : $CONTAINER_NAME"
echo "    镜像 : $IMAGE"
if [[ -n "$digest" ]]; then echo "    摘要 : $digest"; fi
echo "    端口 : 宿主机 ${APP_PORT} -> 容器 8080"
echo "    健康 : ${health:-未知}（镜像自带 HEALTHCHECK，探针是 /healthz）"
echo
echo "    自检 : curl -fsS http://localhost:${APP_PORT}/healthz"
echo "    排障 : docker logs -f $CONTAINER_NAME"
echo "           docker exec -it $CONTAINER_NAME sh（基础镜像是 alpine，有 shell）"
if [[ -n "$previous_image" && "$previous_image" != "$IMAGE" ]]; then
  echo "    回滚 : $0 $previous_image"
fi
