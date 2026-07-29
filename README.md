# LangBot New API

> [!IMPORTANT]
> 本仓库是 **LangBot 团队基于
> [New API](https://github.com/QuantumNous/new-api) 改造和维护的派生版本**，
> 并非 New API 官方发行版。
>
> This repository is a **LangBot-maintained derivative of
> [New API](https://github.com/QuantumNous/new-api)**. It is not an official
> New API distribution.

LangBot New API 保留了 New API 的统一大模型网关、渠道管理、计费、用户管理和
管理控制台等核心能力，并围绕 [LangBot](https://github.com/langbot-app/LangBot) /
LangBot Space 的模型目录与运营需求进行定制。

## 与上游的关系

- **上游项目：** [QuantumNous/new-api](https://github.com/QuantumNous/new-api)
- **本仓库：** [langbot-app/langbot-new-api](https://github.com/langbot-app/langbot-new-api)
- **维护方式：** 在 New API 基础上持续同步上游变更，并保留 LangBot 所需的定制契约
- **兼容性说明：** 本仓库不会保证与上游每个提交实时同步；部署和升级前请先核对本仓库的变更与数据迁移要求

上游项目提供了本项目的主要架构、模型适配、管理界面与部署基础。感谢 New API 及其所有贡献者。

**Frontend design and development by New API contributors.**

## LangBot 定制内容

相较于上游 New API，本仓库主要包含以下 LangBot 场景改造：

- 扩展模型元数据，包括 UUID、分类、标签、供应商、展示图标、精选状态与排序
- 向模型定价接口发布 LangBot Space 所需的模型能力和端点信息
- 补充 Rerank 模型目录及 Cohere Rerank 端点契约
- 增强管理端额度调整的一致性、持久化与审计能力
- 保留 LangBot 部署所需的构建与发布流程

具体行为以当前代码和测试为准。通用 New API 能力及接口说明请参考[上游文档](https://docs.newapi.pro/)。

## 快速开始

### 从源码构建

```bash
git clone https://github.com/langbot-app/langbot-new-api.git
cd langbot-new-api
docker build -t langbot-new-api:local .
```

使用默认 SQLite 数据库启动本地实例：

```bash
mkdir -p data logs
docker run --name langbot-new-api \
  -d --restart unless-stopped \
  -p 3000:3000 \
  -e TZ=Asia/Shanghai \
  -v "$(pwd)/data:/data" \
  -v "$(pwd)/logs:/app/logs" \
  langbot-new-api:local \
  --log-dir /app/logs
```

启动后访问 `http://localhost:3000`。

> 生产环境应使用 PostgreSQL 或 MySQL，并配置 Redis、强随机会话密钥、HTTPS 与可信代理。不要直接使用仓库示例中的默认密码。

### Docker Compose

仓库内的 `docker-compose.yml` 继承自上游，默认引用上游镜像
`calciumion/new-api`。如需运行本仓库的定制版本，请先构建本地镜像，并将
Compose 中的 `image` 改为 `langbot-new-api:local`，或改用你自行发布的镜像地址。

更多部署参数、渠道配置和 API 使用方式请查看：

- [New API 官方文档](https://docs.newapi.pro/)
- [环境变量示例](./.env.example)
- [Docker Compose 示例](./docker-compose.yml)

## 开发

主要技术栈：

- 后端：Go
- 前端：React、Rsbuild
- 数据库：SQLite、PostgreSQL 或 MySQL
- 缓存：Redis（可选，生产环境推荐）

常用检查：

```bash
# 后端测试
go test ./...

# 前端检查（本仓库使用 Bun）
cd web
bun install --frozen-lockfile
bun run lint
bun run build
```

提交改动前，请阅读 [AGENTS.md](./AGENTS.md) 中的仓库约定。

## 合规使用

本项目仅应用于合法、已授权的大模型 API 网关、模型管理、用量统计和私有部署
场景。使用者应合法取得上游模型服务和接口权限，遵守相应服务条款及所在司法
辖区的法律法规。面向公众提供生成式 AI 服务时，使用者应自行完成适用的备案、
许可、内容安全、实名、日志、税务及上游授权义务。

## 安全与隐私

- 不要提交真实 API Key、数据库 DSN、OAuth Secret、Webhook、私钥或生产环境配置
- 本地环境变量应写入被 Git 忽略的 `.env` / `.env.*` 文件；仓库只保留脱敏示例
- 公开日志或问题报告前，请移除请求头、渠道密钥、用户数据和内部地址
- 如果发现安全问题，请优先通过 GitHub Security Advisory 私下报告，不要在公开 Issue 中披露可利用细节

## 上游同步

本仓库保留 `upstream` 远程指向 New API。维护者同步时应先审查上游的许可证、数据库迁移、接口变化及其与 LangBot 定制契约的冲突，再合并到本仓库。

示例：

```bash
git remote add upstream https://github.com/QuantumNous/new-api.git
git fetch upstream
git merge upstream/main
```

请勿在未验证 LangBot 定制行为的情况下直接覆盖本仓库分支。

## 许可证与署名

本项目基于 New API 修改，并依照 **GNU Affero General Public License v3.0（AGPL-3.0）** 发布。

使用、修改、部署或分发本项目时，请完整阅读并遵守：

- [LICENSE](./LICENSE)
- [NOTICE](./NOTICE)
- [THIRD-PARTY-LICENSES.md](./THIRD-PARTY-LICENSES.md)

根据现有 `NOTICE`，修改版本必须保留 New API 的合理法律声明、作者署名以及指向
[原始项目](https://github.com/QuantumNous/new-api)的可见链接，并明确标记修改
来源。通过网络提供修改版本服务时，还需遵守 AGPL-3.0 关于提供对应源代码的
要求。

---

- LangBot: <https://github.com/langbot-app/LangBot>
- New API upstream: <https://github.com/QuantumNous/new-api>
