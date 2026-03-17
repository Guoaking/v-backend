#!/bin/bash
set -e

# 配置
BACKUP_DIR="./backups"
CONTAINER_NAME="database"  # docker-compose 默认的服务名通常是 project_database_1，请根据实际情况调整
DB_USER="kong"
# 支持备份多个数据库，用空格分隔，例如 "kong app_db metrics_db"
# 默认备份 kong 数据库
DATABASES="kong"
RETENTION_DAYS=7

# 确保备份目录存在
mkdir -p "$BACKUP_DIR"

# 获取当前时间戳
TIMESTAMP=$(date +"%Y%m%d_%H%M%S")

echo "Starting backup process..."
echo "Container: $CONTAINER_NAME"
echo "Databases: $DATABASES"

# 遍历数据库列表进行备份
for DB_NAME in $DATABASES; do
    BACKUP_FILE="$BACKUP_DIR/${DB_NAME}_backup_$TIMESTAMP.sql.gz"
    echo "----------------------------------------"
    echo "Backing up database '$DB_NAME'..."

    # 执行备份：全量逻辑导出 -> Gzip 压缩 -> 保存到本地
    # -U: 用户名
    # -d: 数据库名
    # --clean: 包含 DROP 语句（恢复时会先删除旧对象）
    # --if-exists: 配合 --clean 使用，避免对象不存在报错
    # --create: 包含 CREATE DATABASE 语句
    if docker exec "$CONTAINER_NAME" pg_dump -U "$DB_USER" -d "$DB_NAME" --clean --if-exists --create | gzip > "$BACKUP_FILE"; then
        # 检查备份文件大小，确保不为空（gzip 空文件也有几十字节头信息，这里简单检查是否成功生成）
        if [ -s "$BACKUP_FILE" ]; then
             echo "✅ Backup successful: $BACKUP_FILE"
             ls -lh "$BACKUP_FILE"
        else
             echo "❌ Backup failed: File is empty"
             rm -f "$BACKUP_FILE"
        fi
    else
        echo "❌ Backup failed for '$DB_NAME'!"
        # 不退出脚本，继续备份下一个数据库
        rm -f "$BACKUP_FILE"
    fi
done

# 清理旧备份（保留最近7天）
echo "Cleaning up backups older than $RETENTION_DAYS days..."
find "$BACKUP_DIR" -name "db_backup_*.sql.gz" -mtime +$RETENTION_DAYS -delete
echo "Cleanup complete."
