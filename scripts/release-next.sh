#!/usr/bin/env bash
# Следующий тег версии для коммита в main: patch от самой новой стабильной версии.
# Без стабильных версий — первая, v0.1.0.
#
#   git tag -l | scripts/release-next.sh
set -euo pipefail

newest=$({ grep -E '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$' || true; } | sed 's/^v//' | sort -V | tail -n 1)
if [ -z "$newest" ]; then
  echo v0.1.0
  exit 0
fi
echo "v${newest%.*}.$((${newest##*.} + 1))"
