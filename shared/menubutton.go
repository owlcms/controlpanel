package shared

import (
	"image/color"
	"log"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

var _ desktop.Hoverable = (*menuBarButton)(nil)

// CreateMenuButton creates a button that shows a popup menu when clicked
func CreateMenuButton(label string, menuItems []*fyne.MenuItem) *widget.Button {
	btn := widget.NewButton(label+" ▼", nil)
	btn.Importance = widget.LowImportance
	btn.OnTapped = func() {
		showMenu(label, menuItems, btn)
	}
	return btn
}

// CreateMenuBarButton creates a compact, regular-weight control for an
// in-window application menu bar.
func CreateMenuBarButton(label string, menuItems []*fyne.MenuItem) fyne.CanvasObject {
	button := &menuBarButton{label: label, menuItems: menuItems}
	button.ExtendBaseWidget(button)
	return button
}

type menuBarButton struct {
	widget.BaseWidget

	background *canvas.Rectangle
	hovered    bool
	label      string
	menuOpen   bool
	menuItems  []*fyne.MenuItem
}

type compactMenuTheme struct {
	fyne.Theme
}

func (t compactMenuTheme) Size(name fyne.ThemeSizeName) float32 {
	if name == theme.SizeNameInnerPadding {
		return 3
	}
	return t.Theme.Size(name)
}

func (t compactMenuTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	if name == theme.ColorNameOverlayBackground {
		return t.Theme.Color(theme.ColorNameMenuBackground, variant)
	}
	if name == theme.ColorNameShadow {
		return color.Transparent
	}
	return t.Theme.Color(name, variant)
}

func (b *menuBarButton) AccessibilityLabel() string {
	return b.label
}

func (b *menuBarButton) AccessibilityRole() fyne.AccessibleRole {
	return fyne.AccessibleRoleButton
}

func (b *menuBarButton) CreateRenderer() fyne.WidgetRenderer {
	b.background = canvas.NewRectangle(color.Transparent)

	b.updateBackground()
	return widget.NewSimpleRenderer(container.NewStack(b.background, b.menuLabel()))
}

func (b *menuBarButton) MinSize() fyne.Size {
	return b.menuLabel().MinSize().Add(fyne.NewSize(12, 4))
}

func (b *menuBarButton) menuLabel() *canvas.Text {
	label := canvas.NewText(b.label, b.Theme().Color(theme.ColorNameForeground, fyne.CurrentApp().Settings().ThemeVariant()))
	label.Alignment = fyne.TextAlignCenter
	label.TextSize = theme.TextSize()
	return label
}

func (b *menuBarButton) MouseIn(*desktop.MouseEvent) {
	b.hovered = true
	b.updateBackground()
}

func (b *menuBarButton) MouseMoved(*desktop.MouseEvent) {
}

func (b *menuBarButton) MouseOut() {
	b.hovered = false
	b.menuOpen = false
	b.updateBackground()
}

func (b *menuBarButton) Tapped(*fyne.PointEvent) {
	b.menuOpen = true
	b.updateBackground()
	showCompactMenu(b.label, b.menuItems, b, b.Theme(), func() {
		b.menuOpen = false
		b.updateBackground()
	})
}

func (b *menuBarButton) updateBackground() {
	if b.background == nil {
		return
	}

	backgroundColor := color.Color(color.Transparent)
	if b.hovered || b.menuOpen {
		backgroundColor = b.Theme().Color(theme.ColorNameHover, fyne.CurrentApp().Settings().ThemeVariant())
	}
	b.background.FillColor = backgroundColor
	b.background.Refresh()
}
func showMenu(label string, menuItems []*fyne.MenuItem, anchor fyne.CanvasObject) {
	menuCanvas, pos, ok := menuPopupLocation(label, anchor)
	if !ok {
		return
	}

	widget.ShowPopUpMenuAtPosition(fyne.NewMenu("", menuItems...), menuCanvas, pos)
}

func showCompactMenu(label string, menuItems []*fyne.MenuItem, anchor fyne.CanvasObject, menuTheme fyne.Theme, onDismiss func()) {
	menuCanvas, pos, ok := menuPopupLocation(label, anchor)
	if !ok {
		onDismiss()
		return
	}

	menu := widget.NewMenu(fyne.NewMenu("", menuItems...))
	compactTheme := compactMenuTheme{Theme: menuTheme}
	topInset := canvas.NewRectangle(compactTheme.Color(theme.ColorNameMenuBackground, fyne.CurrentApp().Settings().ThemeVariant()))
	topInset.SetMinSize(fyne.NewSize(1, 3))
	leftInset := canvas.NewRectangle(compactTheme.Color(theme.ColorNameMenuBackground, fyne.CurrentApp().Settings().ThemeVariant()))
	leftInset.SetMinSize(fyne.NewSize(3, 1))
	rightInset := canvas.NewRectangle(compactTheme.Color(theme.ColorNameMenuBackground, fyne.CurrentApp().Settings().ThemeVariant()))
	rightInset.SetMinSize(fyne.NewSize(3, 1))
	content := container.NewBorder(topInset, nil, leftInset, rightInset, container.NewThemeOverride(menu, compactTheme))
	popup := widget.NewPopUp(container.NewThemeOverride(content, compactTheme), menuCanvas)
	container.NewThemeOverride(popup, compactTheme)
	menu.OnDismiss = func() {
		popup.Hide()
		onDismiss()
	}
	popup.ShowAtPosition(pos)
}

func menuPopupLocation(label string, anchor fyne.CanvasObject) (fyne.Canvas, fyne.Position, bool) {
	app := fyne.CurrentApp()
	if app == nil || app.Driver() == nil {
		log.Printf("Cannot show %s menu: Fyne application driver is unavailable", label)
		return nil, fyne.Position{}, false
	}

	driver := app.Driver()
	menuCanvas := driver.CanvasForObject(anchor)
	if menuCanvas == nil {
		windows := driver.AllWindows()
		if len(windows) == 0 {
			log.Printf("Cannot show %s menu: no Fyne window is available", label)
			return nil, fyne.Position{}, false
		}
		menuCanvas = windows[0].Canvas()
	}

	pos := driver.AbsolutePositionForObject(anchor)
	pos.Y += anchor.Size().Height
	return menuCanvas, pos, true
}
