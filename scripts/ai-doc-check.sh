#!/bin/bash
echo "======================================"
echo "🤖 Agent/AI Documentation Sync Reminder"
echo "======================================"

# 1. Get the diff of staged files
DIFF_CONTENT=$(git diff --cached --stat)
if [ -z "$DIFF_CONTENT" ]; then
    exit 0
fi

# 2. Extract modified files list
CHANGED_FILES=$(git diff --cached --name-only)

# 3. Check if any core backend files are changed but NO markdown files are changed
HAS_CODE_CHANGES=false
HAS_DOC_CHANGES=false

for file in $CHANGED_FILES; do
    if [[ "$file" == *.go ]]; then
        HAS_CODE_CHANGES=true
    elif [[ "$file" == *.md ]]; then
        HAS_DOC_CHANGES=true
    fi
done

# 4. Trigger warning if code changed but docs didn't
if [ "$HAS_CODE_CHANGES" = true ] && [ "$HAS_DOC_CHANGES" = false ]; then
    echo -e "\n⚠️  \033[1;33m[AGENT REMINDER]: You have modified Go code but haven't updated any markdown documents.\033[0m"
    echo "Please consider if these changes require updating:"
    echo "  - docs/BACKEND_GUIDE.md"
    echo "  - AGENTS.md"
    echo "  - API specifications"
    echo ""
    
    # Check if we are running in an interactive terminal
    if [ -t 0 ]; then
        # Handle cross-platform tty for interactive prompts
        exec < /dev/tty
        read -p "Do you want to abort the commit to update docs? (y/N) " yn
        case $yn in
            [Yy]* ) 
                echo "🛑 Commit aborted. Please update docs and try again."
                exit 1
                ;;
            * ) 
                echo "🚀 Acknowledged. Proceeding with commit."
                exit 0
                ;;
        esac
    else
        # In non-interactive mode (like AI Agent), enforce doc updates
        echo "❌ [AGENT ERROR]: Code changes detected but no markdown documentation was updated."
        echo "   As an AI Agent, you MUST maintain the Documentation Tree (Rooted at AGENTS.md)."
        echo "   Please evaluate and update any relevant files in the tree:"
        echo "   - Global Guides: docs/ (e.g., PROJECT_GUIDE.md)"
        echo "   - Module Specs: v-backend/docs/specs/ or architecture/"
        echo "   - Project Context: v-backend/AGENTS.md"
        echo "   - E2E Checklist: v-backend/docs/guides/E2E_TESTING_GUIDE.md"
        echo "   Check all referenced .md files for consistency, accuracy, and completeness."
        exit 1
    fi
else
    echo "✅ Agent checked: Documentation sync looks good or no code changes detected."
    exit 0
fi
