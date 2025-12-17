-- Kong插件：服务响应验证插件
-- 用于验证后端服务的响应签名，确保服务身份

local kong = kong
local crypto = require "crypto"
local cjson = require "cjson"

local plugin = {
  PRIORITY = 1000,
  VERSION = "1.0.0",
}

-- 插件配置schema
local schema = {
  fields = {
    { service_secret_key = { type = "string", required = true } },
    { service_name = { type = "string", required = true } },
    { validate_timestamp = { type = "boolean", default = true } },
    { timestamp_window = { type = "number", default = 300 } }, -- 5分钟
    { enable_logging = { type = "boolean", default = true } },
  },
}

plugin.schema = schema

-- 生成HMAC签名
local function generate_signature(message, key)
  local hmac = crypto.hmac.new("sha256", key)
  hmac:update(message)
  local digest = hmac:final()
  return kong.utils.base64_encode(digest)
end

-- 验证服务签名
local function verify_service_signature(conf, headers, path, method)
  local service_signature = headers["x-service-signature"]
  local service_timestamp = headers["x-service-timestamp"]
  local service_name = headers["x-service-name"]
  local service_nonce = headers["x-service-nonce"]

  -- 检查必需的头
  if not service_signature or not service_timestamp or not service_name or not service_nonce then
    return false, "缺少服务认证头"
  end

  -- 验证时间戳
  if conf.validate_timestamp then
    local timestamp = tonumber(service_timestamp)
    if not timestamp then
      return false, "服务时间戳格式错误"
    end

    local current_time = os.time()
    local time_diff = math.abs(current_time - timestamp)
    
    if time_diff > conf.timestamp_window then
      return false, "服务时间戳超出有效范围"
    end
  end

  -- 构建签名消息
  local message = string.format("%s:%s:%s:%s:%s", 
    service_name, path, method, service_timestamp, service_nonce)

  -- 生成期望的签名
  local expected_signature = generate_signature(message, conf.service_secret_key)

  -- 比较签名
  if service_signature ~= expected_signature then
    return false, "服务签名验证失败"
  end

  return true, nil
end

-- 生成Kong网关签名（用于请求到服务）
local function generate_kong_signature(conf, path, method)
  local timestamp = os.time()
  local message = string.format("%s:%s:%s:%s", 
    conf.service_name, path, method, timestamp)
  local signature = generate_signature(message, conf.service_secret_key)

  return {
    signature = signature,
    timestamp = timestamp,
    service = conf.service_name
  }
end

-- 请求阶段：添加Kong认证头
function plugin:access(conf)
  local path = kong.request.get_path()
  local method = kong.request.get_method()

  -- 生成Kong签名
  local kong_auth = generate_kong_signature(conf, path, method)

  -- 设置Kong认证头
  kong.service.request.set_header("X-Kong-Signature", kong_auth.signature)
  kong.service.request.set_header("X-Kong-Timestamp", kong_auth.timestamp)
  kong.service.request.set_header("X-Kong-Service", conf.service_name)
  kong.service.request.set_header("X-Forwarded-By", "Kong")

  if conf.enable_logging then
    kong.log.info("添加Kong认证头 - 服务: ", conf.service_name, " 路径: ", path)
  end
end

-- 响应阶段：验证服务签名
function plugin:header_filter(conf)
  local headers = kong.response.get_headers()
  local path = kong.request.get_path()
  local method = kong.request.get_method()

  -- 跳过健康检查路径
  if string.match(path, "/health") then
    return
  end

  -- 验证服务签名
  local valid, error_msg = verify_service_signature(conf, headers, path, method)
  
  if not valid then
    kong.log.err("服务签名验证失败: ", error_msg)
    
    -- 记录安全事件
    kong.log.err("安全事件 - 服务签名验证失败: ", error_msg, 
      " 服务: ", conf.service_name, " 路径: ", path)

    -- 返回错误响应
    kong.response.set_status(502)
    kong.response.set_header("Content-Type", "application/json")
    kong.response.set_raw_body(cjson.encode({
      error = "服务认证失败",
      code = "SERVICE_AUTH_FAILED",
      message = error_msg
    }))
    return
  end

  if conf.enable_logging then
    kong.log.info("服务签名验证通过 - 服务: ", conf.service_name, " 路径: ", path)
  end
end

-- 日志阶段：记录安全事件
function plugin:log(conf)
  local status = kong.response.get_status()
  
  -- 记录认证失败事件
  if status == 401 or status == 403 or status == 502 then
    local path = kong.request.get_path()
    local client_ip = kong.client.get_ip()
    
    kong.log.warn("安全事件 - 状态码: ", status, 
      " 路径: ", path, " 客户端IP: ", client_ip)
  end
end

return plugin