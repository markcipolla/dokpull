# dokpull

A terminal UI for pulling the latest Docker images across all your [Dokploy](https://dokploy.com) services and automatically triggering redeployments when updates are found.

<img width="1152" height="862" alt="dokpull" src="https://github.com/user-attachments/assets/f32f0a6e-5ce5-4f76-8363-e1ae7d01a6d4" />


## Why

If you self-host with Dokploy like I do, you'll run many services. I use it for my homelab as well as for my servers, and keeping images up to date means clicking through each one individually. dokpull connects to the Dokploy API, discovers every compose stack and application, pulls all their images in parallel, and redeploys anything that changed — all from a single command.

## Features

- **Auto-discovery** — Fetches all projects, environments, compose stacks, and applications from the Dokploy API
- **Parallel pulls** — Pulls up to 3 images concurrently with live status per image
- **SHA tracking** — Shows before/after image digests so you can see exactly what changed
- **Auto-deploy** — Triggers Dokploy redeployments for services with updated images
- **Manual redeploy** — Select any service and press `Enter` to trigger a deploy on demand
- **Full-terminal TUI** — Scrollable table with keyboard navigation, progress bar, and spinner animations

## Requirements

- Go 1.25+
- Docker CLI available on `$PATH` (used for `docker pull` and `docker inspect`)
- A running [Dokploy](https://dokploy.com) instance with API access

## Installation

```sh
go install github.com/markcipolla/dokpull@latest
```

Or build from source:

```sh
git clone https://github.com/markcipolla/dokpull.git
cd dokpull
go build -o dokpull .
```

## Configuration

dokpull is configured via environment variables:

| Variable | Required | Description |
|---|---|---|
| `DOKPLOY_URL` | Yes | Base URL of your Dokploy instance (e.g. `https://dokploy.example.com`) |
| `DOKPLOY_API_KEY` | Yes | API key for authenticating with Dokploy |

```sh
export DOKPLOY_URL=https://dokploy.example.com
export DOKPLOY_API_KEY=your-api-key
```

## Usage

```sh
dokpull
```

This will:

1. Fetch all services from Dokploy
2. Match them to running Docker containers
3. Pull the latest image for each
4. Automatically redeploy any service whose image was updated

### Options

```
--deploy-all    Redeploy all services, even if their images haven't changed
-h, --help      Show help
```

### Keyboard Controls

| Key | Action |
|---|---|
| `↑` / `k` | Move cursor up |
| `↓` / `j` | Move cursor down |
| `PgUp` / `PgDn` | Jump 10 rows |
| `Home` / `End` | Jump to top / bottom |
| `Enter` | Trigger redeploy for selected service |
| `q` / `Esc` | Quit |

## How It Works

dokpull talks to two things: the **Dokploy API** and the local **Docker daemon**.

1. **Load services** — `GET /api/project.all` retrieves all projects and their compose/application services
2. **Match containers** — `docker ps` and `docker inspect` map running containers back to Dokploy services and capture current image digests
3. **Pull images** — `docker pull` runs for each image (3 at a time), streaming output to detect digest changes
4. **Deploy** — `POST /api/compose.deploy` triggers a redeployment for any service with an updated image

## Built With

- [Bubble Tea](https://github.com/charmbracelet/bubbletea) — TUI framework
- [Bubbles](https://github.com/charmbracelet/bubbles) — Spinner and progress bar components
- [Lip Gloss](https://github.com/charmbracelet/lipgloss) — Terminal styling
