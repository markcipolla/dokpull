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
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Config
var (
	dokployURL    = os.Getenv("DOKPLOY_URL")
	dokployAPIKey = os.Getenv("DOKPLOY_API_KEY")
)

// Styles
var (
	borderColor    = lipgloss.Color("63")
	borderStyle    = lipgloss.NewStyle().Foreground(borderColor)
	headerBg       = lipgloss.Color("236")
	headerFg       = lipgloss.Color("39")
	serviceFg      = lipgloss.Color("252")
	imageFg        = lipgloss.Color("245")
	imageUpdatedFg = lipgloss.Color("82")
	shaChangedFg   = lipgloss.Color("220")
	selectedBg     = lipgloss.Color("57")
	selectedFg     = lipgloss.Color("229")
	scrollTrackFg  = lipgloss.Color("238")
	scrollThumbFg  = lipgloss.Color("63")
)

// Pull/Deploy status enums
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

type ImageInfo struct {
	Image      string
	Container  string
	PullStatus PullStatus
	Updated    bool
	OldSHA     string
	NewSHA     string
	Error      string
}

type ServiceState struct {
	ComposeID    string
	Name         string
	AppName      string
	Images       []ImageInfo
	DeployStatus DeployStatus
}

type model struct {
	services     []ServiceState
	spinner      spinner.Model
	progress     progress.Model
	phase        string // "loading", "pulling", "deploying", "done"
	err          error
	deployAll    bool
	width        int
	height       int
	scrollOffset int
	cursor       int
}

// A displayRow is a service header, image row, or separator between services.
type displayRow struct {
	serviceIdx int
	imageIdx   int // -1 = service header, -2 = separator
}

// Messages
type servicesLoadedMsg struct {
	services []ServiceState
	err      error
}
type pullCompleteMsg struct {
	serviceIdx int
	imageIdx   int
	updated    bool
	sha        string
}
type pullErrorMsg struct {
	serviceIdx int
	imageIdx   int
	err        error
}
type deployCompleteMsg struct {
	serviceIdx int
	success    bool
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
		loadServices,
	)
}

// buildRows creates the flat display row list from services.
func (m model) buildRows() []displayRow {
	var rows []displayRow
	for si, svc := range m.services {
		if si > 0 {
			rows = append(rows, displayRow{serviceIdx: si, imageIdx: -2}) // separator
		}
		rows = append(rows, displayRow{serviceIdx: si, imageIdx: -1})
		for ii := range svc.Images {
			rows = append(rows, displayRow{serviceIdx: si, imageIdx: ii})
		}
	}
	return rows
}

// loadServices fetches compose services from Dokploy and resolves their
// running container images via docker inspect.
func loadServices() tea.Msg {
	req, err := http.NewRequest("GET", dokployURL+"/api/project.all", nil)
	if err != nil {
		return servicesLoadedMsg{err: fmt.Errorf("building request: %w", err)}
	}
	req.Header.Set("x-api-key", dokployAPIKey)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return servicesLoadedMsg{err: fmt.Errorf("connecting to %s: %w", dokployURL, err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return servicesLoadedMsg{err: fmt.Errorf("%s returned HTTP %d", dokployURL, resp.StatusCode)}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return servicesLoadedMsg{err: fmt.Errorf("reading response: %w", err)}
	}

	var projects []struct {
		Environments []struct {
			Compose []struct {
				ComposeID string `json:"composeId"`
				Name      string `json:"name"`
				AppName   string `json:"appName"`
			} `json:"compose"`
			Applications []struct {
				ApplicationID string `json:"applicationId"`
				Name          string `json:"name"`
				AppName       string `json:"appName"`
				DockerImage   string `json:"dockerImage"`
			} `json:"applications"`
		} `json:"environments"`
	}
	if err := json.Unmarshal(body, &projects); err != nil {
		return servicesLoadedMsg{err: fmt.Errorf("parsing response: %w", err)}
	}

	var services []ServiceState
	for _, proj := range projects {
		for _, env := range proj.Environments {
			for _, comp := range env.Compose {
				services = append(services, ServiceState{
					ComposeID: comp.ComposeID,
					Name:      comp.Name,
					AppName:   comp.AppName,
				})
			}
			for _, app := range env.Applications {
				svc := ServiceState{
					ComposeID: app.ApplicationID,
					Name:      app.Name,
					AppName:   app.AppName,
				}
				if app.DockerImage != "" {
					svc.Images = []ImageInfo{{Image: app.DockerImage, PullStatus: StatusPending}}
				}
				services = append(services, svc)
			}
		}
	}

	if len(services) == 0 {
		return servicesLoadedMsg{err: fmt.Errorf("no services found in Dokploy")}
	}

	// Fetch appName for compose services that don't have one
	var wg sync.WaitGroup
	var mu sync.Mutex
	for i := range services {
		if services[i].AppName != "" {
			continue
		}
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			appName := fetchComposeAppName(services[idx].ComposeID)
			if appName != "" {
				mu.Lock()
				services[idx].AppName = appName
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()

	// Get running container images and match to services
	cmd := exec.Command("docker", "ps", "--format", "{{.Names}}\t{{.Label \"com.docker.compose.project\"}}\t{{.ID}}")
	out, err := cmd.Output()
	if err == nil {
		containersByProject := make(map[string][]string)
		scanner := bufio.NewScanner(bytes.NewReader(out))
		for scanner.Scan() {
			parts := strings.Split(scanner.Text(), "\t")
			if len(parts) == 3 && parts[1] != "" {
				containersByProject[parts[1]] = append(containersByProject[parts[1]], parts[2])
			}
		}

		for i, svc := range services {
			containerIDs, ok := containersByProject[svc.AppName]
			if !ok || len(containerIDs) == 0 {
				continue
			}
			args := append([]string{"inspect", "--format", "{{.Name}}\t{{.Config.Image}}"}, containerIDs...)
			inspectCmd := exec.Command("docker", args...)
			inspectOut, err := inspectCmd.Output()
			if err != nil {
				continue
			}
			var images []ImageInfo
			seen := make(map[string]bool)
			inspectScanner := bufio.NewScanner(bytes.NewReader(inspectOut))
			for inspectScanner.Scan() {
				parts := strings.Split(inspectScanner.Text(), "\t")
				if len(parts) == 2 {
					img := parts[1]
					if idx := strings.Index(img, "@"); idx != -1 {
						img = img[:idx]
					}
					if !seen[img] {
						seen[img] = true
						container := strings.TrimPrefix(parts[0], "/")
						// Get current image digest
						oldSHA := resolveImageSHA(img)
						images = append(images, ImageInfo{
							Image:      img,
							Container:  container,
							PullStatus: StatusPending,
							OldSHA:     oldSHA,
						})
					}
				}
			}
			if len(images) > 0 {
				services[i].Images = images
			}
		}
	}

	sort.Slice(services, func(i, j int) bool {
		return services[i].Name < services[j].Name
	})

	return servicesLoadedMsg{services: services}
}

// resolveImageSHA gets the short digest for a local Docker image.
func resolveImageSHA(image string) string {
	cmd := exec.Command("docker", "image", "inspect", "--format", "{{index .RepoDigests 0}}", image)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	digest := strings.TrimSpace(string(out))
	// Extract sha256:abc... from image@sha256:abc...
	if idx := strings.Index(digest, "sha256:"); idx != -1 {
		sha := digest[idx+7:]
		if len(sha) > 12 {
			sha = sha[:12]
		}
		return sha
	}
	return ""
}

// fetchComposeAppName gets the appName for a compose service from the Dokploy API.
func fetchComposeAppName(composeID string) string {
	req, err := http.NewRequest("GET", dokployURL+"/api/compose.one?composeId="+composeID, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("x-api-key", dokployAPIKey)
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	var result struct {
		AppName string `json:"appName"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return ""
	}
	return result.AppName
}

func pullImage(serviceIdx, imageIdx int, imageName string) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("docker", "pull", imageName)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return pullErrorMsg{serviceIdx: serviceIdx, imageIdx: imageIdx, err: err}
		}
		cmd.Stderr = cmd.Stdout

		if err := cmd.Start(); err != nil {
			return pullErrorMsg{serviceIdx: serviceIdx, imageIdx: imageIdx, err: err}
		}

		updated := false
		var lastSHA string
		shaRegex := regexp.MustCompile(`Digest:\s*sha256:([a-f0-9]+)`)

		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			if matches := shaRegex.FindStringSubmatch(line); len(matches) > 1 {
				lastSHA = matches[1][:12]
			}
			if strings.Contains(line, "Pull complete") || strings.Contains(line, "Downloaded newer") {
				updated = true
			}
		}
		cmd.Wait()

		return pullCompleteMsg{
			serviceIdx: serviceIdx,
			imageIdx:   imageIdx,
			updated:    updated,
			sha:        lastSHA,
		}
	}
}

func triggerDeploy(serviceIdx int, composeID string) tea.Cmd {
	return func() tea.Msg {
		payload := fmt.Sprintf(`{"composeId":"%s"}`, composeID)
		req, err := http.NewRequest("POST", dokployURL+"/api/compose.deploy",
			bytes.NewBufferString(payload))
		if err != nil {
			return deployCompleteMsg{serviceIdx: serviceIdx, success: false}
		}
		req.Header.Set("x-api-key", dokployAPIKey)
		req.Header.Set("Content-Type", "application/json")

		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return deployCompleteMsg{serviceIdx: serviceIdx, success: false}
		}
		defer resp.Body.Close()

		return deployCompleteMsg{serviceIdx: serviceIdx, success: resp.StatusCode < 400}
	}
}

// countInFlight returns how many images are currently pulling.
func (m model) countInFlight() int {
	n := 0
	for _, svc := range m.services {
		for _, img := range svc.Images {
			if img.PullStatus == StatusPulling {
				n++
			}
		}
	}
	return n
}

// startNextPulls starts pulling pending images up to the concurrency limit.
func (m *model) startNextPulls(cmds *[]tea.Cmd) {
	maxConcurrent := 3
	inFlight := m.countInFlight()
	for si := range m.services {
		for ii := range m.services[si].Images {
			if inFlight >= maxConcurrent {
				return
			}
			if m.services[si].Images[ii].PullStatus == StatusPending {
				m.services[si].Images[ii].PullStatus = StatusPulling
				*cmds = append(*cmds, pullImage(si, ii, m.services[si].Images[ii].Image))
				inFlight++
			}
		}
	}
}

// allImagesDone checks if every image is complete or errored.
func (m model) allImagesDone() bool {
	for _, svc := range m.services {
		for _, img := range svc.Images {
			if img.PullStatus != StatusComplete && img.PullStatus != StatusError {
				return false
			}
		}
	}
	return true
}

// pullProgress returns 0.0-1.0 for how many images are done.
func (m model) pullProgress() float64 {
	total := 0
	complete := 0
	for _, svc := range m.services {
		for _, img := range svc.Images {
			total++
			if img.PullStatus == StatusComplete || img.PullStatus == StatusError {
				complete++
			}
		}
	}
	if total == 0 {
		return 0
	}
	return float64(complete) / float64(total)
}

func (m model) formatImageStatus(img ImageInfo) string {
	switch img.PullStatus {
	case StatusPending:
		return "◌ waiting"
	case StatusPulling:
		return m.spinner.View() + " pulling"
	case StatusComplete:
		if img.Updated {
			return "✓ updated"
		}
		return "✓ current"
	case StatusError:
		return "✗ error"
	}
	return ""
}

func formatSHA(img ImageInfo) string {
	if img.PullStatus == StatusComplete && img.NewSHA != "" {
		return img.NewSHA
	}
	if img.OldSHA != "" {
		return img.OldSHA
	}
	return ""
}

func formatDeployStatus(ds DeployStatus) string {
	switch ds {
	case DeployNone:
		return "─"
	case DeployNeeded:
		return "⏳ pending"
	case DeployTriggered:
		return "✓ triggered"
	case DeployFailed:
		return "✗ failed"
	}
	return "─"
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	rows := m.buildRows()

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				// Skip separator rows
				if m.cursor >= 0 && m.cursor < len(rows) && rows[m.cursor].imageIdx == -2 {
					m.cursor--
				}
			}
		case "down", "j":
			if m.cursor < len(rows)-1 {
				m.cursor++
				// Skip separator rows
				if m.cursor < len(rows) && rows[m.cursor].imageIdx == -2 {
					if m.cursor < len(rows)-1 {
						m.cursor++
					} else {
						m.cursor--
					}
				}
			}
		case "pgup":
			m.cursor -= 10
			if m.cursor < 0 {
				m.cursor = 0
			}
			if m.cursor < len(rows) && rows[m.cursor].imageIdx == -2 {
				m.cursor++
			}
		case "pgdown":
			m.cursor += 10
			if m.cursor >= len(rows) {
				m.cursor = len(rows) - 1
			}
			if m.cursor >= 0 && m.cursor < len(rows) && rows[m.cursor].imageIdx == -2 {
				m.cursor++
				if m.cursor >= len(rows) {
					m.cursor -= 2
				}
			}
		case "home":
			m.cursor = 0
		case "end":
			if len(rows) > 0 {
				m.cursor = len(rows) - 1
			}
		case "enter":
			if m.cursor >= 0 && m.cursor < len(rows) {
				si := rows[m.cursor].serviceIdx
				svc := m.services[si]
				if svc.DeployStatus != DeployNeeded {
					m.services[si].DeployStatus = DeployNeeded
					if m.phase == "done" {
						m.phase = "deploying"
					}
					return m, triggerDeploy(si, svc.ComposeID)
				}
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.progress.Width = m.width - 5 // status bar content area = Width - padding = (m.width-3) - 2

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)

	case progress.FrameMsg:
		progressModel, cmd := m.progress.Update(msg)
		m.progress = progressModel.(progress.Model)
		cmds = append(cmds, cmd)

	case servicesLoadedMsg:
		if msg.err != nil {
			m.err = msg.err
			m.phase = "done"
			return m, nil
		}
		m.services = msg.services
		if len(m.services) > 0 && m.phase == "loading" {
			// Check if any service has pullable images
			hasImages := false
			for _, svc := range m.services {
				if len(svc.Images) > 0 {
					hasImages = true
					break
				}
			}
			if !hasImages {
				m.phase = "done"
			} else {
				m.phase = "pulling"
				m.startNextPulls(&cmds)
				cmd := m.progress.SetPercent(0)
				cmds = append(cmds, cmd)
			}
		}

	case pullCompleteMsg:
		si, ii := msg.serviceIdx, msg.imageIdx
		if si >= 0 && si < len(m.services) && ii >= 0 && ii < len(m.services[si].Images) {
			m.services[si].Images[ii].PullStatus = StatusComplete
			m.services[si].Images[ii].Updated = msg.updated
			if msg.sha != "" {
				m.services[si].Images[ii].NewSHA = msg.sha
			}

			m.startNextPulls(&cmds)

			// Animate progress bar
			cmd := m.progress.SetPercent(m.pullProgress())
			cmds = append(cmds, cmd)

			if m.allImagesDone() {
				m.phase = "deploying"
				// Mark services with updated images for deploy
				for si := range m.services {
					hasUpdate := false
					for _, img := range m.services[si].Images {
						if img.Updated {
							hasUpdate = true
							break
						}
					}
					if m.deployAll && len(m.services[si].Images) > 0 {
						m.services[si].DeployStatus = DeployNeeded
					} else if hasUpdate {
						m.services[si].DeployStatus = DeployNeeded
					}
				}
				cmds = append(cmds, m.startDeploys())
			}
		}

	case pullErrorMsg:
		si, ii := msg.serviceIdx, msg.imageIdx
		if si >= 0 && si < len(m.services) && ii >= 0 && ii < len(m.services[si].Images) {
			m.services[si].Images[ii].PullStatus = StatusError
			m.services[si].Images[ii].Error = msg.err.Error()
		}
		m.startNextPulls(&cmds)

		cmd := m.progress.SetPercent(m.pullProgress())
		cmds = append(cmds, cmd)

		if m.allImagesDone() {
			m.phase = "deploying"
			cmds = append(cmds, m.startDeploys())
		}

	case deployCompleteMsg:
		si := msg.serviceIdx
		if si >= 0 && si < len(m.services) {
			if msg.success {
				m.services[si].DeployStatus = DeployTriggered
			} else {
				m.services[si].DeployStatus = DeployFailed
			}
		}
		allDeployed := true
		for _, svc := range m.services {
			if svc.DeployStatus == DeployNeeded {
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
	for i := range m.services {
		if m.services[i].DeployStatus == DeployNeeded {
			cmds = append(cmds, triggerDeploy(i, m.services[i].ComposeID))
		}
	}
	if len(cmds) == 0 {
		return func() tea.Msg { return allDoneMsg{} }
	}
	return tea.Batch(cmds...)
}

// ─────────────────────────────────────────────
// View rendering
// ─────────────────────────────────────────────

func renderCell(content string, w int, fg, bg lipgloss.Color) string {
	// Truncate content to fit within text area (w minus padding), using display width
	textW := w - 2
	if textW < 0 {
		textW = 0
	}
	if lipgloss.Width(content) > textW {
		if textW > 3 {
			// Truncate rune-by-rune until it fits
			truncated := []rune(content)
			for lipgloss.Width(string(truncated)+"...") > textW && len(truncated) > 0 {
				truncated = truncated[:len(truncated)-1]
			}
			content = string(truncated) + "..."
		}
	}
	return lipgloss.NewStyle().
		Width(w).
		Foreground(fg).
		Background(bg).
		Padding(0, 1).
		Render(content)
}

func renderHLine(cellWidths []int, left, mid, right string) string {
	var b strings.Builder
	b.WriteString(left)
	for i, w := range cellWidths {
		b.WriteString(strings.Repeat("─", w)) // match cell Width (includes padding)
		if i < len(cellWidths)-1 {
			b.WriteString(mid)
		}
	}
	b.WriteString(right)
	return borderStyle.Render(b.String())
}

func (m model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	// Column widths: lipgloss Width includes padding, so cell renders to w chars.
	// Row = 5 (│ borders) + nameW + shaW + statusW + deployW + 1 (scrollbar) = m.width
	statusW := 14
	deployW := 15
	shaW := 14
	nameW := m.width - statusW - deployW - shaW - 6
	if nameW < 20 {
		nameW = 20
	}
	colWidths := []int{nameW, shaW, statusW, deployW}

	// Calculate vertical space to fill entire terminal height.
	// Status bar: top border + content lines + bottom border = 3 (or 4 with progress)
	// Table chrome: top border + header + separator + bottom border = 4
	// Total lines = statusBoxLines + 4 + tableHeight = m.height
	statusBoxLines := 3
	if m.phase == "pulling" {
		statusBoxLines = 4
	}
	tableHeight := m.height - statusBoxLines - 4
	if tableHeight < 1 {
		tableHeight = 1
	}

	rows := m.buildRows()

	// Clamp cursor
	if m.cursor >= len(rows) {
		m.cursor = len(rows) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}

	// Auto-scroll
	if m.cursor < m.scrollOffset {
		m.scrollOffset = m.cursor
	}
	if m.cursor >= m.scrollOffset+tableHeight {
		m.scrollOffset = m.cursor - tableHeight + 1
	}
	maxScroll := len(rows) - tableHeight
	if maxScroll < 0 {
		maxScroll = 0
	}
	if m.scrollOffset > maxScroll {
		m.scrollOffset = maxScroll
	}
	if m.scrollOffset < 0 {
		m.scrollOffset = 0
	}

	// Scrollbar: determine if we need one and calculate thumb position
	needScroll := len(rows) > tableHeight
	var thumbStart, thumbEnd int
	if needScroll && tableHeight > 0 {
		thumbSize := tableHeight * tableHeight / len(rows)
		if thumbSize < 1 {
			thumbSize = 1
		}
		thumbStart = m.scrollOffset * tableHeight / len(rows)
		thumbEnd = thumbStart + thumbSize
		if thumbEnd > tableHeight {
			thumbEnd = tableHeight
		}
	}

	var b strings.Builder

	// ═══ Status Bar Box ═══
	statusContent := m.renderStatusBar()
	if m.phase == "pulling" {
		statusContent += "\n" + m.progress.View()
	}
	statusBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Width(m.width - 3). // Width includes padding; border adds 2 → total = m.width-1 (matches table+scrollbar)
		Padding(0, 1).
		Render(statusContent)
	b.WriteString(statusBox)
	b.WriteByte('\n')

	// ═══ Table ═══
	// Top border
	b.WriteString(renderHLine(colWidths, "╭", "┬", "╮"))
	b.WriteByte(' ') // scrollbar column placeholder
	b.WriteByte('\n')

	// Header row
	hdrCells := []string{
		renderCell("Service / Image", nameW, headerFg, headerBg),
		renderCell("SHA", shaW, headerFg, headerBg),
		renderCell("Status", statusW, headerFg, headerBg),
		renderCell("Deploy", deployW, headerFg, headerBg),
	}
	hdrSep := lipgloss.NewStyle().Foreground(borderColor).Background(headerBg).Render("│")
	b.WriteString(hdrSep + strings.Join(hdrCells, hdrSep) + hdrSep)
	b.WriteByte(' ') // scrollbar column placeholder
	b.WriteByte('\n')

	// Header separator
	b.WriteString(renderHLine(colWidths, "├", "┼", "┤"))
	b.WriteByte(' ') // scrollbar column placeholder
	b.WriteByte('\n')

	// Data rows
	endIdx := m.scrollOffset + tableHeight
	if endIdx > len(rows) {
		endIdx = len(rows)
	}

	sep := lipgloss.NewStyle().Foreground(borderColor).Render("│")

	dataRowIdx := 0
	for i := m.scrollOffset; i < endIdx; i++ {
		r := rows[i]

		// Separator row: horizontal line between services
		if r.imageIdx == -2 {
			b.WriteString(renderHLine(colWidths, "├", "┼", "┤"))
			if needScroll && dataRowIdx >= thumbStart && dataRowIdx < thumbEnd {
				b.WriteString(lipgloss.NewStyle().Foreground(scrollThumbFg).Render("▐"))
			} else if needScroll {
				b.WriteString(lipgloss.NewStyle().Foreground(scrollTrackFg).Render("│"))
			} else {
				b.WriteByte(' ')
			}
			b.WriteByte('\n')
			dataRowIdx++
			continue
		}

		svc := m.services[r.serviceIdx]
		isSelected := i == m.cursor

		var rowFg lipgloss.Color
		if isSelected {
			rowFg = selectedFg
		} else if r.imageIdx == -1 {
			rowFg = serviceFg
		} else {
			rowFg = imageFg
		}

		var name, sha, status, deploy string

		if r.imageIdx == -1 {
			// Service header row
			prefix := "  "
			if isSelected {
				prefix = "▶ "
			}
			name = prefix + svc.Name
			deploy = formatDeployStatus(svc.DeployStatus)
		} else {
			// Image row
			img := svc.Images[r.imageIdx]
			prefix := "    "
			if isSelected {
				prefix = "  › "
			}
			name = prefix + img.Image
			sha = formatSHA(img)
			status = m.formatImageStatus(img)
		}

		// Updated images get green text (unless selected)
		nameFg := rowFg
		if r.imageIdx >= 0 && !isSelected {
			img := svc.Images[r.imageIdx]
			if img.Updated {
				nameFg = imageUpdatedFg
			}
		}

		// Row background: only for selected row
		nameStyle := lipgloss.NewStyle().Width(nameW).Foreground(nameFg).Padding(0, 1)
		if isSelected {
			nameStyle = nameStyle.Background(selectedBg)
		}
		if r.imageIdx == -1 {
			nameStyle = nameStyle.Bold(true)
		}
		nameContent := name
		nameTextW := nameW - 2
		if lipgloss.Width(nameContent) > nameTextW {
			if nameTextW > 3 {
				truncated := []rune(nameContent)
				for lipgloss.Width(string(truncated)+"...") > nameTextW && len(truncated) > 0 {
					truncated = truncated[:len(truncated)-1]
				}
				nameContent = string(truncated) + "..."
			}
		}

		shaFg := rowFg
		if r.imageIdx >= 0 && !isSelected {
			img := svc.Images[r.imageIdx]
			if img.PullStatus == StatusComplete && img.NewSHA != "" && img.OldSHA != img.NewSHA {
				shaFg = shaChangedFg
			}
		}

		cellStyle := func(w int, fg lipgloss.Color) lipgloss.Style {
			s := lipgloss.NewStyle().Width(w).Foreground(fg).Padding(0, 1)
			if isSelected {
				s = s.Background(selectedBg)
			}
			return s
		}

		b.WriteString(sep)
		b.WriteString(nameStyle.Render(nameContent))
		b.WriteString(sep)
		b.WriteString(cellStyle(shaW, shaFg).Render(sha))
		b.WriteString(sep)
		b.WriteString(cellStyle(statusW, rowFg).Render(status))
		b.WriteString(sep)
		b.WriteString(cellStyle(deployW, rowFg).Render(deploy))
		b.WriteString(sep)

		// Scrollbar
		if needScroll && dataRowIdx >= thumbStart && dataRowIdx < thumbEnd {
			b.WriteString(lipgloss.NewStyle().Foreground(scrollThumbFg).Render("▐"))
		} else if needScroll {
			b.WriteString(lipgloss.NewStyle().Foreground(scrollTrackFg).Render("│"))
		} else {
			b.WriteByte(' ')
		}
		b.WriteByte('\n')
		dataRowIdx++
	}

	// Pad empty rows
	linesWritten := endIdx - m.scrollOffset
	for linesWritten < tableHeight {
		sep := lipgloss.NewStyle().Foreground(borderColor).Render("│")
		emptyCell := func(w int) string {
			return lipgloss.NewStyle().Width(w).Padding(0, 1).Render("")
		}
		b.WriteString(sep)
		b.WriteString(emptyCell(nameW))
		b.WriteString(sep)
		b.WriteString(emptyCell(shaW))
		b.WriteString(sep)
		b.WriteString(emptyCell(statusW))
		b.WriteString(sep)
		b.WriteString(emptyCell(deployW))
		b.WriteString(sep)

		// Scrollbar
		if needScroll && dataRowIdx >= thumbStart && dataRowIdx < thumbEnd {
			b.WriteString(lipgloss.NewStyle().Foreground(scrollThumbFg).Render("▐"))
		} else if needScroll {
			b.WriteString(lipgloss.NewStyle().Foreground(scrollTrackFg).Render("│"))
		} else {
			b.WriteByte(' ')
		}
		b.WriteByte('\n')
		linesWritten++
		dataRowIdx++
	}

	// Bottom border
	b.WriteString(renderHLine(colWidths, "╰", "┴", "╯"))
	b.WriteByte(' ') // scrollbar column placeholder

	return b.String()
}

func (m model) renderStatusBar() string {
	if m.err != nil {
		return fmt.Sprintf("Error: %s  Press q to quit.", m.err)
	}
	switch m.phase {
	case "loading":
		return m.spinner.View() + " Loading services from " + dokployURL + "..."
	case "pulling":
		pulling := 0
		complete := 0
		total := 0
		for _, svc := range m.services {
			for _, img := range svc.Images {
				total++
				if img.PullStatus == StatusPulling {
					pulling++
				}
				if img.PullStatus == StatusComplete {
					complete++
				}
			}
		}
		return fmt.Sprintf("%s Pulling images... (%d/%d complete, %d in progress)",
			m.spinner.View(), complete, total, pulling)
	case "deploying":
		return m.spinner.View() + " Triggering deployments..."
	case "done":
		updated := 0
		deployed := 0
		for _, svc := range m.services {
			for _, img := range svc.Images {
				if img.Updated {
					updated++
				}
			}
			if svc.DeployStatus == DeployTriggered {
				deployed++
			}
		}
		return fmt.Sprintf("Done! %d images updated, %d deployments triggered.  Enter=redeploy  q=quit",
			updated, deployed)
	}
	return ""
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
			fmt.Println("  DOKPLOY_URL      Dokploy instance URL (required)")
			fmt.Println("  DOKPLOY_API_KEY  Dokploy API key (required)")
			os.Exit(0)
		}
	}

	var missing []string
	if dokployURL == "" {
		missing = append(missing, "DOKPLOY_URL")
	}
	if dokployAPIKey == "" {
		missing = append(missing, "DOKPLOY_API_KEY")
	}
	if len(missing) > 0 {
		fmt.Println("Error: required environment variables are not set:")
		for _, v := range missing {
			fmt.Printf("  %s\n", v)
		}
		fmt.Println()
		fmt.Println("Example:")
		fmt.Println("  export DOKPLOY_URL=http://your-server:3000")
		fmt.Println("  export DOKPLOY_API_KEY=your-api-key")
		os.Exit(1)
	}

	p := tea.NewProgram(initialModel(deployAll), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}
