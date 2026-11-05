#!/usr/bin/env bash
# Разбирает git-тег релиза: какая это версия и какими тегами публиковать образ.
#
#   git tag -l | scripts/release-meta.sh v1.2.3 >> "$GITHUB_OUTPUT"
#
# На stdin — все теги репозитория: плавающие теги образа не должны откатиться,
# если выпущен патч к старой ветке. На stdout — строки key=value:
#
#   version     версия без «v»: её показывает lumend version
#   tags        теги образа; точная версия всегда последняя, поэтому её наличие
#               в реестре значит, что опубликованы и остальные
#   prerelease  true для vX.Y.Z-<что-то>
#   latest      true, если это самая новая стабильная версия
#
# Схема описана в docs/DEPLOY.md, раздел «Версии и релизы».
set -euo pipefail

tag=${1:?нужен тег вида vX.Y.Z}
num='(0|[1-9][0-9]*)'
if ! [[ $tag =~ ^v$num\.$num\.$num(-[0-9A-Za-z.-]+)?$ ]]; then
  echo "::error::тег «$tag» не подходит: нужен vX.Y.Z или vX.Y.Z-<метка>" >&2
  exit 1
fi
major=${BASH_REMATCH[1]}
minor=${BASH_REMATCH[2]}
prerelease=${BASH_REMATCH[4]}
version=${tag#v}

if [ -n "$prerelease" ]; then
  # Предварительная версия не двигает плавающие теги.
  printf 'version=%s\ntags=%s\nprerelease=true\nlatest=false\n' "$version" "$version"
  exit 0
fi

stable=$({ grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' || true; echo "$tag"; } | sed 's/^v//' | sort -u -V)

is_newest() { # is_newest <префикс>: версия — самая новая среди начинающихся с префикса
  [ "$(grep -E "^${1//./\\.}" <<<"$stable" | tail -n 1)" = "$version" ]
}

tags=$version
is_newest "$major.$minor." && tags="$major.$minor $tags"
# 0.x по SemVer может ломать совместимость в любой версии: тег «0» ничего не обещал бы.
if [ "$major" != 0 ] && is_newest "$major."; then
  tags="$major $tags"
fi
latest=false
if is_newest ""; then
  latest=true
  tags="latest $tags"
fi

printf 'version=%s\ntags=%s\nprerelease=false\nlatest=%s\n' "$version" "$tags" "$latest"
