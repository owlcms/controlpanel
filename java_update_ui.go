package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"

	"controlpanel/shared"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

func startJavaUpdateGate(w fyne.Window, initialWindowSize fyne.Size, start func()) {
	installed, err := shared.InspectJava("jdk-25")
	needsUpdate := errors.Is(err, shared.ErrJavaNotInstalled)
	installedVersion := ""
	if installed != nil {
		installedVersion = installed.Version
		meetsMinimum, compareErr := shared.JavaVersionAtLeast(installedVersion, shared.MinimumJavaVersion)
		if compareErr != nil {
			log.Printf("Cannot compare installed Java %s with minimum %s: %v",
				installedVersion, shared.MinimumJavaVersion, compareErr)
		} else {
			needsUpdate = !meetsMinimum
		}
	} else if err != nil && !needsUpdate {
		log.Printf("Cannot inspect installed Java: %v", err)
	}
	if !needsUpdate {
		start()
		return
	}

	w.Resize(initialWindowSize)
	w.SetContent(container.NewCenter(widget.NewLabel("Checking required Java runtime...")))
	w.Show()

	message := fmt.Sprintf(
		"OWLCMS requires Java %s.\n\nInstalled Java: %s\n\nDownload and install the latest Temurin 25 now? You may cancel or skip this update.",
		shared.MinimumJavaVersion, displayJavaVersion(installedVersion))
	confirm := dialog.NewConfirm("Java Security Update", message, func(update bool) {
		if !update {
			start()
			return
		}
		runJavaUpdateDialog(w, start, false)
	}, w)
	confirm.SetConfirmText("Update Java")
	confirm.SetDismissText("Skip")
	confirm.Show()
}

func runJavaUpdateFromMenu(w fyne.Window) {
	runJavaUpdateDialog(w, nil, true)
}

func runJavaUpdateDialog(w fyne.Window, after func(), forceLatest bool) {
	progress := widget.NewProgressBar()
	progress.Min = 0
	progress.Max = 1
	status := widget.NewLabel("Checking the latest Temurin release...")
	progressDialog := dialog.NewCustom("Updating Java", "Cancel", container.NewVBox(status, progress), w)
	cancel := make(chan bool)
	var cancelOnce sync.Once
	progressDialog.SetOnClosed(func() {
		cancelOnce.Do(func() { close(cancel) })
	})
	progressDialog.Resize(fyne.NewSize(430, 180))
	progressDialog.Show()

	go func() {
		report := func(downloaded, total int64) {
			if total <= 0 {
				return
			}
			percentage := float64(downloaded) / float64(total)
			fyne.Do(func() {
				progress.SetValue(percentage)
				status.SetText(fmt.Sprintf("Downloading Temurin 25... %.0f%%", percentage*100))
			})
		}
		var result *shared.JavaUpdateResult
		var err error
		shared.SerializeJavaUpdate(func() (*shared.JavaUpdateResult, error) {
			if forceLatest {
				result, err = shared.UpdateToLatestJava(report, cancel)
			} else {
				result, err = shared.EnsureMinimumJava(report, cancel)
			}
			return result, err
		})

		fyne.Do(func() {
			progressDialog.Hide()
			if after != nil {
				after()
			}
			switch {
			case errors.Is(err, context.Canceled):
				return
			case err != nil:
				dialog.ShowError(fmt.Errorf("Java update was not installed: %w", err), w)
			case result != nil && result.Updated:
				dialog.ShowInformation("Java Updated",
					fmt.Sprintf("Java was updated from %s to %s.", displayJavaVersion(result.Previous), result.Current), w)
			case result != nil && forceLatest:
				dialog.ShowInformation("Java Up to Date",
					fmt.Sprintf("Java %s is already the latest available Temurin 25 release.", result.Current), w)
			}
		})
	}()
}

func displayJavaVersion(version string) string {
	if version == "" {
		return "not installed"
	}
	return version
}
