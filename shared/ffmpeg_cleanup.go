package shared

import (
	"log"
	"os"
	"path/filepath"
	"strings"
)

func hasFFmpegExecutable(root string) bool {
	for _, name := range []string{"ffmpeg", "ffmpeg.exe"} {
		if info, err := os.Lstat(filepath.Join(root, "bin", name)); err == nil && info.Mode().IsRegular() {
			return true
		}
	}
	return false
}

func cleanupFFmpegRuntimes(root, active string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		log.Printf("FFmpeg cleanup: %v", err)
		return
	}
	flat := hasFFmpegExecutable(root)
	for _, entry := range entries {
		candidate := filepath.Join(root, entry.Name())
		if candidate == active || !entry.IsDir() {
			continue
		}
		managed := hasFFmpegExecutable(candidate)
		if strings.HasPrefix(entry.Name(), ".ffmpeg-retired-") {
			managed = hasFFmpegExecutable(filepath.Join(candidate, "runtime"))
		}
		if flat && (entry.Name() == "bin" || entry.Name() == "lib") {
			managed = true
		}
		if !managed {
			continue
		}
		if err := os.RemoveAll(candidate); err != nil {
			log.Printf("FFmpeg cleanup deferred for %s: %v", candidate, err)
		} else {
			log.Printf("Removed stale FFmpeg runtime: %s", candidate)
		}
	}
}
