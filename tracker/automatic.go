package tracker

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"controlpanel/shared"
	"controlpanel/shared/portowner"

	"fyne.io/fyne/v2"
)

// LocalConnection describes the configuration actually used by a local Tracker.
type LocalConnection struct {
	Version string
	Port    string
	Key     string
	process *exec.Cmd
	runtime *shared.RuntimeMetadata
	stopMu  sync.Mutex
	stopped bool
}

var automaticStarting atomic.Bool

// LatestLocalConnection returns the latest installed Tracker's effective connection settings.
func LatestLocalConnection() (*LocalConnection, error) {
	versions := GetAllInstalledVersions()
	if len(versions) == 0 {
		return nil, fmt.Errorf("install a Tracker version before enabling automatic launch")
	}
	version := versions[0]
	props, err := loadEnvironmentForReleaseProps(version)
	if err != nil {
		return nil, fmt.Errorf("load Tracker %s configuration: %w", version, err)
	}
	port, ok := props.Get("TRACKER_PORT")
	if !ok || strings.TrimSpace(port) == "" {
		port = "8096"
	}
	port = strings.TrimSpace(port)
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return nil, fmt.Errorf("Tracker %s has invalid port %q", version, port)
	}
	key, present := os.LookupEnv(sharedKeyEnv)
	if !present {
		value, _ := props.Get(sharedKeyEnv)
		key, err = shared.DecryptSecret(value)
		if err != nil {
			return nil, fmt.Errorf("read Tracker %s shared key: %w", version, err)
		}
	}
	return &LocalConnection{Version: version, Port: port, Key: key}, nil
}

// StartLatestForOwlcms starts or adopts the latest Tracker; a Tracker it starts opens in the browser.
// It must run on a worker; its returned handle stops exactly the selected process.
func StartLatestForOwlcms() (*LocalConnection, error) {
	if !automaticStarting.CompareAndSwap(false, true) {
		return nil, fmt.Errorf("Tracker is already starting")
	}
	defer automaticStarting.Store(false)
	versions := GetAllInstalledVersions()
	if len(versions) == 0 {
		return nil, fmt.Errorf("install a Tracker version before enabling automatic launch")
	}
	config := &LocalConnection{Version: versions[0]}
	runtime, running := shared.CheckDaemonRunning(runtimeMetadataPath())
	if running {
		if runtime.Version != config.Version {
			return nil, fmt.Errorf("Tracker %s is running; stop it before launching latest Tracker %s", runtime.Version, config.Version)
		}
		var env []string
		var cmd *exec.Cmd
		if currentProcess != nil && currentProcess.Process != nil && currentProcess.Process.Pid == runtime.PID {
			cmd = currentProcess
			env = cmd.Env
		} else {
			env = portowner.Environ(int32(runtime.PID))
			if env == nil {
				return nil, fmt.Errorf("cannot read the running Tracker's configuration; stop and restart Tracker from the control panel")
			}
		}
		key := ""
		for _, entry := range env {
			if value, ok := strings.CutPrefix(entry, sharedKeyEnv+"="); ok {
				key = value
				break
			}
		}
		deadline := time.Now().Add(30 * time.Second)
		for shared.CheckPort(runtime.Port) != nil {
			if !shared.PIDMatchesStartTicks(runtime.PID, runtime.ProcessStartTicks) {
				return nil, fmt.Errorf("Tracker %s exited before becoming ready", runtime.Version)
			}
			if time.Now().After(deadline) {
				return nil, fmt.Errorf("timed out waiting for running Tracker %s on port %s", runtime.Version, runtime.Port)
			}
			time.Sleep(250 * time.Millisecond)
		}
		config.Port, config.Key, config.process, config.runtime = runtime.Port, key, cmd, runtime
		fyne.DoAndWait(func() {
			if activeRuntime != nil && activeRuntime.PID == runtime.PID {
				activeRuntime.Daemon = false
			}
		})
		return config, nil
	}
	config, err := LatestLocalConnection()
	if err != nil {
		return nil, err
	}
	if pid, _, err := shared.ResolvePIDFromFileOrPort(pidFilePath, config.Port); err != nil {
		return nil, err
	} else if pid != 0 {
		return nil, fmt.Errorf("process %d is already using Tracker's PID file or port %s; stop it before launching Tracker", pid, config.Port)
	}

	params, err := prepareTrackerLaunch(config.Version)
	if err != nil {
		return nil, err
	}
	if params.KeyWarning != "" {
		return nil, fmt.Errorf("%s", params.KeyWarning)
	}
	params.Env = shared.UpsertEnv(shared.RemoveEnv(params.Env, "PORT"), "PORT", params.TargetPort)
	node, err := shared.FindLocalNodeForVersion(params.RequiredNodeVer, shared.GetGoos)
	if err != nil {
		version := params.RequiredNodeVer
		if version == "" {
			version, err = shared.FindLatestNodeRelease("")
			if err != nil {
				return nil, fmt.Errorf("find Node.js release: %w", err)
			}
		}
		node, err = shared.DownloadAndInstallNode(version, nil)
		if err != nil {
			return nil, fmt.Errorf("install Node.js: %w", err)
		}
	}
	logPath := filepath.Join(params.VersionDir, "logs", "tracker.log")
	if err := shared.EnsureDir0755(filepath.Dir(logPath)); err != nil {
		return nil, fmt.Errorf("create Tracker log directory: %w", err)
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return nil, fmt.Errorf("open Tracker log: %w", err)
	}
	cmd := exec.Command(node, params.ScriptToRun)
	shared.ConfigureNoConsoleWindow(cmd)
	cmd.Dir, cmd.Env, cmd.Stdout, cmd.Stderr = params.VersionDir, params.Env, logFile, logFile
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return nil, fmt.Errorf("start Tracker %s: %w", config.Version, err)
	}
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
		logFile.Close()
	}()
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			return nil, fmt.Errorf("Tracker %s exited before becoming ready: %v", config.Version, err)
		case <-deadline.C:
			if err := shared.StopOwnedProcess(cmd, 10*time.Second); err != nil {
				return nil, fmt.Errorf("Tracker %s startup timed out and stopping it failed: %w", config.Version, err)
			}
			return nil, fmt.Errorf("timed out waiting for Tracker %s on port %s", config.Version, params.TargetPort)
		case <-ticker.C:
			if shared.CheckPort(params.TargetPort) != nil {
				continue
			}
			config.Port = params.TargetPort
			config.process = cmd
			config.runtime = recordTrackerStart(cmd.Process.Pid, config.Version, config.Port, false)
			if config.runtime == nil {
				if err := shared.StopOwnedProcess(cmd, 10*time.Second); err != nil {
					return nil, fmt.Errorf("Tracker runtime metadata could not be saved and stopping Tracker failed: %w", err)
				}
				return nil, fmt.Errorf("Tracker runtime metadata could not be saved")
			}
			fyne.DoAndWait(func() {
				currentProcess = cmd
				activeRuntime = config.runtime
				restoreTrackerRunningUI(config.Version, config.Port, cmd.Process.Pid)
			})
			if err := shared.OpenBrowser(fmt.Sprintf("http://localhost:%s", config.Port)); err != nil {
				log.Printf("Failed to open browser: %v", err)
			}
			go func() {
				err := <-done
				log.Printf("Tracker %s exited: %v", config.Version, err)
				fyne.Do(func() {
					if currentProcess != cmd {
						return
					}
					currentProcess = nil
					clearRuntimeState()
					releaseTrackerLock()
					stopButton.Hide()
					stopContainer.Hide()
					urlLink.Hide()
					appDirLink.Hide()
					tailLogLink.Hide()
					statusLabel.SetText(fmt.Sprintf("Tracker %s stopped", config.Version))
					setTrackerTabMode(mainWindow)
				})
			}()
			return config, nil
		}
	}
}

// Stop stops the Tracker associated with this OWLCMS launch, never another process on its port.
func (c *LocalConnection) Stop() error {
	if c == nil {
		return nil
	}
	c.stopMu.Lock()
	defer c.stopMu.Unlock()
	if c.stopped || c.runtime == nil || !shared.PIDMatchesStartTicks(c.runtime.PID, c.runtime.ProcessStartTicks) {
		c.stopped = true
		return nil
	}
	var err error
	if c.process != nil {
		err = shared.StopOwnedProcess(c.process, 10*time.Second)
	} else {
		err = shared.GracefullyStopPID(c.runtime.PID)
	}
	if err == nil {
		c.stopped = true
	}
	return err
}
