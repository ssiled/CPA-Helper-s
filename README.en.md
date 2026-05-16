# CPA Helper

English | [中文](README.md)

[![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![Vue](https://img.shields.io/badge/Vue-3.5+-42b883?logo=vuedotjs&logoColor=white)](https://vuejs.org/)
[![Vite](https://img.shields.io/badge/Vite-5.4+-646cff?logo=vite&logoColor=white)](https://vitejs.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
<a href="https://linux.do" alt="LINUX DO"><img src="https://shorturl.at/ggSqS" /></a>

CPA Helper is a local self-hosted multi-user administration panel for CLIProxyAPI / CPA users. It centralizes usage analytics, request records, user accounts, API keys, model pricing, available models and Codex auth file inspection.

It separates API keys and usage data by user: each user can create and manage their own keys and inspect their own requests, tokens and cost statistics, while administrators can create or disable regular accounts and review global plus per-user usage. It is built with Go, SQLite, Vue 3 and Vite, with runtime data stored in the root-level `data/` directory by default.

For clarity, model requests initiated by an Agent are still sent directly from that Agent to CPA. CPA Helper does not proxy or relay those requests; it only calls CPA management-style interfaces such as the usage queue, API key creation and deletion, and credential management for usage views, key management and credential maintenance.

## Table of Contents

- [Features](#features)
- [Screenshots](#screenshots)
- [Tech Stack](#tech-stack)
- [Project Structure](#project-structure)
- [Requirements](#requirements)
- [Quick Start](#quick-start)
- [Configuration](#configuration)
- [Development and Checks](#development-and-checks)
- [Contributing](#contributing)
- [Acknowledgements](#acknowledgements)
- [License](#license)

## Features

- **Usage analytics and cost estimation**: Track requests, tokens, success rate, latency, model distribution and estimated cost from global, per-user and current-account views.
- **Request record tracing**: Admins can filter global request events by time, user, API key description, provider, model, endpoint and failure state; regular users inspect only their own account records.
- **User and permission management**: Provide administrator and regular-user views; admins can create or disable regular accounts and manage nicknames, login accounts, passwords and roles.
- **API key lifecycle management**: Each user can independently create, edit, copy and delete their own API keys and synchronize them to CPA, with usage counted and displayed per user.
- **Model pricing maintenance**: Maintain input, output, cached and reasoning prices in USD per million tokens; historical costs are recalculated with current prices.
- **Available model aggregation**: Query available models through the current account's bound CPA API keys and enrich them with local pricing data.
- **CLIProxyAPI / CPAMC integration**: Configure the service URL, management key, usage queue and local collector options to persist remote usage events into SQLite.
- **Codex auth file inspection**: Support Cron scheduling, quota thresholds, check-only mode, concurrent workers, priority rules, account enable/disable and deletion.
- **Local-first data storage**: Use SQLite and the `data/` directory by default, with `CPA_HELPER_DATA_DIR` available for overriding the runtime data path.
- **Modern admin interface**: Built with Vue 3, Naive UI, ECharts and lucide icons, with light, dark and system theme modes.

## Screenshots

### Admin

**Usage dashboard**

Admins can inspect global request volume, tokens, cost, trends and distributions by time range, user, model and endpoint.

![Usage dashboard](pictures/历史用量.png)

**Request records**

Admins can filter global request events, while regular users can inspect records scoped to their own account, with drawer-based detail views.

![Request records](pictures/请求明细.png)

**User management**

Admins can create or disable regular accounts, manage nicknames, roles and enabled status, and review per-user daily usage summaries.

![User management](pictures/用户管理.png)

**Model pricing**

Maintain model pricing and recalculate historical request costs using the latest configured prices.

![Model pricing](pictures/模型价格.png)

**System settings**

Configure the CLIProxyAPI / CPAMC endpoint, management key, local collector and polling options.

![System settings](pictures/系统设置.png)

### Account Inspection

**Inspection settings**

Configure Codex auth file inspection with Cron schedules, quota thresholds, timeouts, retries, worker count and priority rules.

![Inspection settings](pictures/巡检设置.png)

**Account status**

Review auth file health, quota windows, account types, priorities and the latest inspection actions.

![Account status](pictures/账号状态.png)

### Account Views

**My usage**

Each user can review their own requests, tokens, costs, trends and model usage.

![My usage](pictures/我的账户.png)

**My records**

Each user can inspect request events and details scoped to their own account, separated from other users.

![My records](pictures/我的明细.png)

**API keys**

Each account can independently create and manage its own API keys and review daily request, token and cost summaries.

![API keys](pictures/API密钥.png)

**Available models**

Query available models through bound CPA API keys and display source keys with pricing information.

![Available models](pictures/可用模型.png)

**Account settings**

View the current signed-in account and update the password.

![Account settings](pictures/账户设置.png)

## Tech Stack

- **Backend**: Go standard-library HTTP server, SQLite, robfig/cron and modernc.org/sqlite.
- **Frontend**: Vue 3, Vite, TypeScript, Vue Router, Naive UI, ECharts and lucide-vue-next.
- **Runtime data**: Stored in root-level `data/` by default; the SQLite database is `data/db/cpa_helper.sqlite3`.
- **API shape**: The backend exposes `/api/*`; the Vite development server proxies API calls to `http://127.0.0.1:18317`.

## Project Structure

```text
CPA-Helper/
├── backend/                 # Go backend project
│   ├── cmd/cpa-helper/      # Application entrypoint
│   ├── internal/app/        # App composition, routes, business logic and database access
│   ├── internal/httpserver/ # HTTP server lifecycle and graceful shutdown
│   ├── internal/platform/   # External-system infrastructure adapters
│   ├── internal/security/   # Password, session and API-key security helpers
│   ├── migrations/          # Embedded goose SQLite migrations
│   ├── go.mod
│   └── go.sum
├── frontend/                # Vue + Vite frontend project
│   ├── src/                 # App, feature modules, shared utilities and styles
│   ├── public/              # Static assets
│   └── package.json         # Frontend dependencies and scripts
├── pictures/                # README screenshots
├── docs/                    # Reference documentation
├── data/                    # Runtime data, ignored by Git by default
├── README.md
├── README.en.md
└── LICENSE
```

## Requirements

- Go 1.25 or newer.
- Node.js 20 or newer.
- npm.
- An accessible CLIProxyAPI / CPA service. The default URL is `http://127.0.0.1:8317`.

## Quick Start

### 1. Docker Compose deployment (recommended)

Create `docker-compose.yml` in the deployment directory:

```yaml
services:
  cpa-helper:
    image: walkingd/cpa-helper:latest
    container_name: cpa-helper
    restart: always
    # 如需改为bridge,需将容器内部端口 18317 映射至主机
    # 程序默认访问地址为 `http://127.0.0.1:18317`
    network_mode: host
    environment:
      - TZ=Asia/Shanghai
    volumes:
      - ./data:/app/data
```

Then pull the image and start the service:

```powershell
docker compose pull
docker compose up -d
```

Open:

```text
http://127.0.0.1:18317
```

On first visit, the application guides you through creating the first administrator account.

### 2. Clone the repository

```powershell
git clone <your-repo-url>
cd CPA-Helper
```

### 3. Start the backend

Run all backend commands from `backend/`:

```powershell
cd backend
go mod download
go run ./cmd/cpa-helper
```

For a local binary build, write the output to the ignored `backend/bin/` directory:

```powershell
go build -o bin/cpa-helper.exe ./cmd/cpa-helper
```

Health check:

```powershell
curl http://127.0.0.1:18317/api/health
```

Expected response:

```json
{"status":"ok"}
```

### 4. Start the frontend development server

Open a new terminal and run from `frontend/`:

```powershell
cd frontend
npm install
npm run dev
```

If a normal backend is already using `18317`, start the test backend on another
port and point the Vite proxy to it:

```powershell
$env:CPA_HELPER_PROXY_TARGET="http://127.0.0.1:18318"
npm run dev -- --host 127.0.0.1 --port 5174 --strictPort
```

Open:

```text
http://127.0.0.1:5173
```

On first visit, the application guides you through creating the first administrator account.

### 5. Single-service preview or deployment

To let the Go backend serve the frontend static files, build the frontend first:

```powershell
cd frontend
npm install
npm run build
```

Then start the backend:

```powershell
cd ../backend
go run ./cmd/cpa-helper
```

Open:

```text
http://127.0.0.1:18317
```

When `frontend/dist` exists, the backend serves the built single-page application.

## Configuration

### CLIProxyAPI / CPAMC

Use the System Settings page to configure:

- **CLIProxyAPI / CPAMC URL**: defaults to `http://127.0.0.1:8317`.
- **Management key**: used to access the CLIProxyAPI Management API.
- **Enable local collector**: when enabled, CPA Helper reads events from the usage queue and writes them to the local database.
- **Batch size, polling interval and retry interval**: control local collector throughput and failure retry behavior.

### Data Directory

Default runtime data directory:

```text
data/
```

Default SQLite database:

```text
data/db/cpa_helper.sqlite3
```

Override the runtime data directory with:

```powershell
$env:CPA_HELPER_DATA_DIR="<your-data-dir>"
```

Then start the backend service.

### Account Inspection

The Inspection Settings page manages Codex auth files:

- Cron expressions define the automatic inspection schedule.
- Quota thresholds decide when account priority should be degraded or restored.
- Check-only mode records planned actions without disabling accounts or changing priorities.
- Priority rules define default scheduling weights by account type.
- The Account Status page shows health, quota, latest inspection, enabled state and manual priority.

## Development and Checks

### Isolated Local Testing

If a normal service is already running locally, do not reuse its ports or real
data directory for tests. Start the backend with a temporary port and data
directory:

```powershell
cd backend
$env:CPA_HELPER_ADDR=":18318"
$env:CPA_HELPER_DATA_DIR="$env:TEMP\cpa-helper-test-data"
go run ./cmd/cpa-helper
```

Run the frontend test server on a separate port and point the Vite proxy at the
test backend:

```powershell
cd frontend
$env:CPA_HELPER_PROXY_TARGET="http://127.0.0.1:18318"
npm run dev -- --host 127.0.0.1 --port 5174 --strictPort
```

Automated validation should not use a real CPA URL or real management key by
default. For account inspection, enable/disable, priority changes, or deletion
flows, use a fake CLIProxyAPI / CPAMC test double and prefer check-only mode;
connect to real CPA only after the risk is explicitly accepted.

Backend:

```powershell
cd backend
go fmt ./...
go test ./...
```

Frontend:

```powershell
cd frontend
npm run lint
npm run build
```

Database schema:

```powershell
cd backend
go test ./...
```

The Go backend runs embedded goose SQLite migrations on startup; migration files live in `backend/migrations/`, and Alembic is no longer required.
For Docker upgrades, keep mounting the existing `data/db/cpa_helper.sqlite3`; migration logic is packaged into the Go binary and does not read the old source tree.

## Contributing

Issues and pull requests are welcome. Before submitting changes, please check:

- Backend passes `go fmt ./...` and `go test ./...`.
- Frontend passes `npm run lint` and `npm run build`.
- Relational schema changes add or update goose migrations under `backend/migrations/`.
- Local runtime data, virtual environments, build outputs and secrets are not committed.

## Acknowledgements

Thanks to the [Linux.do](https://linux.do/) site and community for support and inspiration around the project.

## License

This project is open-sourced under the [MIT License](LICENSE).
