## Release Process

### Steps to Release

1. Decide on a tag number
2. Update the version number in `release.sh` to match the desired tag
3. Update `ReleaseNotes.md` with the changes for this release
4. Run the release script:
   
   ```bash
   ./release.sh
   ```
   
   This script will:
   - Clean up any existing tag/release with the same version
   - Pull latest changes
   - Update build configuration
   - Commit and push all changes (including your version/release notes updates)
   - Create and push the tag, which automatically triggers the GitHub Actions workflow

5. The GitHub Actions workflow will automatically build all platforms and create the release

*Note: The workflow is triggered automatically when the tag is pushed, so no manual workflow triggering is needed.*

#### Building only some platforms

The workflow has boolean inputs `build_mac`, `build_windows`, `build_linux`, `build_raspberry` (Trixie) and `build_raspberry_bookworm`, all `true` by default. A disabled platform's jobs are skipped without starting a runner. If the release for the tag already exists, the files are added to it. For example, to publish only the Bookworm Raspberry Pi packages:

```bash
gh workflow run release.yaml --field tag=3.9.0 \
  --field build_mac=false --field build_windows=false --field build_linux=false --field build_raspberry=false
```
