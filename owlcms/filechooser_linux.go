//go:build linux
// +build linux

package owlcms

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
)

// selectLocalZip shows a native Fyne file open dialog on Linux.
func selectLocalZip(w fyne.Window, cb func(path string, err error)) {
	fd := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
		if err != nil {
			cb("", err)
			return
		}
		if reader == nil {
			// Cancelled
			cb("", nil)
			return
		}
		defer reader.Close()
		uri := reader.URI()
		if uri == nil {
			cb("", nil)
			return
		}
		cb(uri.Path(), nil)
	}, w)
	fd.SetFilter(storage.NewExtensionFileFilter([]string{".zip"}))
	fd.Show()
}

// selectSaveZip shows a native Fyne file save dialog on Linux.
func selectSaveZip(w fyne.Window, defaultFilename string, cb func(path string, err error)) {
	fileDialog := dialog.NewFileSave(func(writer fyne.URIWriteCloser, err error) {
		if err != nil {
			cb("", err)
			return
		}
		if writer == nil {
			// User cancelled
			cb("", nil)
			return
		}
		defer writer.Close()
		cb(writer.URI().Path(), nil)
	}, w)

	fileDialog.SetFilter(storage.NewExtensionFileFilter([]string{".zip"}))
	fileDialog.SetFileName(defaultFilename)
	fileDialog.Show()
}

// selectFolder shows a Fyne folder chooser on Linux, starting in startDir when it is set.
func selectFolder(w fyne.Window, _ string, startDir string, cb func(path string, err error)) {
	fd := dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
		if err != nil {
			cb("", err)
			return
		}
		if uri == nil {
			cb("", nil)
			return
		}
		cb(uri.Path(), nil)
	}, w)
	if startDir != "" {
		if location, err := storage.ListerForURI(storage.NewFileURI(startDir)); err == nil {
			fd.SetLocation(location)
		}
	}
	fd.Show()
}
