# AGENTS.md - dokpull

## Project Overview

**dokpull** is a terminal UI (TUI) application written in **Go** that pulls the latest Docker images and triggers redeployments on [Dokploy](https://dokploy.com).

## Architecture

Single-file Go application (`main.go`, ~596 lines) using the **Bubble Tea** MVU (Model-Update-View) framework from Charm Bracelet.

### Key Dependencies

- `github.com/charmbracelet/bubbletea` - TUI framework / event loop
- `github.com/charmbracelet/bubbles` - UI components (spinner, progress bar)
- `github.com/charmbracelet/lipgloss` - Terminal styling and layout

### Application Flow

1. **Loading** - Loads local Docker images via `docker images` and fetches Dokploy apps via API
2. **Pulling** - Pulls all images concurrently (up to 3 at a time), tracking progress
3. **Deploying** - Matches updated images to Dokploy apps and triggers redeployments via API
4. **Done** - Displays summary of updates and deployments

### Key Types

- `ImageState` - Tracks each Docker image's name, SHA, pull status, and deploy status
- `DokployApp` - Represents a Dokploy application (ID, name, Docker image)
- `model` - Main application state (Bubble Tea model)

### Configuration

- **`DOKPLOY_URL`** - Dokploy instance URL (env var)
- **`DOKPLOY_API_KEY`** - Dokploy API key (env var)
- **`--deploy-all`** - CLI flag to redeploy all apps regardless of image updates

## Building

Requires Go 1.25.3+.

```bash
# Build for current platform
go build -o dokpull .

# Cross-compile
GOOS=linux GOARCH=amd64 go build -o dokpull-linux .
GOOS=darwin GOARCH=arm64 go build -o dokpull-macos .
```

## Running

```bash
DOKPLOY_URL=http://your-server:3000 DOKPLOY_API_KEY=your-key ./dokpull
```

Requires Docker to be available on the host (uses `docker images` and `docker pull` commands).

## File Structure

```
dokpull/
  main.go       - Entire application source
  go.mod        - Module definition and Go version
  go.sum        - Dependency checksums
  README.md     - Brief project description
  AGENTS.md     - This file
```
