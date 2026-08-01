package video

import (
	"controlpanel/shared"

	"fyne.io/fyne/v2"
)

// resetToExplainMode clears download/update UI and shows the uninstalled explanation
func resetToExplainMode(w fyne.Window) {
	if updateTitleContainer != nil {
		updateTitleContainer.Objects = []fyne.CanvasObject{}
		updateTitleContainer.Hide()
		updateTitleContainer.Refresh()
	}
	if downloadContainer != nil {
		downloadContainer.Objects = []fyne.CanvasObject{}
		downloadContainer.Hide()
		downloadContainer.Refresh()
	}
	if releaseDropdown != nil {
		releaseDropdown.Hide()
	}
	if prereleaseCheckbox != nil {
		prereleaseCheckbox.Hide()
	}
	if downloadButtonTitle != nil {
		downloadButtonTitle.Hide()
	}
	if statusLabel != nil {
		statusLabel.SetText("")
		statusLabel.Hide()
	}
	if stopContainer != nil {
		stopContainer.Hide()
	}

	shared.ShowUninstalledTabContent(versionContainer, "asset/video.md", func(usePrerelease bool) { InstallDefault(w, usePrerelease) }, nil)
	versionContainer.Show()
	versionContainer.Refresh()
}
