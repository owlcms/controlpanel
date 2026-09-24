package shared

import (
	"errors"
	"log"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// NewSharedKeyField builds the Shared Key password entry and the notice shown when the saved key cannot be used.
func NewSharedKeyField(savedKey string, loadErr error, logContext string) (*widget.Entry, *widget.Label) {
	keyEntry := widget.NewPasswordEntry()
	keyNotice := widget.NewLabel("")
	keyNotice.Wrapping = fyne.TextWrapWord
	keyNotice.Importance = widget.WarningImportance
	keyNotice.Hide()
	switch {
	case loadErr == nil:
		keyEntry.SetText(savedKey)
	case errors.Is(loadErr, ErrSecretReentryRequired):
		log.Printf("Shared key for %s: %v", logContext, loadErr)
		keyNotice.SetText("The saved key was encrypted on another computer. Please enter it again.")
		keyNotice.Show()
	default:
		log.Printf("Cannot read shared key for %s: %v", logContext, loadErr)
	}
	return keyEntry, keyNotice
}

// NewClearableSharedKeyField is NewSharedKeyField with a "Clear Key" button; the returned object replaces the entry in a form.
func NewClearableSharedKeyField(savedKey string, loadErr error, logContext string) (*widget.Entry, fyne.CanvasObject, *widget.Label) {
	keyEntry, keyNotice := NewSharedKeyField(savedKey, loadErr, logContext)
	clearButton := widget.NewButton("Clear Key", func() {
		keyEntry.SetText("")
	})
	return keyEntry, container.NewBorder(nil, nil, nil, clearButton, keyEntry), keyNotice
}

// SharedKeyOverride edits a per-version key: the version's own key (possibly empty) or no line at all, meaning the default key applies.
type SharedKeyOverride struct {
	Entry  *widget.Entry
	Row    fyne.CanvasObject
	Status *widget.Label
	Notice *widget.Label
	// OnStateChanged runs after every change to the key state, including Clear/Reset on an already empty entry.
	OnStateChanged func()
	useDefault     bool
	defaultKey     string
}

// NewSharedKeyOverride builds the per-version key row; hasOwn tells whether the version's env.properties has a key line.
func NewSharedKeyOverride(ownKey string, hasOwn bool, ownErr error, defaultKey string, logContext string) *SharedKeyOverride {
	o := &SharedKeyOverride{useDefault: !hasOwn, defaultKey: defaultKey}
	o.Entry, o.Notice = NewSharedKeyField(ownKey, ownErr, logContext)
	o.Status = widget.NewLabel("")
	o.Status.Wrapping = fyne.TextWrapWord
	defaultSet := IsSecretSet(defaultKey)

	programmatic := false
	setText := func(text string) {
		programmatic = true
		o.Entry.SetText(text)
		programmatic = false
	}
	updateStatus := func() {
		if o.useDefault && defaultSet {
			o.Entry.SetPlaceHolder("(default key)")
		} else {
			o.Entry.SetPlaceHolder("(empty)")
		}
		switch {
		case o.useDefault && defaultSet:
			o.Status.SetText("Using the default key.")
		case o.useDefault:
			o.Status.SetText("No key (no default key is set).")
		case !IsSecretSet(o.Entry.Text):
			o.Status.SetText("No key for this version.")
		default:
			o.Status.SetText("Key specific to this version.")
		}
		if o.OnStateChanged != nil {
			o.OnStateChanged()
		}
	}

	resetButton := widget.NewButton("Reset to Default Key", func() {
		o.useDefault = true
		setText("")
		updateStatus()
	})
	if !defaultSet {
		resetButton.Hide()
	}
	clearButton := widget.NewButton("Clear Key", func() {
		o.useDefault = false
		setText("")
		o.Entry.Enable()
		updateStatus()
	})
	o.Entry.OnChanged = func(_ string) {
		if !programmatic {
			o.useDefault = false
		}
		updateStatus()
	}

	if o.useDefault {
		o.Notice.Hide()
		setText("")
	}
	updateStatus()
	if defaultSet {
		// two buttons beside the entry would squeeze it to nothing
		o.Row = container.NewVBox(o.Entry, container.NewHBox(resetButton, clearButton))
	} else {
		o.Row = container.NewBorder(nil, nil, nil, clearButton, o.Entry)
	}
	return o
}

// UseDefault reports whether saving should remove the version's key line so the default key applies.
func (o *SharedKeyOverride) UseDefault() bool {
	return o.useDefault
}

// EffectiveKey is the key the version will use after saving.
func (o *SharedKeyOverride) EffectiveKey() string {
	if o.useDefault {
		return o.defaultKey
	}
	return o.Entry.Text
}
