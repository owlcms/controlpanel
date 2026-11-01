//go:build !linux

package owlcms

import (
	"fyne.io/fyne/v2"
	"github.com/sqweek/dialog"
)

// selectLocalZip shows a native sqweek file chooser on Windows and macOS.
func selectLocalZip(_ fyne.Window, cb func(path string, err error)) {
	// Run in a goroutine because sqweek blocks the thread.
	go func() {
		path, err := dialog.File().Filter("ZIP files", "zip").Title("Select OWLCMS ZIP").Load()
		if err != nil {
			// If user cancelled, return nil error and empty path
			if err == dialog.ErrCancelled {
				cb("", nil)
				return
			}
			cb("", err)
			return
		}
		cb(path, nil)
	}()
}

// selectSaveZip shows a native sqweek file save dialog on Windows and macOS.
func selectSaveZip(_ fyne.Window, defaultFilename string, cb func(path string, err error)) {
	// Run in a goroutine because sqweek blocks the thread.
	go func() {
		path, err := dialog.File().Filter("ZIP files", "zip").Title("Save OWLCMS ZIP").SetStartFile(defaultFilename).Save()
		if err != nil {
			// If user cancelled, return nil error and empty path
			if err == dialog.ErrCancelled {
				cb("", nil)
				return
			}
			cb("", err)
			return
		}
		cb(path, nil)
	}()
}

// selectFolder shows a native sqweek folder chooser on Windows and macOS, starting in startDir when it is set.
func selectFolder(_ fyne.Window, title, startDir string, cb func(path string, err error)) {
	// Run in a goroutine because sqweek blocks the thread.
	go func() {
		builder := dialog.Directory().Title(title)
		if startDir != "" {
			builder = builder.SetStartDir(startDir)
		}
		path, err := builder.Browse()
		if err == dialog.ErrCancelled {
			cb("", nil)
			return
		}
		cb(path, err)
	}()
}
