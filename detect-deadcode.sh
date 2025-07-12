#!/bin/bash
# File: detect-deadcode.sh

set -euo pipefail

export PATH="$PATH:$(go env GOPATH)/bin"

if ! command -v deadcode &> /dev/null; then
  echo "❌ 'deadcode' not found."
  echo "➡️  Please install it with:"
  echo "    go install github.com/tsenart/deadcode@latest"
  exit 1
fi

echo "🔍 Running deadcode analysis..."

# 분석 대상 디렉터리 리스트
dirs=$(find . -type f -name "*.go" -not -path "./vendor/*" -exec dirname {} \; | sort -u)

# 분석 실행
> deadcode-report.txt
for dir in $dirs; do
  if [[ -f "$dir/go.mod" || -f go.mod ]]; then
    echo "📦 Analyzing $dir"
    deadcode "$dir" >> deadcode-report.txt
  fi
done

if [[ -s deadcode-report.txt ]]; then
  echo -e "\n⚠️  Unused code found (see deadcode-report.txt above)."
else
  echo -e "\n✅ No unused code found!"
fi
