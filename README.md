# tailscale-gateway

CPA 插件：在 CPA 管理面板中提供一个 **Tailscale 配置生成器**。  
填入 Auth Key / Hostname / Socks5 端口 / 代理池地址 → 生成 config.yaml 片段 → 手动复制到配置文件。

## 工作原理

本插件 **不运行 Tailscale 进程**，仅负责：
1. 在 CPA 面板渲染一个配置表单
2. 接收表单数据，生成 `config.yaml` 片段
3. 你复制片段到 `config.yaml`，重启 CPA
4. CPA 加载插件，插件读取配置后启动 tsnet + SOCKS5 桥

```
CPA (proxy-url: socks5://127.0.0.1:18080)
  └─> 本地 SOCKS5 桥 (插件启动时监听 127.0.0.1:18080)
        └─> tsnet 用户态网络栈 (dial 组网内地址 100.x.x.x:1080)
              └─> Tailscale 组网
                    └─> 代理池服务
```

## 编译

在有 Go 1.23+ 的 Linux 环境（或 Docker）：

```bash
cd tailscale-gateway
go mod tidy
./build.sh
# → dist/tailscale-gateway.so
```

## 部署

1. 将 `dist/tailscale-gateway.so` 复制到 CPA 的 `plugins/` 目录
2. 在 CPA 管理面板打开 **tailscale-gateway** 页面
3. 填写表单，生成配置片段
4. 将片段复制到 `config.yaml`，重启 CPA

## 配置示例

```yaml
plugins:
  enabled: true
  dir: plugins
  configs:
    tailscale-gateway:
      enabled: true
      priority: 1
      auth_key: "tskey-auth-你的key"
      hostname: "cpa-docker"
      socks_port: 18080
      target: "100.x.x.x:1080"
```

- `auth_key`：从 tailscale.com/admin/settings/keys 生成（勾选 Reusable）
- `target`：代理池在 tailnet 内的地址（IP:port）
- `proxy-url`（在 auth 里）：`socks5://127.0.0.1:18080`

## 验证

```bash
# 检查插件状态
curl http://localhost:8317/v0/resource/plugins/tailscale-gateway/status
```

## 注意事项

- Auth Key 必须是 **Reusable** 类型
- SOCKS5 监听仅绑定 `127.0.0.1`
- tsnet 是用户态网络栈，不需要 root 权限或 tun 设备
- 代理池地址从 Tailscale 控制台查看（100.x.x.x 格式）
