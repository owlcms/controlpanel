package main

import (
	"fmt"
	"image/color"
	"io"
	"log"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"controlpanel/cameras"
	"controlpanel/firmata"
	"controlpanel/owlcms"
	"controlpanel/owlcms/javacheck"
	"controlpanel/replays"
	"controlpanel/shared"
	"controlpanel/tracker"
	"controlpanel/video"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

var exitInProgress bool
var controlPanelLogPath string

const (
	moduleOWLCMSPreference  = "modules.owlcms.visible"
	moduleTrackerPreference = "modules.tracker.visible"
	moduleFirmataPreference = "modules.firmata.visible"
	moduleVideoPreference   = "modules.video.visible"
	moduleCamerasPreference = "modules.cameras.visible"
	moduleReplaysPreference = "modules.replays.visible"
)

type moduleVisibility struct {
	owlcms  bool
	tracker bool
	firmata bool
	video   bool
	cameras bool
	replays bool
}

type myTheme struct {
	fyne.Theme
}

var checkboxOutlineIcon = fyne.NewStaticResource("checkbox-outline.svg", []byte(`
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">
	<rect x="2.5" y="2.5" width="19" height="19" rx="2" fill="none" stroke="currentColor" stroke-width="0.5"/>
</svg>`))

func newMyTheme() *myTheme {
	return &myTheme{Theme: theme.DefaultTheme()}
}

func (m myTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	if name == theme.ColorNameBackground {
		return color.White
	}
	if name == theme.ColorNameForeground {
		return color.Black
	}
	if name == theme.ColorNameShadow {
		return color.NRGBA{R: 0, G: 0, B: 0, A: 40}
	}
	if name == theme.ColorNameSuccess {
		// Much darker green for Firmata buttons
		return color.RGBA{R: 15, G: 80, B: 15, A: 255}
	}
	if name == theme.ColorNameError {
		// Much darker red for Firmata buttons
		return color.RGBA{R: 100, G: 10, B: 10, A: 255}
	}
	// The panel always renders with a light background, so resolve remaining
	// colors against the light variant regardless of the user preference.
	return m.Theme.Color(name, theme.VariantLight)
}

func (m myTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	if name == theme.IconNameCheckButton {
		return checkboxOutlineIcon
	}
	return m.Theme.Icon(name)
}

func getInstanceIdentity() (string, string) {
	instance := strings.TrimSpace(os.Getenv("CONTROLPANEL_INSTANCE"))
	if instance == "" {
		return "app.owlcms.controlpanel", "OWLCMS Control Panel"
	}

	return "app.owlcms.controlpanel." + instance, fmt.Sprintf("OWLCMS Control Panel (%s)", instance)
}

func hasInstalledVersions(installDir string) bool {
	entries, err := os.ReadDir(installDir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return true
		}
	}
	return false
}

func loadModuleVisibility(preferences fyne.Preferences, defaults moduleVisibility) moduleVisibility {
	return moduleVisibility{
		owlcms:  preferences.BoolWithFallback(moduleOWLCMSPreference, defaults.owlcms),
		tracker: preferences.BoolWithFallback(moduleTrackerPreference, defaults.tracker),
		firmata: preferences.BoolWithFallback(moduleFirmataPreference, defaults.firmata),
		video:   preferences.BoolWithFallback(moduleVideoPreference, defaults.video),
		cameras: preferences.BoolWithFallback(moduleCamerasPreference, defaults.cameras),
		replays: preferences.BoolWithFallback(moduleReplaysPreference, defaults.replays),
	}
}

func saveModuleVisibility(preferences fyne.Preferences, visibility moduleVisibility) {
	preferences.SetBool(moduleOWLCMSPreference, visibility.owlcms)
	preferences.SetBool(moduleTrackerPreference, visibility.tracker)
	preferences.SetBool(moduleFirmataPreference, visibility.firmata)
	preferences.SetBool(moduleVideoPreference, visibility.video)
	preferences.SetBool(moduleCamerasPreference, visibility.cameras)
	preferences.SetBool(moduleReplaysPreference, visibility.replays)
}

func main() {
	args := os.Args[1:]
	cliOptions := parseCLIOptions(args)
	moduleCommand, moduleCommandFound, moduleCommandErr := parseModuleCommand(args)
	if cliOptions.help {
		printUsage()
		return
	}
	if moduleCommandErr != nil {
		fmt.Fprintf(os.Stderr, "command line: %v\n", moduleCommandErr)
		os.Exit(1)
	}
	if err := applyCLIInstanceOptions(cliOptions); err != nil {
		fmt.Fprintf(os.Stderr, "instance setup: %v\n", err)
		os.Exit(1)
	}
	if cliOptions.init {
		initializedInstance := strings.TrimSpace(os.Getenv("CONTROLPANEL_INSTANCE"))
		if initializedInstance == "" {
			initializedInstance = strings.TrimSpace(cliOptions.instanceArg)
		}
		fmt.Printf("Initialized instance %q\n", initializedInstance)
		fmt.Printf("control panel dir: %s\n", shared.GetControlPanelInstallDir())
		fmt.Printf("owlcms dir:        %s\n", shared.GetOwlcmsInstallDir())
		fmt.Printf("tracker dir:       %s\n", shared.GetTrackerInstallDir())
		fmt.Printf("runtime dir:       %s\n", shared.GetRuntimeDir())
		return
	}

	javacheck.InitJavaCheck(shared.GetOwlcmsInstallDir(), owlcms.GetTemurinVersion)

	// Set up logging to file (and stderr if available)
	controlPanelDir := shared.GetControlPanelInstallDir()
	_ = shared.EnsureDir0755(controlPanelDir) // ignore error, can't log yet
	controlPanelLogPath = filepath.Join(controlPanelDir, "control-panel.log")
	logFile, err := os.OpenFile(controlPanelLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		// Fall back to stderr only if file open fails
		log.SetOutput(shared.NewLogPathShorteningWriter(os.Stderr))
	} else {
		// On Windows GUI apps without console, stderr may be invalid
		// Test if stderr is usable by checking if Stat succeeds
		stderrUsable := false
		if fi, err := os.Stderr.Stat(); err == nil && fi != nil {
			stderrUsable = true
		}
		if stderrUsable {
			multiWriter := io.MultiWriter(os.Stderr, logFile)
			log.SetOutput(shared.NewLogPathShorteningWriter(multiWriter))
		} else {
			log.SetOutput(shared.NewLogPathShorteningWriter(logFile))
		}
	}
	log.SetFlags(log.LstdFlags | log.Lmicroseconds | log.Llongfile)
	log.Printf("Starting OWLCMS Control Panel %s", shared.GetLauncherVersion())

	if moduleCommandFound {
		moduleCommand.MQTT = moduleCommand.MQTT || cliOptions.mqtt
		if moduleCommandRequiresExclusiveControlPanel(moduleCommand) {
			if running, ok := runningControlPanelMetadata(); ok {
				err := fmt.Errorf("another OWLCMS Control Panel is already running (%s); use that UI or close it before running module commands", describeControlPanelRuntime(running))
				log.Printf("ERROR: %v", err)
				fmt.Fprintf(os.Stderr, "%v\n", err)
				os.Exit(1)
			}
		}
		if err := executeModuleCommand(moduleCommand, os.Stdout); err != nil {
			log.Printf("ERROR: %v", err)
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
		return
	}

	configureProcessDPIAwareness()

	shared.NormalizeLocaleEnvironment()

	appID, windowTitle := getInstanceIdentity()
	a := app.NewWithID(appID)
	a.Settings().SetTheme(newMyTheme())
	w := a.NewWindow(windowTitle)
	initialWindowSize := fyne.NewSize(950, 600)
	if !startControlPanelRuntimeGate(a, w, initialWindowSize, func() {
		startControlPanelUI(w, a, initialWindowSize)
	}) {
		return
	}
	defer clearCurrentControlPanelRuntime()
	a.Run()
}

func startControlPanelUI(w fyne.Window, a fyne.App, initialWindowSize fyne.Size) {
	// Create tab contents - owlcms.CreateTab handles its own initialization
	owlcmsTabContent := owlcms.CreateTab(w, a)
	trackerTabContent := tracker.CreateTab(w)
	firmataTabContent := firmata.CreateTab(w)
	videoTabContent := video.CreateTab(w)
	camerasTabContent := cameras.CreateTab(w)
	replaysTabContent := replays.CreateTab(w)
	owlcmsTab := container.NewTabItem("OWLCMS", owlcmsTabContent)
	trackerTab := container.NewTabItem("Tracker", trackerTabContent)
	firmataTab := container.NewTabItem("Arduino Devices", firmataTabContent)
	videoTab := container.NewTabItem("Video", videoTabContent)
	camerasTab := container.NewTabItem("Cameras", camerasTabContent)
	replaysTab := container.NewTabItem("Replays", replaysTabContent)
	mainContent := container.NewAppTabs()

	// Refresh tracker version list when its tab is selected (to update OWLCMS version warning)
	mainContent.OnSelected = func(tab *container.TabItem) {
		if tab.Text == "Tracker" {
			tracker.OnTabSelected()
		}
	}

	selection := loadModuleVisibility(a.Preferences(), moduleVisibility{
		owlcms:  true,
		tracker: true,
		firmata: hasInstalledVersions(firmata.GetInstallDir()),
		video:   hasInstalledVersions(video.GetInstallDir()),
		cameras: hasInstalledVersions(cameras.GetInstallDir()),
		replays: hasInstalledVersions(replays.GetInstallDir()),
	})
	var modulesMenu *fyne.Menu
	refreshTabs := func() {
		visible := make([]*container.TabItem, 0, 6)
		if selection.owlcms {
			visible = append(visible, owlcmsTab)
		}
		if selection.tracker {
			visible = append(visible, trackerTab)
		}
		if selection.firmata {
			visible = append(visible, firmataTab)
		}
		if selection.video {
			visible = append(visible, videoTab)
		}
		if selection.cameras {
			visible = append(visible, camerasTab)
		}
		if selection.replays {
			visible = append(visible, replaysTab)
		}
		if len(visible) == 0 {
			selection.owlcms = true
			visible = append(visible, owlcmsTab)
		}
		mainContent.SetItems(visible)
		mainContent.Select(visible[0])
		for _, item := range modulesMenu.Items {
			switch item.Label {
			case "OWLCMS":
				item.Checked = selection.owlcms
			case "Tracker":
				item.Checked = selection.tracker
			case "Arduino Devices":
				item.Checked = selection.firmata
			case "Video":
				item.Checked = selection.video
			case "Cameras":
				item.Checked = selection.cameras
			case "Replays":
				item.Checked = selection.replays
			}
		}
		modulesMenu.Refresh()
	}
	newModuleItem := func(label string, selected *bool) *fyne.MenuItem {
		return fyne.NewMenuItem(label, func() {
			*selected = !*selected
			refreshTabs()
			saveModuleVisibility(a.Preferences(), selection)
		})
	}
	modulesMenu = fyne.NewMenu("Modules",
		newModuleItem("OWLCMS", &selection.owlcms),
		newModuleItem("Tracker", &selection.tracker),
		newModuleItem("Arduino Devices", &selection.firmata),
		newModuleItem("Video", &selection.video),
		newModuleItem("Cameras", &selection.cameras),
		newModuleItem("Replays", &selection.replays),
	)
	refreshTabs()

	// Create dummy/loading content with the correct sizing to anchor window dimensions upfront
	dummyBackground := canvas.NewRectangle(color.Transparent)
	dummyBackground.SetMinSize(initialWindowSize)
	dummyLabel := widget.NewLabelWithStyle("Loading OWLCMS modules...", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	dummyContent := container.NewStack(dummyBackground, dummyLabel)

	// Setup menus before content so Fyne initializes the menu canvas before
	// the first content layout pass.
	mainMenu := setupMenus(w, modulesMenu)

	// Combine into a stack so that dummyContent is initially visible, layout doesn't break,
	// and we avoid swapping the root content in a way that causes rendering failures.
	mainContent.Hide()
	rootContainer := container.NewStack(dummyContent, mainContent)
	var rootContent fyne.CanvasObject = rootContainer
	if shared.GetGoos() == "darwin" {
		rootContent = container.NewBorder(createInWindowMainMenu(mainMenu), nil, nil, nil, rootContainer)
	}
	w.SetContent(rootContent)

	// Show installed modules popup
	// Query the actual install directories from each package
	owlp := owlcms.GetInstallDir()
	tp := tracker.GetInstallDir()
	fp := firmata.GetInstallDir()
	vp := video.GetInstallDir()

	mods := shared.DetectInstalledModules(owlp, tp, fp, vp)

	owlcmsCheck := widget.NewCheck("OWLCMS", nil)
	owlcmsCheck.SetChecked(mods.OWLCMS)
	trackerCheck := widget.NewCheck("Tracker", nil)
	trackerCheck.SetChecked(mods.Tracker)
	firmataCheck := widget.NewCheck("Firmata", nil)
	firmataCheck.SetChecked(mods.Firmata)
	videoCheck := widget.NewCheck("Video", nil)
	videoCheck.SetChecked(mods.Video)
	checks := container.NewVBox(owlcmsCheck, trackerCheck, firmataCheck, videoCheck)

	// The initial installed-modules popup is kept in code for future use
	// but is not shown by default. To re-enable, call `dialog.ShowCustom(...)`.
	_ = checks // keep variable referenced

	// Set up signal handling
	setupSignalHandling()

	// Setup cleanup on exit
	setupCleanupOnExit(w)

	setInitialWindowGeometry(w, initialWindowSize)
	w.Show()
	// Re-apply Resize after Show so that GLFW/Fyne can compute window geometry with resolved DPI scaling
	w.Resize(initialWindowSize)

	// Keep dummy content visible for a brief moment to let OS DPI scaling/framebuffer sync settle,
	// then transition smoothly to main tab menu without causing layout artifacts
	time.AfterFunc(150*time.Millisecond, func() {
		fyne.Do(func() {
			dummyContent.Hide()
			mainContent.Show()
			rootContainer.Refresh()
		})
	})

	// Check for updates after the main window is shown and sized
	go owlcms.CheckForUpdates(w, false)
	scheduleStartupRepaint(w)
	scheduleWindowDiagnostics(w)
}

func setInitialWindowGeometry(w fyne.Window, size fyne.Size) {
	// This is the earliest point where Fyne has both the real main menu and
	// the real root content, so Resize can record the intended canvas geometry
	// before Show starts GLFW native window creation.
	logFyneWindowDiagnostics("before-initial-geometry", w)
	w.Resize(size)
	logFyneWindowDiagnostics("after-initial-geometry", w)
}

func scheduleStartupRepaint(w fyne.Window) {
	for _, delay := range []time.Duration{250 * time.Millisecond, time.Second} {
		time.AfterFunc(delay, func() {
			fyne.Do(func() {
				log.Printf("window diagnostics [startup-repaint-%s]: refreshing Fyne content and native window", delay)
				if content := w.Content(); content != nil {
					content.Refresh()
					w.Canvas().Refresh(content)
				}
				forceNativeWindowRedraw(w)
				logWindowDiagnostics(fmt.Sprintf("startup-repaint-%s", delay), w)
			})
		})
	}
}

func logFyneWindowDiagnostics(label string, w fyne.Window) {
	canvasSize := w.Canvas().Size()
	contentSize := fyne.Size{}
	contentPos := fyne.Position{}
	if content := w.Content(); content != nil {
		contentSize = content.Size()
		contentPos = content.Position()
	}
	menuCount := 0
	if menu := w.MainMenu(); menu != nil {
		menuCount = len(menu.Items)
	}
	log.Printf(
		"window diagnostics [%s]: fyne canvas=%0.2fx%0.2f contentPos=%0.2f,%0.2f content=%0.2fx%0.2f contentBR=%0.2f,%0.2f mainMenuItems=%d",
		label,
		canvasSize.Width,
		canvasSize.Height,
		contentPos.X,
		contentPos.Y,
		contentSize.Width,
		contentSize.Height,
		contentPos.X+contentSize.Width,
		contentPos.Y+contentSize.Height,
		menuCount,
	)
}

func scheduleWindowDiagnostics(w fyne.Window) {
	for _, delay := range []time.Duration{250 * time.Millisecond, time.Second, 3 * time.Second} {
		time.AfterFunc(delay, func() {
			fyne.Do(func() {
				logWindowDiagnostics(fmt.Sprintf("post-show-%s", delay), w)
			})
		})
	}
}

func anyDaemonRunning() bool {
	return owlcms.IsRecoveredDaemonRunning() || tracker.IsRecoveredDaemonRunning()
}

func closableProgramNames() []string {
	programs := make([]string, 0, 6)
	if owlcms.IsLocalProcessRunning() {
		programs = append(programs, "OWLCMS")
	}
	if tracker.IsLocalProcessRunning() {
		programs = append(programs, "Tracker")
	}
	if firmata.IsRunning() {
		programs = append(programs, "Firmata")
	}
	if cameras.IsRunning() {
		programs = append(programs, "Cameras")
	}
	if replays.IsRunning() {
		programs = append(programs, "Replays")
	}
	if video.IsRunning() {
		programs = append(programs, "Video")
	}
	return programs
}

func recoveredDaemonProgramNames() []string {
	programs := make([]string, 0, 2)
	if owlcms.IsRecoveredDaemonRunning() {
		programs = append(programs, "OWLCMS")
	}
	if tracker.IsRecoveredDaemonRunning() {
		programs = append(programs, "Tracker")
	}
	return programs
}

func anyClosableProgramRunning() bool {
	return len(closableProgramNames()) > 0
}

func joinProgramNames(programs []string) string {
	switch len(programs) {
	case 0:
		return ""
	case 1:
		return programs[0]
	case 2:
		return programs[0] + " and " + programs[1]
	default:
		return strings.Join(programs[:len(programs)-1], ", ") + ", and " + programs[len(programs)-1]
	}
}

func stopClosableRunningProcesses(w fyne.Window) {
	if owlcms.IsLocalProcessRunning() {
		owlcms.StopRunningProcess(w)
	}
	if tracker.IsLocalProcessRunning() {
		tracker.StopRunningProcess(w)
	}
	firmata.StopRunningProcess(w)
	cameras.StopRunningProcess(w)
	replays.StopRunningProcess(w)
	video.StopRunningProcess(w)
}

func stopClosableRunningProcessesForSignal() {
	if owlcms.IsLocalProcessRunning() {
		log.Println("Signal cleanup: forcefully stopping OWLCMS process")
		owlcms.HandleSignalCleanup()
	}
	if tracker.IsLocalProcessRunning() {
		log.Println("Signal cleanup: forcefully stopping Tracker process")
		tracker.HandleSignalCleanup()
	}
	if firmata.IsRunning() {
		log.Println("Signal cleanup: forcefully stopping Firmata process")
		firmata.HandleSignalCleanup()
	}
	if cameras.IsRunning() {
		log.Println("Signal cleanup: forcefully stopping Cameras process")
		cameras.HandleSignalCleanup()
	}
	if replays.IsRunning() {
		log.Println("Signal cleanup: forcefully stopping Replays process")
		replays.HandleSignalCleanup()
	}
	if video.IsRunning() {
		log.Println("Signal cleanup: forcefully stopping Video process")
		video.HandleSignalCleanup()
	}
}

func cleanupJavaVersions(w fyne.Window) {
	dialog.ShowConfirm(
		"Cleanup Java Versions",
		"This will:\n• Scan env.properties files to find the highest required Java version\n• Download and install that version if not present\n• Remove all older Java versions from the control panel\n• Remove all legacy Java installations from owlcms and firmata directories\n\nContinue?",
		func(confirm bool) {
			if !confirm {
				return
			}

			// Create a status label for progress updates
			statusLabel := widget.NewLabel("Scanning for Java versions...")
			progressDialog := dialog.NewCustom("Cleaning Up Java", "Close", statusLabel, w)
			progressDialog.Show()

			// Run cleanup in goroutine to allow UI updates
			go func() {
				// Pass legacy directories for bundled cleanup, but scanning happens in control panel
				removed, err := shared.CleanupObsoleteJavaVersions(owlcms.GetInstallDir(), firmata.GetInstallDir(), statusLabel, w)
				progressDialog.Hide()

				if err != nil {
					dialog.ShowError(fmt.Errorf("cleanup failed: %w", err), w)
					return
				}

				if len(removed) == 0 {
					dialog.ShowInformation("Cleanup Complete", "No obsolete Java versions found.", w)
				} else {
					var message strings.Builder
					message.WriteString("Cleanup results:\n\n")
					for _, v := range removed {
						message.WriteString("• ")
						message.WriteString(v)
						message.WriteString("\n")
					}
					dialog.ShowInformation("Cleanup Complete", message.String(), w)
				}
			}()
		},
		w,
	)
}

func cleanupNodeVersions(w fyne.Window) {
	dialog.ShowConfirm(
		"Cleanup Node.js Versions",
		"This will:\n• Keep only the latest Node.js version in the control panel\n• Remove all older Node.js versions from the control panel\n• Remove all bundled Node.js from tracker version directories\n\nContinue?",
		func(confirm bool) {
			if !confirm {
				return
			}

			// Create a status label for progress updates
			statusLabel := widget.NewLabel("Scanning for Node.js versions...")
			progressDialog := dialog.NewCustom("Cleaning Up Node.js", "Close", statusLabel, w)
			progressDialog.Show()

			// Run cleanup in goroutine to allow UI updates
			go func() {
				removed, err := shared.CleanupObsoleteNodeVersions(statusLabel, w)
				progressDialog.Hide()

				if err != nil {
					dialog.ShowError(fmt.Errorf("cleanup failed: %w", err), w)
					return
				}

				if len(removed) == 0 {
					dialog.ShowInformation("Cleanup Complete", "No obsolete Node.js versions found.", w)
				} else {
					var message strings.Builder
					message.WriteString("Cleanup results:\n\n")
					for _, v := range removed {
						message.WriteString("• ")
						message.WriteString(v)
						message.WriteString("\n")
					}
					dialog.ShowInformation("Cleanup Complete", message.String(), w)
				}
			}()
		},
		w,
	)
}

func requestExit(w fyne.Window) {
	if exitInProgress {
		return
	}

	daemonPrograms := recoveredDaemonProgramNames()
	closablePrograms := closableProgramNames()
	message := "Exit the Control Panel?"
	confirmText := "Exit"
	if len(daemonPrograms) > 0 && len(closablePrograms) > 0 {
		message = fmt.Sprintf("%s will continue running in the background. Closing the Control Panel will stop %s. Exiting may affect users.", joinProgramNames(daemonPrograms), joinProgramNames(closablePrograms))
		confirmText = "Stop and Exit"
	} else if len(daemonPrograms) > 0 {
		message = fmt.Sprintf("%s will continue running in the background.", joinProgramNames(daemonPrograms))
	} else if len(closablePrograms) > 0 {
		message = fmt.Sprintf("Closing the Control Panel will stop %s. Exiting may affect users.", joinProgramNames(closablePrograms))
		confirmText = "Stop and Exit"
	}

	confirmDialog := dialog.NewConfirm(
		"Confirm Exit",
		message,
		func(confirm bool) {
			if !confirm {
				return
			}

			exitInProgress = true
			if len(closablePrograms) > 0 {
				log.Printf("Exiting launcher - stopping %s", joinProgramNames(closablePrograms))
				stopClosableRunningProcesses(w)
			}
			if len(daemonPrograms) > 0 {
				log.Printf("Exiting launcher - leaving %s running in background", joinProgramNames(daemonPrograms))
			}

			// Avoid re-triggering our close intercept (especially when Quit is from the menu).
			w.SetCloseIntercept(nil)
			w.Close()
		},
		w,
	)
	confirmDialog.SetConfirmText(confirmText)
	confirmDialog.SetDismissText("Cancel")
	confirmDialog.Show()
}

// setupMenus sets up the application menu bar.
func setupMenus(w fyne.Window, modulesMenu *fyne.Menu) *fyne.MainMenu {
	// Use "Quit" on all platforms - Fyne checks for this exact label
	// and won't add its own duplicate if it finds one.
	fileMenu := fyne.NewMenu(
		"File",
		fyne.NewMenuItem("Open Control Panel Installation Directory", func() {
			controlPanelDir := shared.GetControlPanelInstallDir()
			if err := shared.OpenFile(controlPanelDir); err != nil {
				dialog.ShowError(fmt.Errorf("failed to open directory: %w", err), w)
			}
		}),
		// fyne.NewMenuItem("Cleanup Obsolete Java Versions", func() {
		// 	cleanupJavaVersions(w)
		// }),
		// fyne.NewMenuItem("Cleanup Obsolete Node.js Versions", func() {
		// 	cleanupNodeVersions(w)
		// }),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Refresh", func() {
			owlcms.RefreshVersionList(w)
			tracker.RefreshVersionList(w)
			firmata.RefreshVersionList(w)
			video.RefreshVersionList(w)
			cameras.RefreshVersionList(w)
			replays.RefreshVersionList(w)
		}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Quit", func() {
			requestExit(w)
		}),
	)
	helpMenu := fyne.NewMenu("Help",
		fyne.NewMenuItem("Documentation", func() {
			linkURL, _ := url.Parse("https://owlcms.github.io/owlcms4-prerelease/#/LocalControlPanel")
			link := widget.NewHyperlink("Control Panel Documentation", linkURL)
			dialog.ShowCustom("Documentation", "Close", link, w)
		}),
		fyne.NewMenuItem("Show Control Panel Log", func() {
			if controlPanelLogPath == "" {
				dialog.ShowError(fmt.Errorf("log file path not available"), w)
				return
			}
			if _, err := os.Stat(controlPanelLogPath); os.IsNotExist(err) {
				dialog.ShowError(fmt.Errorf("log file does not exist: %s", controlPanelLogPath), w)
				return
			}
			if err := shared.OpenFile(controlPanelLogPath); err != nil {
				dialog.ShowError(fmt.Errorf("failed to open log file: %w", err), w)
			}
		}),
		fyne.NewMenuItem("Check for Updates", func() {
			// Show confirmation dialog when checking from menu
			owlcms.CheckForUpdates(w, true)
		}),
		fyne.NewMenuItem("About", func() {
			dialog.ShowInformation("About", "OWLCMS Launcher version "+shared.GetLauncherVersion(), w)
		}),
	)
	menu := fyne.NewMainMenu(fileMenu, modulesMenu, helpMenu)
	w.SetMainMenu(menu)
	return menu
}

// createInWindowMainMenu mirrors the native menu bar for platforms where it
// appears outside the application window.
func createInWindowMainMenu(mainMenu *fyne.MainMenu) fyne.CanvasObject {
	buttons := make([]fyne.CanvasObject, 0, len(mainMenu.Items))
	for _, menu := range mainMenu.Items {
		buttons = append(buttons, shared.CreateMenuBarButton(menu.Label, menu.Items))
	}

	return container.NewVBox(container.NewHBox(buttons...), widget.NewSeparator())
}

// setupCleanupOnExit sets up cleanup when the window is closed
func setupCleanupOnExit(w fyne.Window) {
	w.SetCloseIntercept(func() {
		if exitInProgress {
			// Allow close to proceed without re-confirming.
			w.SetCloseIntercept(nil)
			w.Close()
			return
		}

		// Check if any program is running
		requestExit(w)
	})
}

// setupSignalHandling sets up OS signal handlers for graceful shutdown
func setupSignalHandling() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	if shared.GetGoos() == "linux" {
		signal.Notify(sigChan, syscall.SIGHUP)
	}

	go func() {
		sig := <-sigChan
		log.Printf("Signal %v caught, cleaning up.\n", sig)

		// Start a hard exit timer in case cleanup hangs
		go func() {
			time.Sleep(5 * time.Second)
			log.Println("Cleanup timeout reached, forcing exit...")
			clearCurrentControlPanelRuntime()
			os.Exit(1)
		}()

		if shared.IsRunningUnderSystemd() {
			// Under systemd the Go process owns the child — stop everything
			// so systemctl stop actually terminates OWLCMS/Tracker.
			log.Println("Running under systemd — stopping all processes due to signal...")
			stopClosableRunningProcessesForSignal()
			log.Println("All processes stopped.")
		} else {
			if anyClosableProgramRunning() {
				log.Printf("Stopping %s due to signal...", joinProgramNames(closableProgramNames()))
				stopClosableRunningProcessesForSignal()
				log.Println("Closable processes stopped.")
			}
			if anyDaemonRunning() {
				log.Printf("Leaving %s running after terminal signal", joinProgramNames(recoveredDaemonProgramNames()))
			}
		}

		log.Println("Exiting Control Panel...")
		clearCurrentControlPanelRuntime()
		os.Exit(0)
	}()
}

// parseDaemonFlags scans args for --owlcms [version|latest|previous|stop|list] and --tracker [version|latest|previous|stop|list].
// When a switch is present without a value, it defaults to "previous".
// Returns empty strings when the flags are absent.
func parseDaemonFlags(args []string) (owlcmsVersion, trackerVersion string) {
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--owlcms":
			owlcmsVersion = "previous"
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
				owlcmsVersion = args[i]
			}
		case "--tracker":
			trackerVersion = "previous"
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
				trackerVersion = args[i]
			}
		}
	}
	return
}

func isListVerb(value string) bool {
	return strings.EqualFold(strings.TrimSpace(value), "list")
}

func writeAvailableVersions(out io.Writer, label string, versions []string) {
	fmt.Fprintf(out, "%s available versions:\n", label)
	if len(versions) == 0 {
		fmt.Fprintln(out, "  (none installed)")
		return
	}
	for _, version := range versions {
		fmt.Fprintf(out, "  %s\n", version)
	}
}

func handleHeadlessListRequests(out io.Writer, owlcmsRequest, trackerRequest string) (string, string, bool) {
	var listed bool

	if isListVerb(owlcmsRequest) {
		writeAvailableVersions(out, "owlcms", owlcms.GetAllInstalledVersions())
		owlcmsRequest = ""
		listed = true
	}
	if isListVerb(trackerRequest) {
		writeAvailableVersions(out, "tracker", tracker.GetAllInstalledVersions())
		trackerRequest = ""
		listed = true
	}

	return owlcmsRequest, trackerRequest, listed
}

// resolveVersion turns "latest" into the highest semver-installed directory name,
// or validates that the given version directory exists.  installDir is the module's
// install root and allVersions is the semver-descending list from GetAllInstalledVersions.
func resolveVersion(label, requested string, allVersions []string, installDir string, getLastRunVersion func() string) (string, error) {
	if len(allVersions) == 0 {
		return "", fmt.Errorf("no installed %s versions found", label)
	}
	if strings.EqualFold(requested, "latest") {
		v := allVersions[0] // already sorted by semver descending
		log.Printf("Resolved %s 'latest' to %s", label, v)
		return v, nil
	}

	if strings.EqualFold(requested, "previous") {
		prev := getLastRunVersion()
		if prev == "" {
			v := allVersions[0]
			log.Printf("No previous %s version recorded, falling back to latest: %s", label, v)
			return v, nil
		}
		dir := filepath.Join(installDir, prev)
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			v := allVersions[0]
			log.Printf("Previous %s version %q no longer installed, falling back to latest: %s", label, prev, v)
			return v, nil
		}
		log.Printf("Resolved %s 'previous' to %s", label, prev)
		return prev, nil
	}

	// Check the requested version exists as a directory
	dir := filepath.Join(installDir, requested)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return "", fmt.Errorf("%s version %q is not installed (directory %s not found)", label, requested, dir)
	}
	return requested, nil
}

func configureTrackerConnectionForHeadlessTandem(owlcmsVersion, trackerVersion string) error {
	trackerPort := strings.TrimSpace(tracker.GetPortForRelease(trackerVersion))
	if trackerPort == "" {
		return fmt.Errorf("selected tracker version %q has no configured port", trackerVersion)
	}
	trackerHost := "127.0.0.1"
	trackerURL := fmt.Sprintf("ws://%s/ws", trackerHost)
	if err := owlcms.ConfigureTrackerConnectionForReleaseURL(owlcmsVersion, trackerURL, trackerPort); err != nil {
		return err
	}
	trackerURL = fmt.Sprintf("ws://%s:%s/ws", trackerHost, trackerPort)
	log.Printf("Configured OWLCMS %s to connect to Tracker %s using host=%s port=%s url=%s; updated %s",
		owlcmsVersion, trackerVersion, trackerHost, trackerPort, trackerURL, owlcms.GetReleaseEnvPath(owlcmsVersion))
	return nil
}

type runningModuleProcess struct {
	Label        string
	Version      string
	PID          int
	Port         string
	Source       string
	MetadataPath string
	PIDFilePath  string
}

func resolveRunningModuleProcess(label, metadataPath, pidFilePath, port, fallbackVersion string) (*runningModuleProcess, error) {
	if meta, running := shared.CheckDaemonRunning(metadataPath); running {
		return &runningModuleProcess{
			Label:        label,
			Version:      meta.Version,
			PID:          meta.PID,
			Port:         meta.Port,
			Source:       "runtime metadata",
			MetadataPath: metadataPath,
			PIDFilePath:  pidFilePath,
		}, nil
	}

	_ = shared.ClearRuntimeMetadata(metadataPath)

	pid, source, err := shared.ResolvePIDFromFileOrPort(pidFilePath, port)
	if err != nil {
		return nil, err
	}
	if pid == 0 {
		return nil, nil
	}

	return &runningModuleProcess{
		Label:        label,
		Version:      fallbackVersion,
		PID:          pid,
		Port:         port,
		Source:       source,
		MetadataPath: metadataPath,
		PIDFilePath:  pidFilePath,
	}, nil
}

// stopHeadlessDaemons stops running OWLCMS and/or Tracker modules from the command line.
func stopHeadlessDaemons(stopOwlcms, stopTracker bool) {
	var failed bool

	if stopOwlcms {
		if err := owlcms.InitEnv(); err != nil {
			log.Printf("ERROR: failed to load owlcms environment: %v", err)
			fmt.Fprintf(os.Stderr, "owlcms: failed to load environment: %v\n", err)
			failed = true
		} else {
			failed = stopOneModule("owlcms", owlcms.RuntimeMetadataPath(), owlcms.PIDFilePath(), owlcms.GetPort(), owlcms.GetLastRunVersion()) || failed
		}
	}

	if stopTracker {
		if err := tracker.InitEnv(); err != nil {
			log.Printf("ERROR: failed to load tracker environment: %v", err)
			fmt.Fprintf(os.Stderr, "tracker: failed to load environment: %v\n", err)
			failed = true
		} else {
			failed = stopOneModule("tracker", tracker.RuntimeMetadataPath(), tracker.PIDFilePath(), tracker.GetPort(), tracker.GetLastRunVersion()) || failed
		}
	}

	if failed {
		os.Exit(1)
	}
}

// stopOneModule stops a single module identified by runtime metadata, PID file, or configured port.
// Returns true on failure.
func stopOneModule(label, metadataPath, pidFilePath, port, fallbackVersion string) bool {
	running, err := resolveRunningModuleProcess(label, metadataPath, pidFilePath, port, fallbackVersion)
	if err != nil {
		log.Printf("ERROR: failed to resolve %s process: %v", label, err)
		fmt.Fprintf(os.Stderr, "%s: failed to resolve running process: %v\n", label, err)
		return true
	}
	if running == nil {
		log.Printf("%s is not running", label)
		fmt.Printf("%s is not running\n", label)
		_ = shared.ClearRuntimeMetadata(metadataPath)
		return false
	}

	versionText := strings.TrimSpace(running.Version)
	if versionText == "" {
		versionText = "unknown version"
	}

	log.Printf("Stopping %s %s (PID %d, port %s, source %s)...", label, versionText, running.PID, running.Port, running.Source)
	fmt.Printf("Stopping %s %s (PID %d)...\n", label, versionText, running.PID)

	var stopErr error
	if label == "owlcms" {
		stopErr = owlcms.StopProcessByPort(running.Port)
	} else {
		stopErr = shared.StopPIDFileOrPortProcess(pidFilePath, running.Port)
	}
	if stopErr != nil {
		log.Printf("ERROR: failed to stop %s PID %d on port %s: %v", label, running.PID, running.Port, stopErr)
		fmt.Fprintf(os.Stderr, "%s: failed to stop PID %d on port %s: %v\n", label, running.PID, running.Port, stopErr)
		return true
	}

	_ = shared.ClearRuntimeMetadata(metadataPath)
	log.Printf("%s %s (PID %d) stopped", label, versionText, running.PID)
	fmt.Printf("%s %s stopped\n", label, versionText)
	return false
}
