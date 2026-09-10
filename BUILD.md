### Developer Notes
- Native development builds can work on Windows, Linux, and other supported Go platforms.
- Windows Fyne builds can still be noticeably slower than Linux builds.
- WSL2 is not required for normal development builds.
- Some Fyne tooling, especially `fyne-cross`, may still work better or require WSL2 plus Docker Desktop for packaging and cross-platform bundle creation.

- This is a standard `golang` program
  - install the go environment for your platform
  - run `go mod download` to get the dependencies

- Linux native builds also need the Fyne/GLFW development libraries installed on the host.
  On Debian/Ubuntu/Raspberry Pi OS, install them with:
  ```bash
  sudo apt install build-essential pkg-config libgl1-mesa-dev xorg-dev libwayland-dev libxkbcommon-dev
  ```
  If the build fails with `wayland-client-core.h: No such file or directory`, `libwayland-dev` is the missing package.

- VS Code works fine
  - Standard Go extensions
  - GitHub Free CoPilot works fine

- for Linux testing, use the terminal window in VS Code
  ```
  go run .
  ```
  or, to create a binary
  ```
  go build -o owlcms .
  ```

- macOS local builds
  - A plain local build uses the host architecture. On Apple Silicon this creates an arm64 binary:
    ```
    go build .
    ```
  - The Apple linker may print this warning during Fyne/cgo builds:
    ```
    ld: warning: ignoring duplicate libraries: '-lobjc'
    ```
    This warning is harmless when `go build` exits successfully; the duplicate Objective-C library is ignored by the linker.
  - To build the same Intel binary currently produced by GitHub Actions:
    ```
    GOOS=darwin GOARCH=amd64 CGO_ENABLED=1 go build -o build/owlcms .
    ```
  - To inspect the architecture of the resulting binary:
    ```
    file controlpanel
    file build/owlcms
    ```

- VS Code target settings
  - The default [.vscode/settings.json](.vscode/settings.json) in this repo is set up for Linux arm64 analysis.
  - When switching development targets, copy the matching section into [.vscode/settings.json](.vscode/settings.json).
  - Linux arm64:
    ```json
    "go.toolsEnvVars": {
        "GOOS": "linux",
        "GOARCH": "arm64"
    },
    "gopls": {
        "build.env": {
            "GOOS": "linux",
            "GOARCH": "arm64"
        }
    }
    ```
  - Windows amd64:
    ```json
    "go.toolsEnvVars": {
        "GOOS": "windows",
        "GOARCH": "amd64"
    },
    "gopls": {
        "build.env": {
            "GOOS": "windows",
            "GOARCH": "amd64"
        }
    }
    ```

- `fyne-cross` can be used to generate the pi and Windows binaries for testing
  - depending on your host setup, this may require WSL2 and Docker Desktop

  - install fyne-cross

     ```
     go install github.com/fyne-io/fyne-cross@latest
     ```
  
  - Cross-compile
     ```
      fyne-cross windows --app-id app.owlcms.controlpanel -name owlcms
      fyne-cross linux -arch arm64 --app-id app.owlcms.controlpanel -name owlcms-pi
      fyne-cross linux -arch amd64 --app-id app.owlcms.controlpanel -name owlcms-linux
     ```

- Windows local build notes
  - For a normal Windows build with console output, use:
    ```
    go build -buildvcs=false -o controlpanel.exe .
    ```
  - This is better while debugging because stdout, stderr, and traces remain visible.
  - For a Windows GUI-style executable without a console window, use:
    ```
    go build -buildvcs=false -ldflags="-H windowsgui" -o controlpanel.exe .
    ```

### Local macOS Camera Permission Test

Use an existing app bundle to test packaging and signing without compiling,
creating a DMG, or running the release workflow:

```bash
bash scripts/package-local-macos.sh
```

This copies `/Applications/owlcms.app` to `build/macos-local/owlcms.app`, adds
the camera usage description, and signs it with hardened runtime and the same
camera entitlement as the release workflow. The installed app is not modified.
Existing output paths are refused; use `--output build/macos-test-2/owlcms.app`
for another run. Use `--executable ./controlpanel` to package a previously built
binary instead of retaining the installed executable.

Quit the running control panel, then open the test bundle in Finder, or launch
it through Launch Services from the repository root:

```bash
open -n "$PWD/build/macos-local/owlcms.app"
```

Start Video from that control panel, enable a camera, and allow camera access
when prompted. Check **System Settings > Privacy & Security > Camera** for
the launcher if access was previously denied. Do not run the executable inside
`Contents/MacOS` directly from Terminal: macOS may attribute the camera request
to Terminal instead of the launcher. The test copy retains the original bundle
identifier and uses the normal installed modules and configuration, so avoid
running both control panels simultaneously.

The default signature is ad-hoc and is suitable for a local metadata test, not
for validating Developer ID identity or notarization. For a closer match to a
release, use a locally installed signing identity:

```bash
bash scripts/package-local-macos.sh \
  --output build/macos-developer-id/owlcms.app \
  --identity 'Developer ID Application: Your Name (TEAMID)'
```

Camera authorization is managed by macOS, not granted by the entitlement itself.
Signing identity changes can affect existing permission decisions. The script
does not reset permissions, remove quarantine, or notarize the app.

### Releasing

See [RELEASE_PROCESS.md](RELEASE_PROCESS.md) for the release process.

### FFmpeg Runtime Manifest

[shared/ffmpeg-runtimes.json](shared/ffmpeg-runtimes.json) is embedded with
`go:embed`. Control Panel remains a standalone executable. To update an FFmpeg
download, edit this JSON and rebuild Control Panel; neither the Go installer nor
the release workflow needs a version change. Downloads happen when a video
module is installed or launched, not during the Control Panel release build.

| Platform | Provider | Archives | Extraction |
| --- | --- | --- | --- |
| Windows | Existing BtbN rolling master, win64 shared | One ZIP | Preserve the provider root and all files, including DLLs beside the executables |
| Linux AMD64 | Existing BtbN rolling master, shared | One `.tar.xz` | Preserve the provider root, `bin/`, `lib/`, and symlinks |
| Linux ARM64 / 64-bit Raspberry Pi | Existing BtbN rolling master, shared | One `.tar.xz` | Same layout as Linux AMD64 |
| macOS ARM64 | Riedl `N-126416-g9997fd0606`, September 6, 2026 | Three ZIPs | Each contains one tool; extract all three into `bin/` |
| macOS Intel | System installation (Homebrew) | None | No bundled download |

Manifest entries use `os-architecture` keys, with an optional OS-wide entry.
The Windows entry preserves the existing win64 download for all Windows hosts.

- `systemOnly`: do not download a bundled runtime.
- `version`: release/snapshot identifier for diagnostics.
- `revision`: required installation revision. Increment it to request a fresh download even when a rolling URL and version remain unchanged.
- `directory`: installation directory under the shared runtime's `ffmpeg/` root.
- `archiveRoot`: directory in the combined extracted payload to install; `.` for Riedl's flat ZIPs.
- `executables`: required filenames under `bin/`; the first is the FFmpeg entry point. All are made executable.
- `requiredDirectories`: directories that must exist beside `bin/`, such as Linux `lib/`.
- `libraryPathDirectories`: directories to prepend to `LD_LIBRARY_PATH` for this managed runtime. Linux shared builds declare `["lib"]`; static builds declare none. Existing environment values are preserved, and a directory merely existing on disk does not enable it. Declared directories are required at installation.
- `archives`: ordered downloads with `url`, `format` (`zip` or `tar.xz`), and `destination` relative to the extraction area.
- `sha256`: pinned SHA-256 of the downloaded archive, checked before extraction.
- `rolling`: explicitly permits an unpinned archive instead of `sha256`. Windows/Linux preserve their previous rolling policy; these are not fixed, tested revisions.
- `allowSystemFallback`: permits system fallback after download failure, but does not permit reuse of unidentified or stale managed installations. False by default; Apple Silicon requires the pinned runtime because Homebrew 9.0.1 has the verified webcam negotiation bug.

ZIP extraction uses Go's ZIP reader with automatic root stripping disabled.
Tar/XZ extraction uses the existing `tar -xJf` helper, so `tar` with XZ support
must be installed on Linux. The manifest, not the host OS, selects the extractor.
Each archive is extracted to its declared destination in a temporary staging
directory outside the shared discovery path. The installer checks the complete
runtime, then renames its `archiveRoot` into `ffmpeg/<directory>/`. Failed
installations clean up staging and do not publish partial runtimes.

Each installation records its manifest entry in `.ffmpeg-runtime.json`. Reuse
requires a matching identity and complete runtime. Missing, unreadable, or
mismatched identity triggers a fresh download. The installer stages and
validates the replacement before retiring the old target, and restores that
target if publishing the replacement fails. Once the new runtime is validated,
old FFmpeg runtime directories in the shared `ffmpeg/` area are removed,
including legacy flat `bin/` and `lib/` layouts. Unrelated directories and
system/Homebrew installations are left alone. Cleanup failures are logged and
retried on a later prerequisite check. Video must be stopped during installation;
the installer does not inspect or manage processes.

Windows/Linux retain their existing shared-build rolling URLs. Their identity
records the manifest used to install them, not the exact upstream commit behind
`latest`. There is no automatic upstream freshness check. Increment `revision`
to force a refresh; use a snapshot URL and checksum when exact reproducibility
is required. Switching to static later requires manifest changes to the archive
URL/layout, directory, and required/library-path directories, not launcher code.

For a snapshot update, change its version, installation directory, URLs, and
all three checksums together. Obtain checksums from the provider and validate
the downloaded tools and full Video workflow before release. Keep checksums in
the embedded manifest, not fetched as the trust source during installation.
Preserve the approved archives in project-controlled storage for long-term
availability, with the corresponding sources and license materials required
for redistribution; a provider's historical URL is not a retention guarantee.

The September 6 ARM64 build was tested locally at 1080p30 with a Logitech C930e
and through Video. No Intel Riedl build is selected: regular Intel snapshots
stopped before the needed FFmpeg fix.

