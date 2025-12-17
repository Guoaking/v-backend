#!/bin/bash

# mTLS证书生成和配置脚本
# 为Kong网关和后端服务生成双向TLS证书

set -e

CERT_DIR="./certs"
KONG_CERT_DIR="${CERT_DIR}/kong"
SERVICE_CERT_DIR="${CERT_DIR}/service"
CA_CERT_DIR="${CERT_DIR}/ca"

echo "🔐 开始生成mTLS证书..."

# 创建证书目录
echo "📁 创建证书目录..."
mkdir -p "$KONG_CERT_DIR" "$SERVICE_CERT_DIR" "$CA_CERT_DIR"

# 生成CA私钥和证书
echo "🔑 生成CA根证书..."
openssl genrsa -out "$CA_CERT_DIR/ca-key.pem" 4096
openssl req -new -x509 -days 3650 -key "$CA_CERT_DIR/ca-key.pem" -out "$CA_CERT_DIR/ca-cert.pem" \
  -subj "/C=CN/ST=Beijing/L=Beijing/O=KYC-Service/CN=KYC-CA"

# 生成Kong网关证书
echo "🌐 生成Kong网关证书..."
openssl genrsa -out "$KONG_CERT_DIR/kong-key.pem" 4096
openssl req -new -key "$KONG_CERT_DIR/kong-key.pem" -out "$KONG_CERT_DIR/kong.csr" \
  -subj "/C=CN/ST=Beijing/L=Beijing/O=KYC-Service/CN=kong-gateway"

# 使用CA签名Kong证书
openssl x509 -req -days 3650 -in "$KONG_CERT_DIR/kong.csr" \
  -CA "$CA_CERT_DIR/ca-cert.pem" -CAkey "$CA_CERT_DIR/ca-key.pem" -CAcreateserial \
  -out "$KONG_CERT_DIR/kong-cert.pem"

# 生成后端服务证书
echo "🔧 生成后端服务证书..."
openssl genrsa -out "$SERVICE_CERT_DIR/service-key.pem" 4096
openssl req -new -key "$SERVICE_CERT_DIR/service-key.pem" -out "$SERVICE_CERT_DIR/service.csr" \
  -subj "/C=CN/ST=Beijing/L=Beijing/O=KYC-Service/CN=kyc-service"

# 使用CA签名服务证书
openssl x509 -req -days 3650 -in "$SERVICE_CERT_DIR/service.csr" \
  -CA "$CA_CERT_DIR/ca-cert.pem" -CAkey "$CA_CERT_DIR/ca-key.pem" -CAcreateserial \
  -out "$SERVICE_CERT_DIR/service-cert.pem"

# 设置证书权限
echo "🔒 设置证书权限..."
chmod 600 "$CA_CERT_DIR/ca-key.pem"
chmod 600 "$KONG_CERT_DIR/kong-key.pem"
chmod 600 "$SERVICE_CERT_DIR/service-key.pem"
chmod 644 "$CA_CERT_DIR/ca-cert.pem"
chmod 644 "$KONG_CERT_DIR/kong-cert.pem"
chmod 644 "$SERVICE_CERT_DIR/service-cert.pem"

# 验证证书
echo "✅ 验证证书..."
openssl x509 -in "$CA_CERT_DIR/ca-cert.pem" -noout -text | grep "Issuer\|Subject"
openssl x509 -in "$KONG_CERT_DIR/kong-cert.pem" -noout -text | grep "Issuer\|Subject"
openssl x509 -in "$SERVICE_CERT_DIR/service-cert.pem" -noout -text | grep "Issuer\|Subject"

echo "🎉 mTLS证书生成完成！"
echo ""
echo "📋 证书文件："
echo "  • CA根证书: $CA_CERT_DIR/ca-cert.pem"
echo "  • CA私钥: $CA_CERT_DIR/ca-key.pem"
echo "  • Kong证书: $KONG_CERT_DIR/kong-cert.pem"
echo "  • Kong私钥: $KONG_CERT_DIR/kong-key.pem"
echo "  • 服务证书: $SERVICE_CERT_DIR/service-cert.pem"
echo "  • 服务私钥: $SERVICE_CERT_DIR/service-key.pem"
echo ""
echo "🔧 下一步配置："
echo "  1. 配置Kong网关mTLS:"
echo "     将证书挂载到Kong容器，配置client_ssl和ca_certificates"
echo ""
echo "  2. 配置后端服务mTLS:"
echo "     修改服务配置，启用HTTPS和客户端证书验证"
echo ""
echo "  3. 运行配置脚本:"
echo "     ./configure-mtls.sh"