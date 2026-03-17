#!/bin/bash
set -e

# 配置
BACKUP_FILE="$1"
CONTAINER_NAME="database"
DB_USER="kong"
# 目标数据库名，如果备份文件包含 CREATE DATABASE 语句，这个参数主要用于建立连接
# 如果不指定，默认连接到 postgres 库执行
TARGET_DB="postgres"

if [ -z "$BACKUP_FILE" ]; then
  echo "Usage: ./scripts/restore-db.sh <backup_file_path> [target_db_name]"
  echo "Example: ./scripts/restore-db.sh ./backups/kong_backup_20240315.sql.gz"
  exit 1
fi

if [ ! -f "$BACKUP_FILE" ]; then
  echo "Error: Backup file '$BACKUP_FILE' not found!"
  exit 1
fi

# 如果指定了第二个参数，则作为目标库名（仅当备份文件不含 CREATE DATABASE 时有用）
if [ -n "$2" ]; then
  TARGET_DB="$2"
fi

echo "⚠️  WARNING: This will restore database from '$BACKUP_FILE' to container '$CONTAINER_NAME'."
echo "Target connection database: $TARGET_DB"
read -p "Are you sure you want to continue? (y/N) " confirm
if [[ "$confirm" != "y" && "$confirm" != "Y" ]]; then
  echo "Restore cancelled."
  exit 0
fi

echo "Starting restore..."

# 解压并恢复
# psql 接收 SQL 文本流执行
# 注意：备份文件中已包含 DROP/CREATE DATABASE 语句（由 backup-db.sh 的 --create --clean 参数生成）
# 我们连接到 postgres 库来执行这些全局操作
if gunzip -c "$BACKUP_FILE" | docker exec -i "$CONTAINER_NAME" psql -U "$DB_USER" -d "$TARGET_DB"; then
  echo "✅ Restore successful!"
else
  echo "❌ Restore failed!"
  exit 1
fi
