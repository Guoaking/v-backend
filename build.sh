#!/bin/sh
set -e
CMD="$1"
IMAGE_NAME="${IMAGE_NAME:-ver/verilocale-backend}"
REMOTE="${REMOTE:-root@159.138.228.40}"
SSH_KEY="${SSH_KEY:-$HOME/.ssh/KeyPair-v2.pem}"
REMOTE_DIR="${REMOTE_DIR:-/root/gk/backend}"
TS=$(date +%Y%m%d-%H%M%S)
REMOTE_BASE="${REMOTE_DIR%/*}"

LOCAL_DEV_DIR="artifacts/dev"
LOCAL_PROD_DIR="artifacts/prod"

send_files() {
  OUT_FILE="$1"
  OUT_BASE="$(basename "$OUT_FILE")"
  scp -i "$SSH_KEY" "$OUT_FILE" "$REMOTE:$REMOTE_BASE/"
  scp -i "$SSH_KEY" -r docker-compose.yml "$REMOTE:$REMOTE_DIR/"
  # Optional: send configs if needed, though usually managed on server or via env
  [ -f config.prod.yaml ] && scp -i "$SSH_KEY" config.prod.yaml "$REMOTE:$REMOTE_DIR/config.yaml"
  [ -f config.prod.yaml ] && scp -i "$SSH_KEY" config.prod.yaml "$REMOTE:$REMOTE_DIR/config.prod.yaml"
  [ -f config.dev.yaml ] && scp -i "$SSH_KEY" config.dev.yaml "$REMOTE:$REMOTE_DIR/config.dev.yaml"
}

remote() {
  ssh "$REMOTE" -i "$SSH_KEY" "$1"
}

health_wait() {
  NAME="$1"
  LIM="$2"
  remote "for i in \$(seq 1 $LIM); do s=\$(docker inspect -f '{{.State.Health.Status}}' $NAME 2>/dev/null || echo none); [ \"\$s\" = \"healthy\" ] && echo HEALTHY && exit 0; sleep 2; done; echo HEALTH_CHECK_FAILED; exit 1"
}

prune_local_tars() {
  dir="$1"; keep="$2"
  [ -d "$dir" ] || return 0
  # shellcheck disable=SC2012
  count=$(ls -1t "$dir"/*.tar.gz 2>/dev/null | wc -l | tr -d ' ')
  [ "$count" -le "$keep" ] && return 0
  # shellcheck disable=SC2012
  ls -1t "$dir"/*.tar.gz 2>/dev/null | tail -n +$(expr $keep + 1) | xargs -r rm -f
}

prune_remote_tars() {
  remote_dir="$1"; keep="$2"
  remote "mkdir -p $remote_dir"
  remote "set -e; cd $remote_dir; list=\$(ls -1t 2>/dev/null | wc -l); if [ \"\$list\" -gt $keep ]; then ls -1t | tail -n +$(expr $keep + 1) | xargs -r rm -f; fi"
}

prune_remote_prod_images() {
  keep="$1"
  # Keep currently running prod image and latest N prod-* tags
  remote "cur=\$(docker inspect -f '{{.Config.Image}}' verilocale-backend 2>/dev/null || echo ''); \
  rep='${IMAGE_NAME}'; \
  tags=\$(docker images \"\$rep\" --format '{{.Tag}}' | grep '^prod-' | sort -r); \
  keep_list=\$(echo \"\$tags\" | tr ' ' '\n' | head -n $keep | tr '\n' ' '); \
  echo \"Current Image: \$cur\"; \
  echo \"Keeping Tags: \$keep_list\"; \
  for t in \$tags; do \
    echo \"Checking tag: \$t\"; \
    if echo \"\$keep_list\" | grep -q \"\<$t\>\"; then \
      echo \"  Skipping (Keep List)\"; \
      continue; \
    fi; \
    if [ -n \"\$cur\" ] && echo \"\$cur\" | grep -q \"\:$t\"; then \
      echo \"  Skipping (Currently Running)\"; \
      continue; \
    fi; \
    echo \"  Removing \$rep:\$t\"; \
    docker rmi \"\$rep:\$t\" || true; \
  done"
}

case "$CMD" in
  dev)
    DEV_TAG="${DEV_TAG:-dev}"
    IMG="$IMAGE_NAME:$DEV_TAG"
    mkdir -p "$LOCAL_DEV_DIR"
    OUT="$LOCAL_DEV_DIR/${IMAGE_NAME##*/}_dev_$TS.tar.gz"
    OUT_BASE="$(basename "$OUT")"
    REMOTE_OUT="$REMOTE_BASE/$OUT_BASE"
    
    # Gen Swagger
    ./scripts/gen-swagger.sh
    
    # Fast Local Build (Dev)
    echo "Building Backend Locally (Linux/AMD64)..."
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o kyc-service-linux ./cmd/server
    
    # Build Docker Image (Use pre-built binary)
    docker buildx build --platform linux/amd64 -f Dockerfile.dev -t "$IMG" .
    docker save "$IMG" | gzip > "$OUT"
    send_files "$OUT"
    remote "docker load -i $REMOTE_OUT"
    remote "mkdir -p $REMOTE_BASE/backups/dev; mv -f $REMOTE_OUT $REMOTE_BASE/backups/dev/"
    prune_remote_tars "$REMOTE_BASE/backups/dev" 2
    prune_local_tars "$LOCAL_DEV_DIR" 2
    remote "cd $REMOTE_DIR; DEV_TAG=$DEV_TAG docker compose up -d dev-backend"
    health_wait "verilocale-backend-dev" 60
    ;;
  prod)
    PROD_TAG_IN="${PROD_TAG:-prod-$TS}"
    IMG="$IMAGE_NAME:$PROD_TAG_IN"
    mkdir -p "$LOCAL_PROD_DIR"
    OUT="$LOCAL_PROD_DIR/${IMAGE_NAME##*/}_prod_$TS.tar.gz"
    OUT_BASE="$(basename "$OUT")"
    REMOTE_OUT="$REMOTE_BASE/$OUT_BASE"
    
    # Gen Swagger Public
    ./scripts/gen-swagger-public.sh
    
    docker buildx build --platform linux/amd64 -t "$IMG" .
    docker save "$IMG" | gzip > "$OUT"
    send_files "$OUT"
    remote "mkdir -p $REMOTE_BASE/backups"
    # Backup current running image
    remote "cur=\$(docker inspect -f '{{.Config.Image}}' verilocale-backend 2>/dev/null || echo ''); if [ -n \"\$cur\" ]; then tag=\$(echo \"\$cur\" | awk -F: '{print \$2}'); [ -n \"\$tag\" ] && docker save \"\$cur\" | gzip > $REMOTE_BASE/backups/${IMAGE_NAME##*/}_\$tag.tar.gz && echo \"\$tag\" > $REMOTE_DIR/.prod_prev; fi || true"
    
    remote "docker load -i $REMOTE_OUT"
    remote "mkdir -p $REMOTE_BASE/backups/prod; mv -f $REMOTE_OUT $REMOTE_BASE/backups/prod/"
    prune_remote_tars "$REMOTE_BASE/backups/prod" 3
    prune_local_tars "$LOCAL_PROD_DIR" 3
    remote "cd $REMOTE_DIR; PROD_TAG=$PROD_TAG_IN docker compose up -d kyc-service"
    health_wait "kyc-service" 60
    prune_remote_prod_images 3
    ;;
  promote)
    SRC_TAG="${SRC_TAG:-dev}"
    NEW_TAG="${PROD_TAG:-prod-$TS}"
    remote "img_id=\$(docker images -q '$IMAGE_NAME:$SRC_TAG'); [ -z \"\$img_id\" ] && echo MISSING_DEV_IMAGE && exit 1 || docker tag '$IMAGE_NAME:$SRC_TAG' '$IMAGE_NAME:$NEW_TAG'"
    remote "mkdir -p $REMOTE_BASE/backups"
    
    # Backup current running image
    remote "cur=\$(docker inspect -f '{{.Config.Image}}' verilocale-backend 2>/dev/null || echo ''); if [ -n \"\$cur\" ]; then tag=\$(echo \"\$cur\" | awk -F: '{print \$2}'); [ -n \"\$tag\" ] && docker save \"\$cur\" | gzip > $REMOTE_BASE/backups/${IMAGE_NAME##*/}_\$tag.tar.gz && echo \"\$tag\" > $REMOTE_DIR/.prod_prev; fi || true"
    
    # Clean up existing container to avoid name conflict
    remote "docker rm -f kyc-service 2>/dev/null || true"
    remote "cd $REMOTE_DIR; PROD_TAG=$NEW_TAG docker compose up -d kyc-service"
    health_wait "kyc-service" 60
    prune_remote_prod_images 3
    ;;
  rollback)
    remote "cd $REMOTE_DIR; prev=\$(cat .prod_prev 2>/dev/null || echo 1.0.0); PROD_TAG=\$prev docker compose up -d kyc-service"
    health_wait "kyc-service" 60
    ;;
  *)
    echo "Usage: ./build.sh [dev|prod|promote|rollback]"
    echo "ENV: IMAGE_NAME, REMOTE, SSH_KEY, REMOTE_DIR, DEV_TAG, PROD_TAG, SRC_TAG"
    exit 1
    ;;
esac
