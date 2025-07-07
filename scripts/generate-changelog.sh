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
FROM_TAG=${1:-$(git describe --tags --abbrev=0 2>/dev/null || echo "")}
TO_REF=${2:-HEAD}

# printf "${BLUE}🎉 Peekaping Detailed Changelog Generator${NC}\n"
# echo "=========================================="
# echo ""

if [ -z "$FROM_TAG" ]; then
    printf "${YELLOW}⚠️  No previous release tag found${NC}\n"
    printf "${CYAN}📝 Showing recent commits instead:${NC}\n"
    echo ""
    git log --pretty=format:"- %s (by %an)" --no-merges -20
    echo ""
    printf "${PURPLE}💡 Tip: Create your first release tag to enable proper changelog generation${NC}\n"
    exit 0
fi

# printf "${GREEN}📋 Generating detailed changelog from ${FROM_TAG} to ${TO_REF}${NC}\n"
# echo ""

# Initialize categories
NEW_FEATURES=""
IMPROVEMENTS=""
BUG_FIXES=""
SECURITY_FIXES=""
DOCS_UPDATES=""
CHORES=""
OTHERS=""

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
    else
        # For direct commits, use short hash as reference
        SHORT_HASH=$(echo $commit_hash | cut -c1-7)
        PR_NUM="$SHORT_HASH"
    fi

    # Format the line
    if [ -n "$PR_NUM" ]; then
        LINE="$PR_NUM $COMMIT_MSG (Thanks @$GITHUB_USER)"
    else
        LINE="$COMMIT_MSG (Thanks @$GITHUB_USER)"
    fi

    # Categorize based on commit message
    if echo "$COMMIT_MSG" | grep -qiE "^(feat|feature)"; then
        NEW_FEATURES="$NEW_FEATURES$LINE\n"
    elif echo "$COMMIT_MSG" | grep -qiE "^(fix|bug)"; then
        BUG_FIXES="$BUG_FIXES$LINE\n"
    elif echo "$COMMIT_MSG" | grep -qiE "^(docs|doc)"; then
        DOCS_UPDATES="$DOCS_UPDATES$LINE\n"
    elif echo "$COMMIT_MSG" | grep -qiE "^(security|sec)"; then
        SECURITY_FIXES="$SECURITY_FIXES$LINE\n"
    elif echo "$COMMIT_MSG" | grep -qiE "^(chore|build|ci|deps)"; then
        CHORES="$CHORES$LINE\n"
    elif echo "$COMMIT_MSG" | grep -qiE "(improve|enhance|update|upgrade|optimize|perf)"; then
        IMPROVEMENTS="$IMPROVEMENTS$LINE\n"
    else
        OTHERS="$OTHERS$LINE\n"
    fi

done <<< "$(git rev-list $FROM_TAG..$TO_REF --no-merges)"

# Display categorized changelog
if [ -n "$NEW_FEATURES" ]; then
    printf "${GREEN}## 🚀 New Features${NC}\n"
    printf "$NEW_FEATURES" | sed '/^$/d'
    echo ""
fi

if [ -n "$IMPROVEMENTS" ]; then
    printf "${BLUE}## ⬆️ Improvements${NC}\n"
    printf "$IMPROVEMENTS" | sed '/^$/d'
    echo ""
fi

if [ -n "$BUG_FIXES" ]; then
    printf "${RED}## 🐛 Bug Fixes${NC}\n"
    printf "$BUG_FIXES" | sed '/^$/d'
    echo ""
fi

if [ -n "$SECURITY_FIXES" ]; then
    printf "${PURPLE}## 🔒 Security Fixes${NC}\n"
    printf "$SECURITY_FIXES" | sed '/^$/d'
    echo ""
fi

if [ -n "$DOCS_UPDATES" ]; then
    printf "${CYAN}## 📚 Documentation${NC}\n"
    printf "$DOCS_UPDATES" | sed '/^$/d'
    echo ""
fi

if [ -n "$CHORES" ]; then
    printf "${YELLOW}## 🔧 Maintenance${NC}\n"
    printf "$CHORES" | sed '/^$/d'
    echo ""
fi

if [ -n "$OTHERS" ]; then
    printf "${CYAN}## 📦 Other Changes${NC}\n"
    printf "$OTHERS" | sed '/^$/d'
    echo ""
fi

# Statistics
COMMIT_COUNT=$(git rev-list --count $FROM_TAG..$TO_REF 2>/dev/null | xargs || echo "0")
CONTRIBUTOR_COUNT=$(git log $FROM_TAG..$TO_REF --pretty=format:"%an" | sort | uniq | wc -l | xargs)

printf "${CYAN}## 📊 Release Statistics${NC}\n"
printf -- "- **%s** commits since %s\n" "$COMMIT_COUNT" "$FROM_TAG"
printf -- "- **%s** contributors\n" "$CONTRIBUTOR_COUNT"
echo ""

# Contributors
printf "${CYAN}## 👥 Contributors${NC}\n"
printf "Thanks to: "
git log $FROM_TAG..$TO_REF --pretty=format:"@%an" | sort | uniq | tr '\n' ' '
# echo ""
# echo ""

# echo "=========================================="
# printf "${GREEN}✅ Detailed changelog generated successfully!${NC}\n"
# echo ""
# printf "${PURPLE}💡 Usage tips:${NC}\n"
# echo "• Copy the sections above for your GitHub release"
# echo "• Use conventional commit messages (feat:, fix:, docs:, etc.) for better categorization"
# echo "• PR numbers will be automatically detected from merge commits"
# echo ""
# printf "${BLUE}🚀 Ready to release? Copy this changelog and use it in the GitHub Actions workflow!${NC}\n"
