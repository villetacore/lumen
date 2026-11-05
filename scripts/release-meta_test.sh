#!/usr/bin/env bash
# Тест release-meta.sh: bash scripts/release-meta_test.sh
set -uo pipefail

meta="$(dirname "$0")/release-meta.sh"
failed=0

# check <тег> <существующие теги через пробел> <ожидаемые строки через «;»>
check() {
  local want got
  want=$(tr ';' '\n' <<<"$3")
  got=$(tr ' ' '\n' <<<"$2" | bash "$meta" "$1" 2>/dev/null)
  if [ "$got" != "$want" ]; then
    printf 'FAIL %s при тегах [%s]\n  ждали: %s\n  вышло: %s\n' "$1" "$2" "$want" "$got"
    failed=1
  fi
}

# rejects <тег>: тег не проходит, ничего не публикуется
rejects() {
  if bash "$meta" "$1" </dev/null >/dev/null 2>&1; then
    echo "FAIL тег «$1» должен быть отклонён"
    failed=1
  fi
}

check v0.1.0 "" 'version=0.1.0;tags=latest 0.1 0.1.0;prerelease=false;latest=true'
check v1.2.3 "v1.2.2 v1.1.9" 'version=1.2.3;tags=latest 1 1.2 1.2.3;prerelease=false;latest=true'
# Сам тег уже есть в списке: на настоящем прогоне так всегда.
check v1.2.3 "v1.2.2 v1.2.3" 'version=1.2.3;tags=latest 1 1.2 1.2.3;prerelease=false;latest=true'
# Числа сравниваются как числа, а не как строки.
check v1.10.0 "v1.9.0" 'version=1.10.0;tags=latest 1 1.10 1.10.0;prerelease=false;latest=true'
check v1.9.1 "v1.10.0 v1.9.0" 'version=1.9.1;tags=1.9 1.9.1;prerelease=false;latest=false'
# Патч к старой ветке и старая старшая версия не забирают плавающие теги.
check v1.2.4 "v1.3.0 v1.2.3" 'version=1.2.4;tags=1.2 1.2.4;prerelease=false;latest=false'
check v1.5.0 "v2.0.0" 'version=1.5.0;tags=1 1.5 1.5.0;prerelease=false;latest=false'
check v1.2.3 "v1.2.4 v1.3.0" 'version=1.2.3;tags=1.2.3;prerelease=false;latest=false'
# Соседние числа не считаются одной веткой.
check v1.2.3 "v11.0.0 v1.20.0" 'version=1.2.3;tags=1.2 1.2.3;prerelease=false;latest=false'
# Предварительные версии не двигают плавающие теги и не мешают стабильным.
check v2.0.0-rc.1 "v1.0.0" 'version=2.0.0-rc.1;tags=2.0.0-rc.1;prerelease=true;latest=false'
check v2.0.0 "v2.0.0-rc.1 v3.0.0-beta" 'version=2.0.0;tags=latest 2 2.0 2.0.0;prerelease=false;latest=true'
# Посторонние теги не мешают.
check v1.0.0 "foo v9 v9.9 release-9.9.9" 'version=1.0.0;tags=latest 1 1.0 1.0.0;prerelease=false;latest=true'

for bad in "" 1.2.3 v1 v1.2 v1.2.3.4 v01.2.3 v1.02.3 v1.2.03 v1.2.3- v1.2.3+build v1.2.3-rc_1 "v1.2.3 x"; do
  rejects "$bad"
done

# next <существующие теги через пробел> <ожидаемый тег>
next() {
  local got
  got=$(tr ' ' '\n' <<<"$1" | bash "$(dirname "$0")/release-next.sh")
  if [ "$got" != "$2" ]; then
    printf 'FAIL следующий при тегах [%s]: ждали %s, вышло %s\n' "$1" "$2" "$got"
    failed=1
  fi
}

next "" v0.1.0
next "foo v9 v9.9 v0.2.0-rc.1" v0.1.0
next "v0.1.0" v0.1.1
next "v0.1.9 v0.1.10" v0.1.11
next "v0.1.5 v0.2.0" v0.2.1
next "v1.9.0 v1.10.0 v0.9.9" v1.10.1
# Предварительная версия и посторонний тег номер не занимают.
next "v0.1.5 v0.2.0-rc.1 v99.0.0.1 v01.0.0" v0.1.6

[ "$failed" = 0 ] && echo "release-meta: ok"
exit "$failed"
