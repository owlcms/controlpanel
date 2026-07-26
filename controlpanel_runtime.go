package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"controlpanel/shared"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

type controlPanelRuntimeMetadata struct {
	PID               int    `json:"pid"`
	ProcessStartTicks uint64 `json:"processStartTicks"`
	Instance          string `json:"instance"`
	Executable        string `json:"executable"`
	WorkingDir        string `json:"workingDir"`
	StartedAt         string `json:"startedAt"`
}

func controlPanelPIDPath() string {
	return filepath.Join(shared.GetControlPanelInstallDir(), "controlpanel.pid")
}

func controlPanelRuntimeMetadataPath() string {
	return filepath.Join(shared.GetControlPanelInstallDir(), "controlpanel-run.json")
}

func currentControlPanelInstanceName() string {
	instance := strings.TrimSpace(os.Getenv("CONTROLPANEL_INSTANCE"))
	if instance == "" {
		return mainInstanceName
	}
	return instance
}

func writeFileAtomically(path string, content []byte, perm os.FileMode) error {
	if err := shared.EnsureDir0755(filepath.Dir(path)); err != nil {
		return err
	}

	tempPath := path + ".tmp"
	if err := os.WriteFile(tempPath, content, perm); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	return nil
}

func writeCurrentControlPanelRuntime() error {
	pid := os.Getpid()
	startTicks, err := shared.ReadProcessStartTicks(pid)
	if err != nil {
		return fmt.Errorf("read process start ticks for control panel PID %d: %w", pid, err)
	}

	executable, err := os.Executable()
	if err != nil {
		log.Printf("Failed to resolve control panel executable: %v", err)
	}
	workingDir, err := os.Getwd()
	if err != nil {
		log.Printf("Failed to resolve control panel working directory: %v", err)
	}

	metadata := controlPanelRuntimeMetadata{
		PID:               pid,
		ProcessStartTicks: startTicks,
		Instance:          currentControlPanelInstanceName(),
		Executable:        executable,
		WorkingDir:        workingDir,
		StartedAt:         time.Now().UTC().Format(time.RFC3339),
	}

	content, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal control panel runtime metadata: %w", err)
	}
	if err := writeFileAtomically(controlPanelRuntimeMetadataPath(), content, 0644); err != nil {
		return fmt.Errorf("write control panel runtime metadata: %w", err)
	}
	if err := writeFileAtomically(controlPanelPIDPath(), fmt.Appendf(nil, "%d\n", pid), 0644); err != nil {
		return fmt.Errorf("write control panel PID file: %w", err)
	}

	log.Printf("Wrote control panel runtime metadata for PID %d", pid)
	return nil
}

func readControlPanelRuntimeMetadata() (*controlPanelRuntimeMetadata, error) {
	content, err := os.ReadFile(controlPanelRuntimeMetadataPath())
	if err != nil {
		return nil, err
	}

	var metadata controlPanelRuntimeMetadata
	if err := json.Unmarshal(content, &metadata); err != nil {
		return nil, fmt.Errorf("unmarshal control panel runtime metadata: %w", err)
	}
	return &metadata, nil
}

func readControlPanelPID() (int, error) {
	content, err := os.ReadFile(controlPanelPIDPath())
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(content)))
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("invalid control panel PID file %s", controlPanelPIDPath())
	}
	return pid, nil
}

func clearControlPanelRuntimeFiles() {
	if err := os.Remove(controlPanelPIDPath()); err != nil && !os.IsNotExist(err) {
		log.Printf("Failed to remove control panel PID file: %v", err)
	}
	if err := os.Remove(controlPanelRuntimeMetadataPath()); err != nil && !os.IsNotExist(err) {
		log.Printf("Failed to remove control panel runtime metadata: %v", err)
	}
}

func clearCurrentControlPanelRuntime() {
	currentPID := os.Getpid()
	owned := false

	if metadata, err := readControlPanelRuntimeMetadata(); err == nil && metadata.PID == currentPID {
		owned = true
	}
	if pid, err := readControlPanelPID(); err == nil && pid == currentPID {
		owned = true
	}

	if !owned {
		return
	}

	clearControlPanelRuntimeFiles()
	log.Printf("Cleared control panel runtime metadata for PID %d", currentPID)
}

func runningControlPanelMetadata() (*controlPanelRuntimeMetadata, bool) {
	if metadata, err := readControlPanelRuntimeMetadata(); err == nil {
		if metadata.PID > 0 && metadata.PID != os.Getpid() && shared.PIDMatchesStartTicks(metadata.PID, metadata.ProcessStartTicks) {
			return metadata, true
		}
		log.Printf("Control panel runtime metadata is stale for PID %d", metadata.PID)
	} else if !os.IsNotExist(err) {
		log.Printf("Failed to read control panel runtime metadata: %v", err)
	}

	pid, err := readControlPanelPID()
	if err == nil && pid > 0 && pid != os.Getpid() && shared.IsProcessRunning(pid) {
		startTicks, _ := shared.ReadProcessStartTicks(pid)
		return &controlPanelRuntimeMetadata{
			PID:               pid,
			ProcessStartTicks: startTicks,
			Instance:          currentControlPanelInstanceName(),
		}, true
	} else if err != nil && !os.IsNotExist(err) {
		log.Printf("Failed to read control panel PID file: %v", err)
	}

	clearControlPanelRuntimeFiles()
	return nil, false
}

func stopRunningControlPanel(metadata *controlPanelRuntimeMetadata) error {
	if metadata == nil || metadata.PID <= 0 {
		return fmt.Errorf("invalid running control panel metadata")
	}
	if metadata.PID == os.Getpid() {
		return fmt.Errorf("refusing to stop current control panel PID %d", metadata.PID)
	}

	if err := shared.GracefullyStopPID(metadata.PID); err != nil {
		return err
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !shared.IsProcessRunning(metadata.PID) {
			clearControlPanelRuntimeFiles()
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}

	return fmt.Errorf("control panel PID %d is still running", metadata.PID)
}

func describeControlPanelRuntime(metadata *controlPanelRuntimeMetadata) string {
	parts := []string{fmt.Sprintf("PID %d", metadata.PID)}
	if strings.TrimSpace(metadata.Instance) != "" {
		parts = append(parts, "instance "+metadata.Instance)
	}
	if strings.TrimSpace(metadata.StartedAt) != "" {
		parts = append(parts, "started "+metadata.StartedAt)
	}
	return strings.Join(parts, ", ")
}

func startControlPanelRuntimeGate(a fyne.App, w fyne.Window, initialWindowSize fyne.Size, start func()) bool {
	running, ok := runningControlPanelMetadata()
	if !ok {
		if err := writeCurrentControlPanelRuntime(); err != nil {
			log.Printf("Failed to write control panel runtime metadata: %v", err)
			fmt.Fprintf(os.Stderr, "control panel runtime: %v\n", err)
			return false
		}
		start()
		return true
	}

	message := fmt.Sprintf(
		"Another OWLCMS Control Panel is already running (%s).\n\nStop the running Control Panel and continue?",
		describeControlPanelRuntime(running),
	)
	w.Resize(initialWindowSize)
	w.SetContent(container.NewCenter(widget.NewLabel("Another OWLCMS Control Panel is already running.")))
	w.Show()

	confirmDialog := dialog.NewConfirm(
		"Control Panel Already Running",
		message,
		func(confirm bool) {
			if !confirm {
				a.Quit()
				return
			}

			go func() {
				log.Printf("Stopping existing control panel %s", describeControlPanelRuntime(running))
				if err := stopRunningControlPanel(running); err != nil {
					log.Printf("Failed to stop existing control panel: %v", err)
					fyne.Do(func() {
						dialog.ShowError(fmt.Errorf("failed to stop running Control Panel: %w", err), w)
					})
					return
				}

				if err := writeCurrentControlPanelRuntime(); err != nil {
					log.Printf("Failed to write control panel runtime metadata: %v", err)
					fyne.Do(func() {
						dialog.ShowError(fmt.Errorf("failed to record this Control Panel process: %w", err), w)
					})
					return
				}

				fyne.Do(start)
			}()
		},
		w,
	)
	confirmDialog.SetConfirmText("Stop Existing and Continue")
	confirmDialog.SetDismissText("Cancel")
	confirmDialog.Show()
	return true
}
