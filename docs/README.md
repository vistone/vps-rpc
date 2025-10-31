# VPS-RPC 高速数据爬虫代理（更新版）

基于 uTLS + QUIC + RPC 技术构建的高速数据爬虫代理系统。

- 文档索引：`docs/INDEX.md`
- 架构：`docs/ARCHITECTURE.md`，配置：`docs/CONFIG.md`，部署：`docs/DEPLOY.md`
- 设计决策：`docs/DESIGN_DECISIONS.md`，对照清单：`docs/INVENTORY.md`

## 项目概述

本项目是一个高性能的数据爬虫代理系统，具有以下特点：

 - uTLS 指纹伪装（h2 优先，h1 回退，std 兜底）
 - 通过 gRPC 提供 RPC 服务接口
 - DNS IP 池（A/AAAA、轮询、403 拉黑、200 解封、自动刷新）
 - 自动网络能力（IPv4/IPv6）与 IPv6 源地址绑定
 - 仅共享观测报告，各自验证黑白名单
 - 种子自举（Peer seeds），自动扩散学习对等（PeerService 规划中）
 - 零配置 TLS（自签证书自动生成/加载）

## 技术架构

```
RPC Client -> gRPC/QUIC Server -> UTLS Client -> Target
                          |
                          v
                         DNS Pool
```

## 主要功能

1. **TLS 指纹伪装**：支持多种浏览器 TLS 指纹模拟
2. **QUIC 协议支持**：低延迟、多路复用
3. **RPC 服务接口**：单次/批量抓取接口
4. **高性能爬虫**：并发、超时、重试可配
5. **外部配置管理**：`config.toml`；Header 完全外部化

## 项目结构

```
.
├── config.toml
├── main.go
├── rpc/
├── server/
├── proxy/
├── config/
└── docs/
```

## 配置文件

- 见 `docs/CONFIG.md`。注意：`dns.blacklist_duration` 为兼容字段，当前实现采用“无过期黑名单”。

## 安装依赖

```bash
go mod tidy
```

## 生成 protobuf 代码

```bash
# 安装工具
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
# 生成服务与管理接口
protoc --go_out=. --go-grpc_out=. rpc/service.proto
protoc --go_out=. --go-grpc_out=. rpc/admin_service.proto
```

## 运行服务

```bash
go run main.go
```

服务监听 `0.0.0.0:4242`，TLS 于 `data/tls/` 自动生成/加载。

## 测试

- 单 URL：`go build -o single_fetch ./cmd/single_fetch && ./single_fetch`
- DNS 探测：`go build -o dns_probe ./cmd/dns_probe && ./dns_probe --domain example.com`
- 集成测试：`go test ./integration_test -v`

## FAQ

- Q: 绑定地址能否配置？
  - A: 目前固定 `0.0.0.0`，便于部署与反向代理对接。
- Q: 如何关闭 DNS 直连？
  - A: 设置 `dns.enabled = false`。
- Q: `blacklist_duration` 是否生效？
  - A: 兼容字段；当前实现为“无过期黑名单”。


