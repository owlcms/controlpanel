package shared

import (
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
)

//go:embed ffmpeg-runtimes.json
var ffmpegManifestJSON []byte

var errFFmpegSystemOnly = errors.New("no bundled FFmpeg for this platform")

type ffmpegArchive struct {
	URL         string `json:"url"`
	Format      string `json:"format"`
	Destination string `json:"destination"`
	SHA256      string `json:"sha256"`
	Rolling     bool   `json:"rolling"`
}

type ffmpegRuntime struct {
	SystemOnly             bool            `json:"systemOnly"`
	AllowSystemFallback    bool            `json:"allowSystemFallback"`
	Version                string          `json:"version"`
	Revision               string          `json:"revision"`
	Directory              string          `json:"directory"`
	ArchiveRoot            string          `json:"archiveRoot"`
	Executables            []string        `json:"executables"`
	RequiredDirectories    []string        `json:"requiredDirectories"`
	LibraryPathDirectories []string        `json:"libraryPathDirectories,omitempty"`
	Archives               []ffmpegArchive `json:"archives"`
}

func loadFFmpegRuntime(data []byte, goos, goarch string) (ffmpegRuntime, error) {
	var manifest map[string]ffmpegRuntime
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return ffmpegRuntime{}, fmt.Errorf("invalid FFmpeg manifest: %w", err)
	}
	runtime, ok := manifest[goos+"-"+goarch]
	if !ok {
		runtime, ok = manifest[goos]
	}
	if !ok || runtime.SystemOnly {
		return ffmpegRuntime{}, fmt.Errorf("%w: %s/%s", errFFmpegSystemOnly, goos, goarch)
	}
	if runtime.Version == "" || runtime.Revision == "" || !validFFmpegRelativePath(runtime.Directory) || runtime.Directory == "." || strings.Contains(runtime.Directory, "/") ||
		!validFFmpegRelativePath(runtime.ArchiveRoot) || len(runtime.Archives) == 0 || len(runtime.Executables) == 0 {
		return ffmpegRuntime{}, fmt.Errorf("incomplete FFmpeg manifest entry for %s/%s", goos, goarch)
	}
	for _, name := range runtime.Executables {
		if !validFFmpegRelativePath(name) || strings.Contains(name, "/") || name == "." {
			return ffmpegRuntime{}, fmt.Errorf("invalid FFmpeg executable name %q", name)
		}
	}
	for _, directory := range append(append([]string{}, runtime.RequiredDirectories...), runtime.LibraryPathDirectories...) {
		if !validFFmpegRelativePath(directory) {
			return ffmpegRuntime{}, fmt.Errorf("invalid FFmpeg required directory %q", directory)
		}
	}
	for _, archive := range runtime.Archives {
		parsed, err := url.Parse(archive.URL)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" ||
			(archive.Format != "zip" && archive.Format != "tar.xz") || !validFFmpegRelativePath(archive.Destination) {
			return ffmpegRuntime{}, fmt.Errorf("invalid FFmpeg archive definition: %q", archive.URL)
		}
		digest, err := hex.DecodeString(archive.SHA256)
		if (archive.Rolling && archive.SHA256 != "") || (!archive.Rolling && (err != nil || len(digest) != 32)) {
			return ffmpegRuntime{}, fmt.Errorf("FFmpeg archive needs either a pinned SHA-256 or rolling=true: %q", archive.URL)
		}
	}
	return runtime, nil
}

func validFFmpegRelativePath(value string) bool {
	return value != "" && !strings.ContainsAny(value, "\\:") && filepath.IsLocal(value)
}
