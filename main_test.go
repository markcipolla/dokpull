package main

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// --- PullStatus enum tests ---

func TestPullStatusEnumValues(t *testing.T) {
	if StatusPending != 0 {
		t.Errorf("StatusPending = %d, want 0", StatusPending)
	}
	if StatusPulling != 1 {
		t.Errorf("StatusPulling = %d, want 1", StatusPulling)
	}
	if StatusComplete != 2 {
		t.Errorf("StatusComplete = %d, want 2", StatusComplete)
	}
	if StatusError != 3 {
		t.Errorf("StatusError = %d, want 3", StatusError)
	}
}

// --- DeployStatus enum tests ---

func TestDeployStatusEnumValues(t *testing.T) {
	if DeployNone != 0 {
		t.Errorf("DeployNone = %d, want 0", DeployNone)
	}
	if DeployNeeded != 1 {
		t.Errorf("DeployNeeded = %d, want 1", DeployNeeded)
	}
	if DeployTriggered != 2 {
		t.Errorf("DeployTriggered = %d, want 2", DeployTriggered)
	}
	if DeployFailed != 3 {
		t.Errorf("DeployFailed = %d, want 3", DeployFailed)
	}
}

// --- initialModel tests ---

func TestInitialModel_DeployAllFalse(t *testing.T) {
	m := initialModel(false)
	if m.deployAll != false {
		t.Errorf("initialModel(false).deployAll = %v, want false", m.deployAll)
	}
	if m.phase != "loading" {
		t.Errorf("initialModel(false).phase = %q, want %q", m.phase, "loading")
	}
	if m.services != nil {
		t.Errorf("initialModel(false).services should be nil, got %v", m.services)
	}
}

func TestInitialModel_DeployAllTrue(t *testing.T) {
	m := initialModel(true)
	if m.deployAll != true {
		t.Errorf("initialModel(true).deployAll = %v, want true", m.deployAll)
	}
	if m.phase != "loading" {
		t.Errorf("initialModel(true).phase = %q, want %q", m.phase, "loading")
	}
}

// --- ImageInfo defaults ---

func TestImageInfo_DefaultValues(t *testing.T) {
	img := ImageInfo{}
	if img.PullStatus != StatusPending {
		t.Errorf("default PullStatus = %d, want StatusPending (%d)", img.PullStatus, StatusPending)
	}
	if img.Updated != false {
		t.Error("default Updated should be false")
	}
	if img.OldSHA != "" {
		t.Errorf("default OldSHA = %q, want empty", img.OldSHA)
	}
	if img.NewSHA != "" {
		t.Errorf("default NewSHA = %q, want empty", img.NewSHA)
	}
	if img.Error != "" {
		t.Errorf("default Error = %q, want empty", img.Error)
	}
}

func TestServiceState_DefaultValues(t *testing.T) {
	svc := ServiceState{}
	if svc.DeployStatus != DeployNone {
		t.Errorf("default DeployStatus = %d, want DeployNone (%d)", svc.DeployStatus, DeployNone)
	}
	if svc.Images != nil {
		t.Error("default Images should be nil")
	}
}

// --- buildRows tests ---

func TestBuildRows_Empty(t *testing.T) {
	m := initialModel(false)
	rows := m.buildRows()
	if len(rows) != 0 {
		t.Errorf("buildRows() with no services = %d rows, want 0", len(rows))
	}
}

func TestBuildRows_Structure(t *testing.T) {
	m := initialModel(false)
	m.services = []ServiceState{
		{Name: "svc1", Images: []ImageInfo{{Image: "img1"}, {Image: "img2"}}},
		{Name: "svc2", Images: []ImageInfo{{Image: "img3"}}},
		{Name: "svc3"}, // no images
	}
	rows := m.buildRows()
	// svc1: header + 2 images = 3, sep + svc2: header + 1 image = 3, sep + svc3: header = 2 => total 8
	if len(rows) != 8 {
		t.Fatalf("buildRows() = %d rows, want 8", len(rows))
	}
	if rows[0].serviceIdx != 0 || rows[0].imageIdx != -1 {
		t.Errorf("row 0: serviceIdx=%d imageIdx=%d, want 0,-1 (svc1 header)", rows[0].serviceIdx, rows[0].imageIdx)
	}
	if rows[1].serviceIdx != 0 || rows[1].imageIdx != 0 {
		t.Errorf("row 1: serviceIdx=%d imageIdx=%d, want 0,0 (img1)", rows[1].serviceIdx, rows[1].imageIdx)
	}
	if rows[2].serviceIdx != 0 || rows[2].imageIdx != 1 {
		t.Errorf("row 2: serviceIdx=%d imageIdx=%d, want 0,1 (img2)", rows[2].serviceIdx, rows[2].imageIdx)
	}
	if rows[3].imageIdx != -2 {
		t.Errorf("row 3: imageIdx=%d, want -2 (separator)", rows[3].imageIdx)
	}
	if rows[4].serviceIdx != 1 || rows[4].imageIdx != -1 {
		t.Errorf("row 4: serviceIdx=%d imageIdx=%d, want 1,-1 (svc2 header)", rows[4].serviceIdx, rows[4].imageIdx)
	}
	if rows[5].serviceIdx != 1 || rows[5].imageIdx != 0 {
		t.Errorf("row 5: serviceIdx=%d imageIdx=%d, want 1,0 (img3)", rows[5].serviceIdx, rows[5].imageIdx)
	}
	if rows[6].imageIdx != -2 {
		t.Errorf("row 6: imageIdx=%d, want -2 (separator)", rows[6].imageIdx)
	}
	if rows[7].serviceIdx != 2 || rows[7].imageIdx != -1 {
		t.Errorf("row 7: serviceIdx=%d imageIdx=%d, want 2,-1 (svc3 header)", rows[7].serviceIdx, rows[7].imageIdx)
	}
}

// --- countInFlight / allImagesDone tests ---

func TestCountInFlight(t *testing.T) {
	m := initialModel(false)
	m.services = []ServiceState{
		{Images: []ImageInfo{
			{PullStatus: StatusPulling},
			{PullStatus: StatusComplete},
		}},
		{Images: []ImageInfo{
			{PullStatus: StatusPulling},
			{PullStatus: StatusPending},
		}},
	}
	if got := m.countInFlight(); got != 2 {
		t.Errorf("countInFlight() = %d, want 2", got)
	}
}

func TestAllImagesDone_AllComplete(t *testing.T) {
	m := initialModel(false)
	m.services = []ServiceState{
		{Images: []ImageInfo{{PullStatus: StatusComplete}, {PullStatus: StatusComplete}}},
		{Images: []ImageInfo{{PullStatus: StatusError}}},
	}
	if !m.allImagesDone() {
		t.Error("allImagesDone() = false, want true (all complete or error)")
	}
}

func TestAllImagesDone_StillPending(t *testing.T) {
	m := initialModel(false)
	m.services = []ServiceState{
		{Images: []ImageInfo{{PullStatus: StatusComplete}, {PullStatus: StatusPending}}},
	}
	if m.allImagesDone() {
		t.Error("allImagesDone() = true, want false (one still pending)")
	}
}

func TestAllImagesDone_NoImages(t *testing.T) {
	m := initialModel(false)
	m.services = []ServiceState{{Name: "empty"}}
	if !m.allImagesDone() {
		t.Error("allImagesDone() = false, want true (no images at all)")
	}
}

// --- pullProgress tests ---

func TestPullProgress_Empty(t *testing.T) {
	m := initialModel(false)
	if got := m.pullProgress(); got != 0 {
		t.Errorf("pullProgress() with no services = %f, want 0", got)
	}
}

func TestPullProgress_HalfDone(t *testing.T) {
	m := initialModel(false)
	m.services = []ServiceState{
		{Images: []ImageInfo{
			{PullStatus: StatusComplete},
			{PullStatus: StatusPulling},
		}},
	}
	got := m.pullProgress()
	if got != 0.5 {
		t.Errorf("pullProgress() = %f, want 0.5", got)
	}
}

func TestPullProgress_AllDone(t *testing.T) {
	m := initialModel(false)
	m.services = []ServiceState{
		{Images: []ImageInfo{
			{PullStatus: StatusComplete},
			{PullStatus: StatusError},
		}},
	}
	got := m.pullProgress()
	if got != 1.0 {
		t.Errorf("pullProgress() = %f, want 1.0", got)
	}
}

// --- formatSHA tests ---

func TestFormatSHA_NoSHAs(t *testing.T) {
	img := ImageInfo{PullStatus: StatusPending}
	if got := formatSHA(img); got != "" {
		t.Errorf("formatSHA() = %q, want empty for pending image with no SHAs", got)
	}
}

func TestFormatSHA_OldSHAOnly(t *testing.T) {
	img := ImageInfo{PullStatus: StatusPending, OldSHA: "abc123def456"}
	if got := formatSHA(img); got != "abc123def456" {
		t.Errorf("formatSHA() = %q, want %q", got, "abc123def456")
	}
}

func TestFormatSHA_CompleteSameSHA(t *testing.T) {
	img := ImageInfo{PullStatus: StatusComplete, OldSHA: "abc123def456", NewSHA: "abc123def456"}
	got := formatSHA(img)
	if got != "abc123def456" {
		t.Errorf("formatSHA() = %q, want just the SHA when old==new", got)
	}
}

func TestFormatSHA_CompleteDifferentSHA(t *testing.T) {
	img := ImageInfo{PullStatus: StatusComplete, OldSHA: "abc123def456", NewSHA: "789xyz012345"}
	got := formatSHA(img)
	want := "abc123def456→789xyz012345"
	if got != want {
		t.Errorf("formatSHA() = %q, want %q", got, want)
	}
}

func TestFormatSHA_CompleteNewSHAOnly(t *testing.T) {
	img := ImageInfo{PullStatus: StatusComplete, NewSHA: "789xyz012345"}
	got := formatSHA(img)
	if got != "789xyz012345" {
		t.Errorf("formatSHA() = %q, want %q", got, "789xyz012345")
	}
}

// --- formatDeployStatus tests ---

func TestFormatDeployStatus(t *testing.T) {
	tests := []struct {
		ds       DeployStatus
		contains string
	}{
		{DeployNone, "─"},
		{DeployNeeded, "pending"},
		{DeployTriggered, "triggered"},
		{DeployFailed, "failed"},
	}
	for _, tt := range tests {
		got := formatDeployStatus(tt.ds)
		if !strings.Contains(got, tt.contains) {
			t.Errorf("formatDeployStatus(%d) = %q, want containing %q", tt.ds, got, tt.contains)
		}
	}
}

// --- formatImageStatus tests ---

func TestFormatImageStatus(t *testing.T) {
	m := initialModel(false)

	tests := []struct {
		name     string
		img      ImageInfo
		contains string
	}{
		{"pending", ImageInfo{PullStatus: StatusPending}, "waiting"},
		{"pulling", ImageInfo{PullStatus: StatusPulling}, "pulling"},
		{"complete updated", ImageInfo{PullStatus: StatusComplete, Updated: true}, "updated"},
		{"complete current", ImageInfo{PullStatus: StatusComplete, Updated: false}, "current"},
		{"error", ImageInfo{PullStatus: StatusError}, "error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.formatImageStatus(tt.img)
			if !strings.Contains(got, tt.contains) {
				t.Errorf("formatImageStatus() = %q, want containing %q", got, tt.contains)
			}
		})
	}
}

// --- startDeploys tests ---

func TestStartDeploys_OnlyDeployNeeded(t *testing.T) {
	m := model{
		services: []ServiceState{
			{ComposeID: "c1", Name: "web", DeployStatus: DeployNeeded, Images: []ImageInfo{{Image: "nginx:latest"}}},
			{ComposeID: "c2", Name: "cache", DeployStatus: DeployNone, Images: []ImageInfo{{Image: "redis:latest"}}},
			{ComposeID: "c3", Name: "backend", DeployStatus: DeployNeeded, Images: []ImageInfo{{Image: "myapp:v1"}}},
		},
	}

	cmd := m.startDeploys()
	if cmd == nil {
		t.Fatal("startDeploys() returned nil, expected commands for DeployNeeded services")
	}
}

func TestStartDeploys_NoneNeeded(t *testing.T) {
	m := model{
		services: []ServiceState{
			{ComposeID: "c1", Name: "web", DeployStatus: DeployNone},
			{ComposeID: "c2", Name: "cache", DeployStatus: DeployTriggered},
		},
	}

	cmd := m.startDeploys()
	if cmd == nil {
		t.Fatal("startDeploys() returned nil, expected allDoneMsg command")
	}
	msg := cmd()
	if _, ok := msg.(allDoneMsg); !ok {
		t.Errorf("expected allDoneMsg when no services need deploy, got %T", msg)
	}
}

func TestStartDeploys_NoServices(t *testing.T) {
	m := model{services: nil}

	cmd := m.startDeploys()
	if cmd == nil {
		t.Fatal("startDeploys() returned nil cmd, expected allDoneMsg command")
	}
	msg := cmd()
	if _, ok := msg.(allDoneMsg); !ok {
		t.Errorf("expected allDoneMsg when no services, got %T", msg)
	}
}

// --- View rendering tests ---

func TestView_LoadingWithZeroWidth(t *testing.T) {
	m := initialModel(false)
	view := m.View()
	if view != "Loading..." {
		t.Errorf("View() with width=0 = %q, want %q", view, "Loading...")
	}
}

func TestView_LoadingPhase(t *testing.T) {
	m := initialModel(false)
	m.width = 120
	m.height = 40
	view := m.View()
	if !strings.Contains(view, "Loading services") {
		t.Error("View() in loading phase should contain 'Loading services'")
	}
}

func TestView_PullingPhase(t *testing.T) {
	m := initialModel(false)
	m.width = 120
	m.height = 40
	m.phase = "pulling"
	m.services = []ServiceState{
		{Name: "Plex", Images: []ImageInfo{{Image: "plex:latest", PullStatus: StatusPulling}}},
		{Name: "Sonarr", Images: []ImageInfo{{Image: "sonarr:latest", PullStatus: StatusComplete, Updated: false}}},
	}
	view := m.View()
	if !strings.Contains(view, "Plex") {
		t.Error("View() should contain service name 'Plex'")
	}
	if !strings.Contains(view, "Sonarr") {
		t.Error("View() should contain service name 'Sonarr'")
	}
	if !strings.Contains(view, "Pulling images") {
		t.Error("View() in pulling phase should contain 'Pulling images'")
	}
	if !strings.Contains(view, "current") {
		t.Error("View() should show 'current' for non-updated complete images")
	}
}

func TestView_DonePhase(t *testing.T) {
	m := initialModel(false)
	m.width = 120
	m.height = 40
	m.phase = "done"
	m.services = []ServiceState{
		{Name: "Plex", DeployStatus: DeployTriggered, Images: []ImageInfo{{Image: "plex:latest", PullStatus: StatusComplete, Updated: true}}},
		{Name: "Sonarr", DeployStatus: DeployNone, Images: []ImageInfo{{Image: "sonarr:latest", PullStatus: StatusComplete, Updated: false}}},
	}
	view := m.View()
	if !strings.Contains(view, "Done!") {
		t.Error("View() in done phase should contain 'Done!'")
	}
	if !strings.Contains(view, "1 images updated") {
		t.Error("View() should report 1 images updated")
	}
	if !strings.Contains(view, "1 deployments triggered") {
		t.Error("View() should report 1 deployment triggered")
	}
	if !strings.Contains(view, "q=quit") {
		t.Error("View() in done phase should contain quit instructions")
	}
}

func TestView_DeployingPhase(t *testing.T) {
	m := initialModel(false)
	m.width = 120
	m.height = 40
	m.phase = "deploying"
	m.services = []ServiceState{
		{Name: "Plex", DeployStatus: DeployNeeded, Images: []ImageInfo{{Image: "plex:latest", PullStatus: StatusComplete, Updated: true}}},
	}
	view := m.View()
	if !strings.Contains(view, "Triggering deployments") {
		t.Error("View() in deploying phase should contain 'Triggering deployments'")
	}
}

func TestView_ErrorState(t *testing.T) {
	m := initialModel(false)
	m.width = 120
	m.height = 40
	m.phase = "pulling"
	m.services = []ServiceState{
		{Name: "BadService", Images: []ImageInfo{{Image: "bad:latest", PullStatus: StatusError, Error: "pull access denied"}}},
	}
	view := m.View()
	if !strings.Contains(view, "error") {
		t.Error("View() should show 'error' for images with StatusError")
	}
}

func TestView_TableHeaders(t *testing.T) {
	m := initialModel(false)
	m.width = 120
	m.height = 40
	view := m.View()
	for _, header := range []string{"Service", "SHA", "Status", "Deploy"} {
		if !strings.Contains(view, header) {
			t.Errorf("View() should contain table header %q", header)
		}
	}
}

func TestView_TableHasBoxDrawingChars(t *testing.T) {
	m := initialModel(false)
	m.width = 120
	m.height = 40
	m.services = []ServiceState{
		{Name: "Svc", Images: []ImageInfo{{Image: "img:latest"}}},
	}
	view := m.View()
	for _, ch := range []string{"╭", "╮", "╰", "╯", "─", "│", "┬", "┴", "├", "┤", "┼"} {
		if !strings.Contains(view, ch) {
			t.Errorf("View() should contain box-drawing character %q", ch)
		}
	}
}

func TestView_StatusBarHasBorder(t *testing.T) {
	m := initialModel(false)
	m.width = 120
	m.height = 40
	view := m.View()
	// The status bar is in a rounded border box, which uses ╭ ╮ characters
	// The table also uses ╭ ╮, so we should see at least 2 occurrences of ╭
	if strings.Count(view, "╭") < 2 {
		t.Error("View() should have separate bordered status bar and table (at least 2 top-left corners)")
	}
}

// --- DeployStatus display tests ---

func TestView_DeployStatusDisplay(t *testing.T) {
	tests := []struct {
		name         string
		deployStatus DeployStatus
		wantContains string
	}{
		{"DeployNone shows dash", DeployNone, "─"},
		{"DeployNeeded shows pending", DeployNeeded, "pending"},
		{"DeployTriggered shows triggered", DeployTriggered, "triggered"},
		{"DeployFailed shows failed", DeployFailed, "failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := initialModel(false)
			m.width = 120
			m.height = 40
			m.phase = "done"
			m.services = []ServiceState{
				{Name: "TestSvc", DeployStatus: tt.deployStatus, Images: []ImageInfo{{Image: "test:latest", PullStatus: StatusComplete}}},
			}
			view := m.View()
			if !strings.Contains(view, tt.wantContains) {
				t.Errorf("View() with DeployStatus=%d should contain %q", tt.deployStatus, tt.wantContains)
			}
		})
	}
}

// --- Model Update tests ---

func TestUpdate_WindowSizeMsg(t *testing.T) {
	m := initialModel(false)
	newModel, _ := m.Update(tea.WindowSizeMsg{Width: 200, Height: 50})
	updated := newModel.(model)
	if updated.width != 200 {
		t.Errorf("width after WindowSizeMsg = %d, want 200", updated.width)
	}
	if updated.height != 50 {
		t.Errorf("height after WindowSizeMsg = %d, want 50", updated.height)
	}
}

func TestUpdate_ServicesLoadedMsg_StartsPulling(t *testing.T) {
	m := initialModel(false)
	m.phase = "loading"
	services := []ServiceState{
		{ComposeID: "c1", Name: "Plex", Images: []ImageInfo{{Image: "plex:latest", PullStatus: StatusPending}}},
		{ComposeID: "c2", Name: "Sonarr", Images: []ImageInfo{{Image: "sonarr:latest", PullStatus: StatusPending}}},
	}
	newModel, _ := m.Update(servicesLoadedMsg{services: services})
	updated := newModel.(model)
	if len(updated.services) != 2 {
		t.Errorf("services count = %d, want 2", len(updated.services))
	}
	if updated.phase != "pulling" {
		t.Errorf("phase after servicesLoaded = %q, want %q", updated.phase, "pulling")
	}
	if updated.services[0].Images[0].PullStatus != StatusPulling {
		t.Errorf("first image status = %d, want StatusPulling (%d)", updated.services[0].Images[0].PullStatus, StatusPulling)
	}
	if updated.services[1].Images[0].PullStatus != StatusPulling {
		t.Errorf("second image status = %d, want StatusPulling (%d)", updated.services[1].Images[0].PullStatus, StatusPulling)
	}
}

func TestUpdate_ServicesLoadedMsg_Error(t *testing.T) {
	m := initialModel(false)
	m.phase = "loading"
	newModel, _ := m.Update(servicesLoadedMsg{err: fmt.Errorf("connection refused")})
	updated := newModel.(model)
	if updated.phase != "done" {
		t.Errorf("phase after error = %q, want %q", updated.phase, "done")
	}
	if updated.err == nil {
		t.Error("err should be set after servicesLoadedMsg with error")
	}
}

func TestUpdate_ServicesLoadedMsg_NoImagesGoesToDone(t *testing.T) {
	m := initialModel(false)
	m.phase = "loading"
	services := []ServiceState{
		{ComposeID: "c1", Name: "NoImage1"},
		{ComposeID: "c2", Name: "NoImage2"},
	}
	newModel, _ := m.Update(servicesLoadedMsg{services: services})
	updated := newModel.(model)
	if updated.phase != "done" {
		t.Errorf("phase = %q, want %q when no services have images", updated.phase, "done")
	}
}

func TestUpdate_ServicesLoadedMsg_MixedImagesAndNoImages(t *testing.T) {
	m := initialModel(false)
	m.phase = "loading"
	services := []ServiceState{
		{ComposeID: "c1", Name: "HasImage", Images: []ImageInfo{{Image: "nginx:latest", PullStatus: StatusPending}}},
		{ComposeID: "c2", Name: "NoImage"},
	}
	newModel, _ := m.Update(servicesLoadedMsg{services: services})
	updated := newModel.(model)
	if updated.phase != "pulling" {
		t.Errorf("phase = %q, want %q when at least one service has images", updated.phase, "pulling")
	}
	if updated.services[0].Images[0].PullStatus != StatusPulling {
		t.Errorf("image PullStatus = %d, want StatusPulling", updated.services[0].Images[0].PullStatus)
	}
}

func TestUpdate_PullCompleteMsg_SetsUpdatedStatus(t *testing.T) {
	m := initialModel(false)
	m.phase = "pulling"
	m.services = []ServiceState{
		{ComposeID: "c1", Name: "Plex", Images: []ImageInfo{{Image: "plex:latest", PullStatus: StatusPulling}}},
	}

	newModel, _ := m.Update(pullCompleteMsg{serviceIdx: 0, imageIdx: 0, updated: true, sha: "newsha123456"})
	updated := newModel.(model)

	img := updated.services[0].Images[0]
	if img.PullStatus != StatusComplete {
		t.Errorf("PullStatus = %d, want StatusComplete (%d)", img.PullStatus, StatusComplete)
	}
	if img.Updated != true {
		t.Error("Updated should be true after pullCompleteMsg with updated=true")
	}
	if img.NewSHA != "newsha123456" {
		t.Errorf("NewSHA = %q, want %q", img.NewSHA, "newsha123456")
	}
	if updated.services[0].DeployStatus != DeployNeeded {
		t.Errorf("DeployStatus = %d, want DeployNeeded for service with updated image", updated.services[0].DeployStatus)
	}
}

func TestUpdate_PullCompleteMsg_NotUpdated(t *testing.T) {
	m := initialModel(false)
	m.phase = "pulling"
	m.services = []ServiceState{
		{ComposeID: "c1", Name: "Plex", Images: []ImageInfo{{Image: "plex:latest", PullStatus: StatusPulling, OldSHA: "oldsha123456"}}},
	}

	newModel, _ := m.Update(pullCompleteMsg{serviceIdx: 0, imageIdx: 0, updated: false, sha: ""})
	updated := newModel.(model)

	img := updated.services[0].Images[0]
	if img.Updated != false {
		t.Error("Updated should be false")
	}
	if img.OldSHA != "oldsha123456" {
		t.Errorf("OldSHA = %q, want %q (should remain unchanged)", img.OldSHA, "oldsha123456")
	}
	if img.NewSHA != "" {
		t.Errorf("NewSHA = %q, want empty (sha was empty in pullCompleteMsg)", img.NewSHA)
	}
	if updated.services[0].DeployStatus != DeployNone {
		t.Errorf("DeployStatus = %d, want DeployNone for non-updated service", updated.services[0].DeployStatus)
	}
}

func TestUpdate_PullCompleteMsg_StartsNextImage(t *testing.T) {
	m := initialModel(false)
	m.phase = "pulling"
	m.services = []ServiceState{
		{ComposeID: "c1", Name: "s1", Images: []ImageInfo{{Image: "img1:latest", PullStatus: StatusPulling}}},
		{ComposeID: "c2", Name: "s2", Images: []ImageInfo{{Image: "img2:latest", PullStatus: StatusPulling}}},
		{ComposeID: "c3", Name: "s3", Images: []ImageInfo{{Image: "img3:latest", PullStatus: StatusPulling}}},
		{ComposeID: "c4", Name: "s4", Images: []ImageInfo{{Image: "img4:latest", PullStatus: StatusPending}}},
	}

	newModel, _ := m.Update(pullCompleteMsg{serviceIdx: 0, imageIdx: 0, updated: false})
	updated := newModel.(model)

	if updated.services[3].Images[0].PullStatus != StatusPulling {
		t.Errorf("s4 image PullStatus = %d, want StatusPulling (%d)", updated.services[3].Images[0].PullStatus, StatusPulling)
	}
}

func TestUpdate_PullCompleteMsg_AllDoneTransition(t *testing.T) {
	m := initialModel(false)
	m.phase = "pulling"
	m.services = []ServiceState{
		{ComposeID: "c1", Name: "s1", Images: []ImageInfo{{Image: "img:latest", PullStatus: StatusPulling}}},
	}

	newModel, _ := m.Update(pullCompleteMsg{serviceIdx: 0, imageIdx: 0, updated: false})
	updated := newModel.(model)

	if updated.phase != "deploying" {
		t.Errorf("phase = %q, want %q after all images pulled", updated.phase, "deploying")
	}
}

func TestUpdate_PullCompleteMsg_DeployAllSetsAllNeeded(t *testing.T) {
	m := initialModel(true)
	m.phase = "pulling"
	m.services = []ServiceState{
		{ComposeID: "c1", Name: "s1", Images: []ImageInfo{{Image: "img1:latest", PullStatus: StatusPulling}}},
		{ComposeID: "c2", Name: "s2", Images: []ImageInfo{{Image: "img2:latest", PullStatus: StatusComplete, Updated: false}}},
	}

	newModel, _ := m.Update(pullCompleteMsg{serviceIdx: 0, imageIdx: 0, updated: false})
	updated := newModel.(model)

	if updated.services[0].DeployStatus != DeployNeeded {
		t.Errorf("s1 DeployStatus = %d, want DeployNeeded with deployAll", updated.services[0].DeployStatus)
	}
	if updated.services[1].DeployStatus != DeployNeeded {
		t.Errorf("s2 DeployStatus = %d, want DeployNeeded with deployAll", updated.services[1].DeployStatus)
	}
}

func TestUpdate_PullErrorMsg(t *testing.T) {
	m := initialModel(false)
	m.phase = "pulling"
	m.services = []ServiceState{
		{ComposeID: "c1", Name: "Bad", Images: []ImageInfo{{Image: "bad:latest", PullStatus: StatusPulling}}},
	}

	newModel, _ := m.Update(pullErrorMsg{serviceIdx: 0, imageIdx: 0, err: fmt.Errorf("pull access denied")})
	updated := newModel.(model)

	img := updated.services[0].Images[0]
	if img.PullStatus != StatusError {
		t.Errorf("PullStatus = %d, want StatusError (%d)", img.PullStatus, StatusError)
	}
	if img.Error != "pull access denied" {
		t.Errorf("Error = %q, want %q", img.Error, "pull access denied")
	}
}

func TestUpdate_DeployCompleteMsg_Success(t *testing.T) {
	m := initialModel(false)
	m.phase = "deploying"
	m.services = []ServiceState{
		{ComposeID: "c1", Name: "Plex", DeployStatus: DeployNeeded},
	}

	newModel, _ := m.Update(deployCompleteMsg{serviceIdx: 0, success: true})
	updated := newModel.(model)

	if updated.services[0].DeployStatus != DeployTriggered {
		t.Errorf("DeployStatus = %d, want DeployTriggered (%d)", updated.services[0].DeployStatus, DeployTriggered)
	}
	if updated.phase != "done" {
		t.Errorf("phase = %q, want %q after all deploys complete", updated.phase, "done")
	}
}

func TestUpdate_DeployCompleteMsg_Failure(t *testing.T) {
	m := initialModel(false)
	m.phase = "deploying"
	m.services = []ServiceState{
		{ComposeID: "c1", Name: "Plex", DeployStatus: DeployNeeded},
	}

	newModel, _ := m.Update(deployCompleteMsg{serviceIdx: 0, success: false})
	updated := newModel.(model)

	if updated.services[0].DeployStatus != DeployFailed {
		t.Errorf("DeployStatus = %d, want DeployFailed (%d)", updated.services[0].DeployStatus, DeployFailed)
	}
}

func TestUpdate_AllDoneMsg(t *testing.T) {
	m := initialModel(false)
	m.phase = "deploying"

	newModel, _ := m.Update(allDoneMsg{})
	updated := newModel.(model)

	if updated.phase != "done" {
		t.Errorf("phase = %q, want %q after allDoneMsg", updated.phase, "done")
	}
}

func TestUpdate_OutOfBoundsServiceIndex(t *testing.T) {
	m := initialModel(false)
	m.phase = "pulling"
	m.services = []ServiceState{
		{ComposeID: "c1", Name: "s1", Images: []ImageInfo{{Image: "img:latest", PullStatus: StatusPulling}}},
	}

	newModel, _ := m.Update(pullCompleteMsg{serviceIdx: 5, imageIdx: 0, updated: true})
	updated := newModel.(model)
	if updated.services[0].Images[0].PullStatus != StatusPulling {
		t.Error("out of bounds pullCompleteMsg should not affect existing images")
	}
}

func TestUpdate_OutOfBoundsImageIndex(t *testing.T) {
	m := initialModel(false)
	m.phase = "pulling"
	m.services = []ServiceState{
		{ComposeID: "c1", Name: "s1", Images: []ImageInfo{{Image: "img:latest", PullStatus: StatusPulling}}},
	}

	newModel, _ := m.Update(pullCompleteMsg{serviceIdx: 0, imageIdx: 5, updated: true})
	updated := newModel.(model)
	if updated.services[0].Images[0].PullStatus != StatusPulling {
		t.Error("out of bounds imageIdx should not affect existing images")
	}
}

// --- Cursor navigation tests ---

func TestUpdate_CursorDown(t *testing.T) {
	m := initialModel(false)
	m.width = 120
	m.height = 40
	m.phase = "pulling"
	m.services = []ServiceState{
		{Name: "svc1", Images: []ImageInfo{{Image: "img1"}}},
		{Name: "svc2", Images: []ImageInfo{{Image: "img2"}}},
	}
	m.cursor = 0

	newModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated := newModel.(model)
	if updated.cursor != 1 {
		t.Errorf("cursor after down = %d, want 1", updated.cursor)
	}
}

func TestUpdate_CursorUp(t *testing.T) {
	m := initialModel(false)
	m.width = 120
	m.height = 40
	m.phase = "pulling"
	m.services = []ServiceState{
		{Name: "svc1", Images: []ImageInfo{{Image: "img1"}}},
		{Name: "svc2", Images: []ImageInfo{{Image: "img2"}}},
	}
	m.cursor = 2

	newModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	updated := newModel.(model)
	if updated.cursor != 1 {
		t.Errorf("cursor after up = %d, want 1", updated.cursor)
	}
}

func TestUpdate_CursorDoesNotGoBelowZero(t *testing.T) {
	m := initialModel(false)
	m.cursor = 0
	m.services = []ServiceState{{Name: "svc1"}}

	newModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	updated := newModel.(model)
	if updated.cursor != 0 {
		t.Errorf("cursor should not go below 0, got %d", updated.cursor)
	}
}

func TestUpdate_CursorDoesNotExceedRows(t *testing.T) {
	m := initialModel(false)
	m.services = []ServiceState{
		{Name: "svc1", Images: []ImageInfo{{Image: "img1"}}},
	}
	// rows: header + image = 2 rows, max cursor = 1
	m.cursor = 1

	newModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated := newModel.(model)
	if updated.cursor != 1 {
		t.Errorf("cursor should not exceed max, got %d", updated.cursor)
	}
}

// --- Error display test ---

func TestView_ErrorInStatusBar(t *testing.T) {
	m := initialModel(false)
	m.width = 120
	m.height = 40
	m.phase = "done"
	m.err = fmt.Errorf("connection refused")
	view := m.View()
	if !strings.Contains(view, "connection refused") {
		t.Error("View() should display error message in status bar")
	}
}

// --- Enter key triggers deploy ---

func TestUpdate_EnterTriggersRedeploy(t *testing.T) {
	m := initialModel(false)
	m.width = 120
	m.height = 40
	m.phase = "done"
	m.services = []ServiceState{
		{ComposeID: "c1", Name: "svc1", DeployStatus: DeployNone, Images: []ImageInfo{{Image: "img1", PullStatus: StatusComplete}}},
		{ComposeID: "c2", Name: "svc2", DeployStatus: DeployNone, Images: []ImageInfo{{Image: "img2", PullStatus: StatusComplete}}},
	}
	m.cursor = 0

	newModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := newModel.(model)

	if updated.services[0].DeployStatus != DeployNeeded {
		t.Errorf("DeployStatus = %d, want DeployNeeded after Enter", updated.services[0].DeployStatus)
	}
	if updated.phase != "deploying" {
		t.Errorf("phase = %q, want %q after Enter triggers deploy", updated.phase, "deploying")
	}
	if cmd == nil {
		t.Error("Enter should return a deploy command")
	}
}

func TestUpdate_EnterOnImageRowDeploysParentService(t *testing.T) {
	m := initialModel(false)
	m.width = 120
	m.height = 40
	m.phase = "done"
	m.services = []ServiceState{
		{ComposeID: "c1", Name: "svc1", DeployStatus: DeployNone, Images: []ImageInfo{{Image: "img1", PullStatus: StatusComplete}}},
	}
	// Cursor on row 1 (image row for svc1's img1)
	m.cursor = 1

	newModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := newModel.(model)

	if updated.services[0].DeployStatus != DeployNeeded {
		t.Errorf("DeployStatus = %d, want DeployNeeded when Enter pressed on image row", updated.services[0].DeployStatus)
	}
}

// --- startNextPulls concurrency ---

func TestStartNextPulls_RespectsLimit(t *testing.T) {
	m := initialModel(false)
	m.services = []ServiceState{
		{Name: "s1", Images: []ImageInfo{{Image: "img1", PullStatus: StatusPending}}},
		{Name: "s2", Images: []ImageInfo{{Image: "img2", PullStatus: StatusPending}}},
		{Name: "s3", Images: []ImageInfo{{Image: "img3", PullStatus: StatusPending}}},
		{Name: "s4", Images: []ImageInfo{{Image: "img4", PullStatus: StatusPending}}},
		{Name: "s5", Images: []ImageInfo{{Image: "img5", PullStatus: StatusPending}}},
	}

	var cmds []tea.Cmd
	m.startNextPulls(&cmds)

	if len(cmds) != 3 {
		t.Errorf("startNextPulls produced %d commands, want 3 (concurrency limit)", len(cmds))
	}

	pulling := 0
	pending := 0
	for _, svc := range m.services {
		for _, img := range svc.Images {
			if img.PullStatus == StatusPulling {
				pulling++
			}
			if img.PullStatus == StatusPending {
				pending++
			}
		}
	}
	if pulling != 3 {
		t.Errorf("pulling count = %d, want 3", pulling)
	}
	if pending != 2 {
		t.Errorf("pending count = %d, want 2", pending)
	}
}

// --- renderHLine tests ---

func TestRenderHLine(t *testing.T) {
	// Just verify it produces output with the expected border characters
	result := renderHLine([]int{10, 5}, "╭", "┬", "╮")
	if !strings.Contains(result, "╭") || !strings.Contains(result, "╮") || !strings.Contains(result, "┬") {
		t.Errorf("renderHLine should contain border chars, got %q", result)
	}
	if !strings.Contains(result, "─") {
		t.Error("renderHLine should contain horizontal line characters")
	}
}
