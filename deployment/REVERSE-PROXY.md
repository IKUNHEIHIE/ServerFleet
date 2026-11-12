# HTTPS 与 Agent mTLS 反代参考

直接 HTTPS/WSS 连接 Controller 最简单；若使用 Nginx，管理页面/API 可在 Nginx 终止 TLS，但 Agent 的入网和身份链路使用 TCP stream 透传，客户端证书必须到达 Controller。不要通过任意 HTTP 头伪造 Agent 身份。

示例角色：管理域名 `panel.example.com:443`，Agent 域名 `agents.example.com:9443`，Controller 仅监听 `127.0.0.1:8443`。这些是示例，不是协议固定端口。Controller 服务端证书的 SAN 应覆盖两个域名；私有 HTTPS CA 须独立可信，节点身份 Root pin 仍须独立核验。

Controller 参数相互对应：

```sh
controller -listen 127.0.0.1:8443 -origin https://panel.example.com \
  -agent-authority agents.example.com:9443 -agent-server-name agents.example.com \
  -tls-cert /srv/serverfleet/tls/server-chain.pem -tls-key /srv/serverfleet/tls/server-key.pem \
  -db /srv/serverfleet/fleet.db -agent-root-pin ROOT_SPKI_SHA256 \
  -agent-pki-dir /srv/serverfleet/pki -credential-key-dir /srv/serverfleet/credential-keys \
  -web-dir /srv/serverfleet/package/web
```

`ROOT_SPKI_SHA256` 是部署时独立核验的值。其他必要初始化/监督参数仍按 README 配置，以上命令不创建 PKI 或管理员。

把 `examples/nginx-controller.conf` 的 server 放在 Nginx `http` 块内，把 `examples/nginx-agent-stream.conf` 的 stream 放在顶层；确认已安装受支持的 stream 模块，替换证书/CA 路径后执行 `nginx -t`。不要让两个进程同时绑定同一地址/端口；`0.0.0.0:8443` 通配监听也会占用该端口的回环地址。

Host 和 Origin 都精确匹配配置。`-origin https://panel.example.com` 对应 `Host: panel.example.com`、`Origin: https://panel.example.com`；非默认端口必须整体一致，例如 `https://panel.example.com:8443`。不是禁止端口；不要一端带端口、另一端遗漏，也不要在配置中随意追加默认 `:443`。保留 `$http_host`，不要用会去掉端口的变量替换。原始 Origin 保留，不由代理伪造为“正确”值。

Agent 使用 `https://agents.example.com:9443` / `wss://agents.example.com:9443`。普通 HTTPS 反代产生的是 Nginx 到 Controller 的新 TLS 连接，不能保留 Agent 客户端证书。管理反代到 Controller 的 HTTPS 也必须开启上游证书校验；不使用 `proxy_ssl_verify off` 或明文上游。

`/healthz` 表示基本存活；`/readyz` 检查数据库、当前运行权威、任务调度器和已配置 Agent transport/签发器，返回 200 或 503。无在线节点、没有配置供应商，不会单独造成全局未就绪。响应只提供固定类别，不公开路径、指纹、节点数量或秘密。
