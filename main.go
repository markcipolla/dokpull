package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Config
var (
	dokployURL    = getEnv("DOKPLOY_URL", "http://your-dokploy-server:3000")
	dokployAPIKey = getEnv("DOKPLOY_API_KEY", "your-api-key")
)

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// Styles
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("205")).
			MarginBottom(1)

	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("39")).
			Background(lipgloss.Color("236")).
			Padding(0, 1)

	cellStyle = lipgloss.NewStyle().
			Padding(0, 1)

	updatedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("82"))

	noUpdateStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245"))

	deployPendingStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("220")).
				Bold(true)

	deployTriggeredStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("82")).
				Bold(true)

	deployNoneStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196"))

	statusBarStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("236")).
			Foreground(lipgloss.Color("252")).
			Padding(0, 1).
			MarginTop(1)
)

// Image states
type PullStatus int

const (
	StatusPending PullStatus = iota
	StatusPulling
	StatusComplete
	StatusError
)

type DeployStatus int

const (
	DeployNone DeployStatus = iota
	DeployNeeded
	DeployTriggered
	DeployFailed
)

type ImageState struct {
	Name         string
	SHA          string
	OldSHA       string
	Progress     float64
	PullStatus   PullStatus
	DeployStatus DeployStatus
	Updated      bool
	Error        string
}

type DokployApp struct {
	ID    string `json:"applicationId"`
	Name  string `json:"name"`
	Image string `json:"dockerImage"`
}

type model struct {
	images       []ImageState
	apps         []DokployApp
	spinner      spinner.Model
	progress     progress.Model
	currentImage int
	phase        string // "pulling", "deploying", "done"
	err          error
	deployAll    bool
	width        int
	height       int
}

// Messages
type imagesLoadedMsg []ImageState
type appsLoadedMsg []DokployApp
type pullProgressMsg struct {
	index    int
	progress float64
	sha      string
}
type pullCompleteMsg struct {
	index   int
	updated bool
	sha     string
}
type pullErrorMsg struct {
	index int
	err   error
}
type deployCompleteMsg struct {
	index   int
	success bool
}
type allDoneMsg struct{}

func initialModel(deployAll bool) model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	p := progress.New(progress.WithDefaultGradient())

	return model{
		spinner:   s,
		progress:  p,
		phase:     "loading",
		deployAll: deployAll,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		loadImages,
		loadApps,
	)
}

func loadImages() tea.Msg {
	cmd := exec.Command("docker", "images", "--format", "{{.Repository}}:{{.Tag}}|{{.ID}}")
	output, err := cmd.Output()
	if err != nil {
		return pullErrorMsg{index: -1, err: err}
	}

	var images []ImageState
	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "<none>") {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) == 2 {
			images = append(images, ImageState{
				Name:       parts[0],
				OldSHA:     parts[1],
				SHA:        parts[1],
				PullStatus: StatusPending,
			})
		}
	}

	// Sort by name
	sort.Slice(images, func(i, j int) bool {
		return images[i].Name < images[j].Name
	})

	return imagesLoadedMsg(images)
}

func loadApps() tea.Msg {
	if dokployAPIKey == "" {
		return appsLoadedMsg(nil)
	}

	req, err := http.NewRequest("GET", dokployURL+"/api/project.all", nil)
	if err != nil {
		return appsLoadedMsg(nil)
	}
	req.Header.Set("x-api-key", dokployAPIKey)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return appsLoadedMsg(nil)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var projects []struct {
		Applications []DokployApp `json:"applications"`
	}
	if err := json.Unmarshal(body, &projects); err != nil {
		return appsLoadedMsg(nil)
	}

	var apps []DokployApp
	for _, p := range projects {
		apps = append(apps, p.Applications...)
	}
	return appsLoadedMsg(apps)
}

func pullImage(index int, imageName string) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("docker", "pull", imageName)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return pullErrorMsg{index: index, err: err}
		}
		cmd.Stderr = cmd.Stdout

		if err := cmd.Start(); err != nil {
			return pullErrorMsg{index: index, err: err}
		}

		scanner := bufio.NewScanner(stdout)
		var lastSHA string
		updated := false

		// Regex to match progress output
		progressRegex := regexp.MustCompile(`(\d+(?:\.\d+)?)\s*[%]`)
		shaRegex := regexp.MustCompile(`Digest:\s*sha256:([a-f0-9]+)`)

		for scanner.Scan() {
			line := scanner.Text()

			// Check for SHA
			if matches := shaRegex.FindStringSubmatch(line); len(matches) > 1 {
				lastSHA = matches[1][:12]
			}

			// Check for update indicators
			if strings.Contains(line, "Pull complete") || strings.Contains(line, "Downloaded newer") {
				updated = true
			}

			// Check for progress
			if matches := progressRegex.FindStringSubmatch(line); len(matches) > 1 {
				if pct, err := strconv.ParseFloat(matches[1], 64); err == nil {
					// This is rough - we get per-layer progress
					_ = pct // We'll use a simpler approach
				}
			}
		}

		cmd.Wait()

		return pullCompleteMsg{
			index:   index,
			updated: updated,
			sha:     lastSHA,
		}
	}
}

func triggerDeploy(index int, appID string) tea.Cmd {
	return func() tea.Msg {
		payload := fmt.Sprintf(`{"applicationId":"%s"}`, appID)
		req, err := http.NewRequest("POST", dokployURL+"/api/application.deploy",
			bytes.NewBufferString(payload))
		if err != nil {
			return deployCompleteMsg{index: index, success: false}
		}
		req.Header.Set("x-api-key", dokployAPIKey)
		req.Header.Set("Content-Type", "application/json")

		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return deployCompleteMsg{index: index, success: false}
		}
		defer resp.Body.Close()

		return deployCompleteMsg{index: index, success: resp.StatusCode < 400}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)

	case imagesLoadedMsg:
		m.images = msg
		if len(m.images) > 0 && m.phase == "loading" {
			m.phase = "pulling"
			m.currentImage = 0
			// Start pulling all images concurrently (up to 3 at a time)
			for i := 0; i < min(3, len(m.images)); i++ {
				m.images[i].PullStatus = StatusPulling
				cmds = append(cmds, pullImage(i, m.images[i].Name))
			}
		}

	case appsLoadedMsg:
		m.apps = msg

	case pullCompleteMsg:
		if msg.index >= 0 && msg.index < len(m.images) {
			m.images[msg.index].PullStatus = StatusComplete
			m.images[msg.index].Updated = msg.updated
			m.images[msg.index].Progress = 1.0
			if msg.sha != "" {
				m.images[msg.index].SHA = msg.sha
			}

			// Mark deploy needed if updated
			if msg.updated {
				m.images[msg.index].DeployStatus = DeployNeeded
			}

			// Start next image if any pending
			for i := range m.images {
				if m.images[i].PullStatus == StatusPending {
					m.images[i].PullStatus = StatusPulling
					cmds = append(cmds, pullImage(i, m.images[i].Name))
					break
				}
			}

			// Check if all done pulling
			allPulled := true
			for _, img := range m.images {
				if img.PullStatus != StatusComplete && img.PullStatus != StatusError {
					allPulled = false
					break
				}
			}

			if allPulled {
				m.phase = "deploying"
				cmds = append(cmds, m.startDeploys())
			}
		}

	case pullErrorMsg:
		if msg.index >= 0 && msg.index < len(m.images) {
			m.images[msg.index].PullStatus = StatusError
			m.images[msg.index].Error = msg.err.Error()
		}

	case deployCompleteMsg:
		if msg.index >= 0 && msg.index < len(m.images) {
			if msg.success {
				m.images[msg.index].DeployStatus = DeployTriggered
			} else {
				m.images[msg.index].DeployStatus = DeployFailed
			}
		}

		// Check if all deploys done
		allDeployed := true
		for _, img := range m.images {
			if img.DeployStatus == DeployNeeded {
				allDeployed = false
				break
			}
		}
		if allDeployed && m.phase == "deploying" {
			m.phase = "done"
		}

	case allDoneMsg:
		m.phase = "done"
	}

	return m, tea.Batch(cmds...)
}

func (m model) startDeploys() tea.Cmd {
	var cmds []tea.Cmd

	for i := range m.images {
		if m.deployAll || m.images[i].Updated {
			// Find matching app
			for _, app := range m.apps {
				imgBase := strings.Split(m.images[i].Name, ":")[0]
				appBase := strings.Split(app.Image, ":")[0]
				if imgBase == appBase || m.images[i].Name == app.Image {
					m.images[i].DeployStatus = DeployNeeded
					cmds = append(cmds, triggerDeploy(i, app.ID))
					break
				}
			}
		}
	}

	if len(cmds) == 0 {
		return func() tea.Msg { return allDoneMsg{} }
	}

	return tea.Batch(cmds...)
}

func (m model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	var b strings.Builder

	// Title
	b.WriteString(titleStyle.Render("🐳 Docker Image Updater"))
	b.WriteString("\n\n")

	// Table header
	colWidths := []int{45, 14, 22, 15}
	headers := []string{"Image", "SHA", "Progress", "Deploy"}

	var headerRow strings.Builder
	for i, h := range headers {
		headerRow.WriteString(headerStyle.Width(colWidths[i]).Render(h))
	}
	b.WriteString(headerRow.String())
	b.WriteString("\n")

	// Table rows
	for _, img := range m.images {
		var row strings.Builder

		// Image name (truncate if needed)
		name := img.Name
		if len(name) > colWidths[0]-2 {
			name = name[:colWidths[0]-5] + "..."
		}
		row.WriteString(cellStyle.Width(colWidths[0]).Render(name))

		// SHA
		sha := img.SHA
		if len(sha) > 12 {
			sha = sha[:12]
		}
		if img.Updated && img.OldSHA != img.SHA {
			row.WriteString(updatedStyle.Width(colWidths[1]).Render(sha))
		} else {
			row.WriteString(noUpdateStyle.Width(colWidths[1]).Render(sha))
		}

		// Progress
		var progressStr string
		switch img.PullStatus {
		case StatusPending:
			progressStr = noUpdateStyle.Render("waiting...")
		case StatusPulling:
			progressStr = m.spinner.View() + " pulling..."
		case StatusComplete:
			if img.Updated {
				progressStr = updatedStyle.Render("✓ updated")
			} else {
				progressStr = noUpdateStyle.Render("✓ up to date")
			}
		case StatusError:
			progressStr = errorStyle.Render("✗ error")
		}
		row.WriteString(cellStyle.Width(colWidths[2]).Render(progressStr))

		// Deploy status
		var deployStr string
		switch img.DeployStatus {
		case DeployNone:
			deployStr = deployNoneStyle.Render("-")
		case DeployNeeded:
			deployStr = deployPendingStyle.Render("⏳ pending")
		case DeployTriggered:
			deployStr = deployTriggeredStyle.Render("✓ triggered")
		case DeployFailed:
			deployStr = errorStyle.Render("✗ failed")
		}
		row.WriteString(cellStyle.Width(colWidths[3]).Render(deployStr))

		b.WriteString(row.String())
		b.WriteString("\n")
	}

	// Status bar
	var status string
	switch m.phase {
	case "loading":
		status = m.spinner.View() + " Loading images..."
	case "pulling":
		pulling := 0
		complete := 0
		for _, img := range m.images {
			if img.PullStatus == StatusPulling {
				pulling++
			}
			if img.PullStatus == StatusComplete {
				complete++
			}
		}
		status = fmt.Sprintf("%s Pulling images... (%d/%d complete, %d in progress)",
			m.spinner.View(), complete, len(m.images), pulling)
	case "deploying":
		status = m.spinner.View() + " Triggering deployments..."
	case "done":
		updated := 0
		deployed := 0
		for _, img := range m.images {
			if img.Updated {
				updated++
			}
			if img.DeployStatus == DeployTriggered {
				deployed++
			}
		}
		status = fmt.Sprintf("✓ Done! %d images updated, %d deployments triggered. Press q to quit.",
			updated, deployed)
	}
	b.WriteString(statusBarStyle.Width(m.width).Render(status))

	return b.String()
}

func main() {
	deployAll := false
	for _, arg := range os.Args[1:] {
		if arg == "--deploy-all" {
			deployAll = true
		}
		if arg == "-h" || arg == "--help" {
			fmt.Println("Usage: dokpull [OPTIONS]")
			fmt.Println()
			fmt.Println("A TUI for pulling Docker images and triggering Dokploy deployments.")
			fmt.Println()
			fmt.Println("Options:")
			fmt.Println("  --deploy-all    Redeploy all applications regardless of image updates")
			fmt.Println("  -h, --help      Show this help message")
			fmt.Println()
			fmt.Println("Environment variables:")
			fmt.Println("  DOKPLOY_URL      Dokploy instance URL (default: http://macpro:3000)")
			fmt.Println("  DOKPLOY_API_KEY  Dokploy API key (required for deployments)")
			os.Exit(0)
		}
	}

	p := tea.NewProgram(initialModel(deployAll), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}
