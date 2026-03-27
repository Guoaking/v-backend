#!/bin/bash
echo "======================================"
echo "🤖 Agent/AI E2E Test Smart Check"
echo "======================================"

# 1. Get the diff of staged files
CHANGED_FILES=$(git diff --cached --name-only)
if [ -z "$CHANGED_FILES" ]; then
  exit 0
fi

HAS_LOGIC_CHANGES=false
HAS_TEST_CHANGES=false
NEEDS_FULL_RUN=false
RECOMMENDED_SUITE=""

# 2. Analyze impact
for file in $CHANGED_FILES; do
  if [[ "$file" == v-backend/internal/api/* ]] || [[ "$file" == v-backend/internal/service/* ]]; then
    HAS_LOGIC_CHANGES=true
    # Determine recommended suite based on filename
    if [[ "$file" == *"kyc"* ]] || [[ "$file" == *"face"* ]] || [[ "$file" == *"liveness"* ]]; then
        RECOMMENDED_SUITE="KYCCoreSuite"
    elif [[ "$file" == *"auth"* ]] || [[ "$file" == *"session"* ]]; then
        RECOMMENDED_SUITE="AuthSecuritySuite"
    elif [[ "$file" == *"org"* ]] || [[ "$file" == *"admin"* ]]; then
        RECOMMENDED_SUITE="AdminOrgSuite"
    elif [[ "$file" == *"user"* ]] || [[ "$file" == *"quota"* ]] || [[ "$file" == *"usage"* ]]; then
        RECOMMENDED_SUITE="ConsoleMgmtSuite"
    fi
  elif [[ "$file" == v-backend/internal/middleware/* ]] || [[ "$file" == v-backend/internal/models/* ]] || [[ "$file" == v-backend/config.* ]]; then
    HAS_LOGIC_CHANGES=true
    NEEDS_FULL_RUN=true
  elif [[ "$file" == v-backend/tests/e2e/* ]]; then
    HAS_TEST_CHANGES=true
  fi
done

# 3. Enforcement Logic
if [ "$HAS_LOGIC_CHANGES" = true ] && [ "$HAS_TEST_CHANGES" = false ]; then
  echo -e "\n❌ \033[1;31m[AGENT ERROR]: Logic changed but E2E tests not updated.\033[0m"
  
  if [ "$NEEDS_FULL_RUN" = true ]; then
    echo "⚠️  CRITICAL: You modified core middleware/models/config. A FULL E2E run is required."
    echo "👉 Run: go test -v ./tests/e2e/..."
  elif [ -n "$RECOMMENDED_SUITE" ]; then
    echo "💡 Target: You modified $RECOMMENDED_SUITE related logic."
    echo "👉 Run: go test -v -run $RECOMMENDED_SUITE ./tests/e2e/..."
  fi
  
  echo "🏃 Quick Check: You can at least run SMOKE tests:"
  echo "👉 Run: go test -v -run Smoke ./tests/e2e/..."
  
  # Block AI Agent in non-interactive mode
  if [ ! -t 0 ]; then
    exit 1
  fi

  # Interactive mode (Human)
  exec </dev/tty
  read -p "Do you want to proceed without updating E2E tests? (y/N) " yn
  case $yn in
    [Yy]*) exit 0 ;;
    *) exit 1 ;;
  esac
fi

exit 0
