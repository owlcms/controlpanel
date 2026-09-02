//go:build !darwin || !cgo

package shared

// EnsureCameraPermission is a no-op outside macOS; camera access there does
// not require a per-app authorization prompt.
func EnsureCameraPermission() error {
	return nil
}
