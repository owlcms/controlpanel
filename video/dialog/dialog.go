package dialog

import (
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// ShowWideError displays an error dialog with proper width for long messages
func ShowWideError(err error, w fyne.Window) {
	dialog.ShowError(err, w)
}

// NewDownloadDialog creates a consistent download dialog with a progress bar and cancel button
func NewDownloadDialog(title string, window fyne.Window, cancel chan bool) (dialog.Dialog, *widget.ProgressBar) {
	progressBar := widget.NewProgressBar()

	content := container.NewVBox(progressBar)
	progressDialog := dialog.NewCustom(title, "Cancel", content, window)

	var once sync.Once
	progressDialog.SetOnClosed(func() {
		once.Do(func() {
			close(cancel)
		})
	})

	progressDialog.Resize(fyne.NewSize(400, 200))

	return progressDialog, progressBar
}
