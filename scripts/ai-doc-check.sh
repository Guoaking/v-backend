#!/bin/bash
echo "======================================"
echo "🤖 AI Docs Sync Checker"
echo "======================================"

# 1. Check if AI Key is configured
if [ -z "$GEMINI_API_KEY" ]; then
    echo "⚠️ GEMINI_API_KEY is not set. Skipping AI doc check."
    exit 0
fi

# 2. Get the diff of staged files
DIFF_CONTENT=$(git diff --cached --stat)
if [ -z "$DIFF_CONTENT" ]; then
    exit 0
fi

# 3. Truncate diff to avoid token limits (first 4000 chars)
SHORT_DIFF=$(git diff --cached | head -c 4000)

echo "🔍 AI is evaluating if this commit requires documentation updates..."

# 4. Prompt for AI
PROMPT="You are a senior architect. Please review the following git diff and evaluate if this code change involves core business logic, API contracts, or configuration changes. If it does, please point out which markdown documents (like README, API docs, etc.) might need to be updated. If it is just a normal bugfix, refactor, or minor change, reply exactly with 'NO_DOCS_NEEDED'. Diff: $SHORT_DIFF"

# 5. Build JSON payload
PAYLOAD=$(jq -n --arg p "$PROMPT" '{"contents":[{"parts":[{"text":$p}]}]}')

# 6. Call Gemini API
RESPONSE=$(curl -s -H "Content-Type: application/json" \
    -d "$PAYLOAD" \
    "https://generativelanguage.googleapis.com/v1beta/models/gemini-1.5-pro:generateContent?key=$GEMINI_API_KEY")

# 7. Parse result
AI_SUGGESTION=$(echo "$RESPONSE" | jq -r '.candidates[0].content.parts[0].text' 2>/dev/null || echo "API_ERROR")

if [[ "$AI_SUGGESTION" == *"NO_DOCS_NEEDED"* ]]; then
    echo "✅ AI evaluated: No documentation updates needed for this commit."
    exit 0
elif [[ "$AI_SUGGESTION" == *"API_ERROR"* || -z "$AI_SUGGESTION" ]]; then
    echo "⚠️ AI API call failed or returned empty. Skipping check."
    exit 0
else
    echo -e "\n⚠️ AI Architect Suggestion:\n"
    echo -e "\033[1;33m$AI_SUGGESTION\033[0m\n"
    
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
                echo "🚀 Ignoring suggestion. Proceeding with commit."
                exit 0
                ;;
        esac
    else
        # In non-interactive mode (like some git GUIs), just warn and let it pass
        echo "⚠️ Non-interactive terminal detected. Passing with warning."
        exit 0
    fi
fi
