#!/bin/bash

# Enhanced Changelog Generator Script
# Usage: ./scripts/generate-changelog.sh [from_tag] [to_ref]

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
PURPLE='\033[0;35m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Get arguments
# Function to get latest stable tag (ignoring release candidates)
get_latest_stable_tag() {
    git tag -l | grep -vE '(-rc|-alpha|-beta|-pre)' | sort -V | tail -1 2>/dev/null || echo ""
}

FROM_TAG=${1:-$(get_latest_stable_tag)}
TO_REF=${2:-HEAD}

if [ -z "$FROM_TAG" ]; then
    printf "${YELLOW}No previous release tag found${NC}\n"
    printf "${CYAN}Showing recent commits instead:${NC}\n"
    echo ""
    git log --pretty=format:"- %s (by %an) %h" --no-merges -20
    echo ""
    printf "${PURPLE}Tip: Create your first release tag to enable proper changelog generation${NC}\n"
    exit 0
fi

printf "${GREEN}Generating changelog from ${FROM_TAG} to ${TO_REF}${NC}\n"
echo ""

# Get all commits since last tag
while IFS= read -r commit_hash; do
    # Get commit message and author
    COMMIT_MSG=$(git log --format="%s" -n 1 $commit_hash)
    AUTHOR=$(git log --format="%an" -n 1 $commit_hash)
    AUTHOR_EMAIL=$(git log --format="%ae" -n 1 $commit_hash)

    # Try to extract GitHub username from commit
    GITHUB_USER=""
    if [[ "$AUTHOR_EMAIL" == *"@users.noreply.github.com" ]]; then
        GITHUB_USER=$(echo "$AUTHOR_EMAIL" | sed 's/@users.noreply.github.com//' | sed 's/^[0-9]*+//')
    else
        # Fallback to author name
        GITHUB_USER="$AUTHOR"
    fi

    # Check if this is a merge commit (has PR number)
    PR_NUM=""
    MERGE_COMMIT=$(git log --merges --format="%H %s" $FROM_TAG..$TO_REF | grep "^$commit_hash" || echo "")

    if [ -n "$MERGE_COMMIT" ]; then
        # This commit is related to a merge, try to find the PR number
        MERGE_MSG=$(echo "$MERGE_COMMIT" | cut -d' ' -f2-)
        if echo "$MERGE_MSG" | grep -qE "Merge pull request #[0-9]+"; then
            PR_NUM=$(echo "$MERGE_MSG" | grep -oE "#[0-9]+" | head -1)
        fi
    fi

    # Format the line with commit hash at the end
    SHORT_HASH=$(echo $commit_hash | cut -c1-7)
    if [ -n "$PR_NUM" ]; then
        LINE="- $COMMIT_MSG (Thanks @$GITHUB_USER) $PR_NUM $SHORT_HASH"
    else
        LINE="- $COMMIT_MSG (Thanks @$GITHUB_USER) $SHORT_HASH"
    fi

    echo "$LINE"

done <<< "$(git rev-list $FROM_TAG..$TO_REF --no-merges)"

echo ""

# Statistics
COMMIT_COUNT=$(git rev-list --count $FROM_TAG..$TO_REF 2>/dev/null | xargs || echo "0")
CONTRIBUTOR_COUNT=$(git log $FROM_TAG..$TO_REF --pretty=format:"%an" | sort | uniq | wc -l | xargs)

printf "${CYAN}Release Statistics${NC}\n"
printf -- "- **%s** commits since %s\n" "$COMMIT_COUNT" "$FROM_TAG"
printf -- "- **%s** contributors\n" "$CONTRIBUTOR_COUNT"
echo ""

# Contributors
printf "${CYAN}Contributors${NC}\n"
printf "Thanks to: "
git log $FROM_TAG..$TO_REF --pretty=format:"@%an" | sort | uniq | tr '\n' ' '
echo ""
