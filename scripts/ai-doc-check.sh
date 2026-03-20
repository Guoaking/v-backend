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
        # In non-interactive mode (like some git GUIs or AI execution), just warn and let it pass
        echo "⚠️  Non-interactive environment. Passing with warning."
        exit 0
    fi
else
    echo "✅ Agent checked: Documentation sync looks good or no code changes detected."
    exit 0
fi
