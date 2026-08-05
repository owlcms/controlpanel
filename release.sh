#!/bin/bash
# Default release tag (can be overridden by first CLI argument)
TAG="3.6.0-rc02"
# =============================================================================
# Release script for owlcms-controlpanel
# Uses workflow_dispatch trigger with tag input parameter
# Monitors builds using timestamp-based filtering (owlcms4 approach)
# =============================================================================

set -e

# Allow only release-related files to be dirty
ALLOWED_FILES=(
    "release.sh"
    "ReleaseNotes.md"
)

DIRTY_FILES=()
while IFS= read -r line; do
    [[ -z "$line" ]] && continue
    path="${line:3}"
    DIRTY_FILES+=("$path")
done < <(git status --porcelain)

if ((${#DIRTY_FILES[@]} > 0)); then
    for f in "${DIRTY_FILES[@]}"; do
        allowed=false
        for a in "${ALLOWED_FILES[@]}"; do
            if [[ "$f" == "$a" ]]; then
                allowed=true
                break
            fi
        done
        if [[ "$allowed" == "false" ]]; then
            echo "Error: Working tree has changes outside allowed files."
            echo "Allowed: ${ALLOWED_FILES[*]}"
            echo "Found:   ${DIRTY_FILES[*]}"
            echo "Commit or stash other changes before releasing."
            exit 1
        fi
    done
fi

# Check for gh CLI
if ! command -v gh &> /dev/null; then
    echo "Error: gh CLI is not installed. Please install it first."
    exit 1
fi

# Check authentication
if ! gh auth status &> /dev/null; then
    echo "Error: gh CLI is not authenticated. Run 'gh auth login' first."
    exit 1
fi

# Optional CLI override for tag
if [[ -n "$1" ]]; then
    TAG="$1"
fi

if [[ -z "$TAG" ]]; then
    echo "Error: TAG is empty. Set TAG at top of file or pass it as first argument."
    echo "Example: TAG=1.9.0 or: $0 1.9.0"
    exit 1
fi

# Validate tag format (semver, with optional v prefix)
if [[ ! "$TAG" =~ ^v?[0-9]+\.[0-9]+\.[0-9]+(-alpha[0-9]*|-beta[0-9]*|-rc[0-9]*)?$ ]]; then
    echo "Error: Tag must be semver format: 1.2.3, 1.2.3-alpha1, 1.2.3-beta1, 1.2.3-rc1 (optional leading v)"
    exit 1
fi

# Check if the tag already exists locally or remotely.
if git rev-parse "$TAG" >/dev/null 2>&1; then
    echo "Error: Tag '$TAG' already exists in local repository."
    echo "Use a new version number or delete the tag first:"
    echo "git tag -d $TAG && git push origin --delete $TAG"
    exit 3
fi

if git ls-remote --tags origin | grep -q "refs/tags/${TAG}$"; then
    echo "Error: Tag '$TAG' already exists in remote repository."
    echo "Use a new version number or delete the tag first:"
    echo "git push origin --delete $TAG"
    exit 3
fi

# Get current branch
BRANCH=$(git rev-parse --abbrev-ref HEAD)
if [[ "$BRANCH" == "HEAD" ]]; then
    echo "Error: Detached HEAD state. Checkout a branch before releasing."
    exit 1
fi

REPO="owlcms/owlcms-controlpanel"
WORKFLOW_FILE="release.yaml"

echo "==== Release $TAG for $REPO ===="
echo "Branch: $BRANCH"

# Commit and push allowed release files if they changed
git add -- "${ALLOWED_FILES[@]}" 2>/dev/null || true
if git diff --cached --quiet; then
    echo "No changes to commit in ${ALLOWED_FILES[*]}"
else
    git commit -m "Release ${TAG}"
    git push origin "$BRANCH"
fi

# Get the most recent run ID before triggering (for comparison)
PREV_RUN_ID=$(gh run list --repo "$REPO" --workflow "$WORKFLOW_FILE" --limit 1 --json databaseId --jq '.[0].databaseId // 0')
START_ISO=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
echo "Previous run ID: $PREV_RUN_ID, Start time: $START_ISO"

# Trigger the workflow
# Notarize for release candidates and final releases; skip for alpha/beta to save time.
if [[ "$TAG" != *"-"* || "$TAG" == *"-rc"* ]]; then
    NOTARIZE=true
else
    NOTARIZE=false
fi
echo "Triggering workflow dispatch with tag=$TAG (notarize=$NOTARIZE) on branch $BRANCH..."
gh workflow run "$WORKFLOW_FILE" --repo "$REPO" --ref "$BRANCH" --field tag="$TAG" --field notarize="$NOTARIZE"

# Wait for the new run to appear
echo "Waiting for workflow run to start..."
sleep 10

# Find the new run (started after our timestamp, or with ID > previous)
for i in {1..30}; do
    RUN_ID=$(gh run list --repo "$REPO" --workflow "$WORKFLOW_FILE" --event workflow_dispatch --limit 5 --json databaseId,createdAt --jq "[.[] | select(.createdAt >= \"$START_ISO\" or .databaseId > $PREV_RUN_ID)] | .[0].databaseId // 0")
    
    if [[ "$RUN_ID" != "0" && -n "$RUN_ID" ]]; then
        echo "Found new workflow run: $RUN_ID"
        break
    fi
    echo "Waiting for run to appear (attempt $i/30)..."
    sleep 5
done

if [[ "$RUN_ID" == "0" || -z "$RUN_ID" ]]; then
    echo "Error: Could not find new workflow run after 150 seconds"
    echo "Check https://github.com/$REPO/actions"
    exit 1
fi

# Monitor the workflow
echo ""
echo "==== Monitoring workflow run $RUN_ID ===="
echo "View at: https://github.com/$REPO/actions/runs/$RUN_ID"
echo ""

if ! gh run watch "$RUN_ID" --repo "$REPO" --exit-status; then
    echo ""
    echo "==== Workflow failed; fetching details ===="
    gh run view "$RUN_ID" --repo "$REPO" || true
    gh run view "$RUN_ID" --repo "$REPO" --log-failed || true
    exit 1
fi

echo ""
echo "==== Release $TAG completed successfully! ===="
echo "View release at: https://github.com/$REPO/releases/tag/$TAG"
