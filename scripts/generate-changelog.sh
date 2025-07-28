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
    # Show recent commits with usernames instead of full names
    git log --pretty=format:"%H|%s|%ae|%an" --no-merges -20 | while IFS='|' read hash subject email name; do
        if [[ "$email" == *"@users.noreply.github.com" ]]; then
            username=$(echo "$email" | sed 's/@users.noreply.github.com//' | sed 's/^[0-9]*+//')
        elif [[ "$name" =~ ^[a-zA-Z0-9._-]+$ ]] && [[ ${#name} -le 30 ]] && [[ "$name" != *" "* ]]; then
            # If author name looks like a username (no spaces, reasonable chars), use it
            username="$name"
        else
            # If author name has spaces, extract first part of email as username
            email_username=$(echo "$email" | cut -d'@' -f1)
            if [[ "$email_username" =~ ^[a-zA-Z0-9._-]+$ ]] && [[ ${#email_username} -le 20 ]]; then
                username="$email_username"
            else
                username=$(echo "$name" | awk '{print $1}')
            fi
        fi

        # Skip bot contributors
        if [[ "$username" == *"[bot]"* ]] || [[ "$username" == *"bot"* ]]; then
            continue
        fi

        short_hash=$(echo $hash | cut -c1-7)
        echo "- $subject (by @$username) $short_hash"
    done
    echo ""
    printf "${PURPLE}Tip: Create your first release tag to enable proper changelog generation${NC}\n"
    exit 0
fi

# Function to extract username/nickname from git commit
get_username() {
    local commit_hash=$1
    local author_email=$(git log --format="%ae" -n 1 $commit_hash)
    local author_name=$(git log --format="%an" -n 1 $commit_hash)

    # Try to extract GitHub username from noreply email first
    if [[ "$author_email" == *"@users.noreply.github.com" ]]; then
        echo "$author_email" | sed 's/@users.noreply.github.com//' | sed 's/^[0-9]*+//'
    elif [[ "$author_name" =~ ^[a-zA-Z0-9._-]+$ ]] && [[ ${#author_name} -le 30 ]] && [[ "$author_name" != *" "* ]]; then
        # If author name looks like a username (no spaces, reasonable chars), use it
        echo "$author_name"
    else
        # If author name has spaces, extract first part of email as username
        local email_username=$(echo "$author_email" | cut -d'@' -f1)
        if [[ "$email_username" =~ ^[a-zA-Z0-9._-]+$ ]] && [[ ${#email_username} -le 20 ]]; then
            echo "$email_username"
        else
            # Final fallback: use first name only
            echo "$author_name" | awk '{print $1}'
        fi
    fi
}

# Get all commits since last tag
while IFS= read -r commit_hash; do
    # Get commit message and author
    COMMIT_MSG=$(git log --format="%s" -n 1 $commit_hash)
    GITHUB_USER=$(get_username $commit_hash)

    # Skip bot contributors
    if [[ "$GITHUB_USER" == *"[bot]"* ]] || [[ "$GITHUB_USER" == *"bot"* ]]; then
        continue
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
CONTRIBUTOR_COUNT=$(git rev-list $FROM_TAG..$TO_REF --no-merges | while read commit; do
    username=$(get_username $commit)
    # Skip bots
    if [[ "$username" == *"[bot]"* ]] || [[ "$username" == *"bot"* ]]; then
        continue
    fi
    echo "$username"
done | sort | uniq | wc -l | xargs)

printf "${CYAN}Release Statistics${NC}\n"
printf -- "- **%s** commits since %s\n" "$COMMIT_COUNT" "$FROM_TAG"
printf -- "- **%s** contributors\n" "$CONTRIBUTOR_COUNT"
echo ""

# Contributors
printf "${CYAN}Contributors${NC}\n"
printf "Thanks to: "
# Get unique contributors using our username extraction function
git rev-list $FROM_TAG..$TO_REF --no-merges | while read commit; do
    username=$(get_username $commit)
    # Skip bots
    if [[ "$username" == *"[bot]"* ]] || [[ "$username" == *"bot"* ]]; then
        continue
    fi
    echo "$username"
done | sort | uniq | sed 's/^/@/' | tr '\n' ' '
echo ""
