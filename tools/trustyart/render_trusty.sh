#!/usr/bin/env bash
set -euo pipefail

file="${1:-trusty_color_map.txt}"
reset=$'\033[0m'

bg_black=$'\033[48;5;16m'
bg_yellow=$'\033[48;5;222m'
bg_orange=$'\033[48;5;208m'
bg_red=$'\033[48;5;203m'
bg_yellow_dark=$'\033[48;5;172m'
bg_white=$'\033[48;5;231m'
bg_mouth=$'\033[48;5;236m'

render_cell() {
  local ch="$1"
  case "$ch" in
    "█") printf '%s %s' "$bg_black" "$reset" ;;
    "▒") printf '%s %s' "$bg_yellow" "$reset" ;;
    "O") printf '%s %s' "$bg_orange" "$reset" ;;
    "R") printf '%s %s' "$bg_red" "$reset" ;;
    "Y") printf '%s %s' "$bg_yellow_dark" "$reset" ;;
    "w") printf '%s %s' "$bg_white" "$reset" ;;
    "m") printf '%s %s' "$bg_mouth" "$reset" ;;
    "p") printf '%s %s' "$bg_orange" "$reset" ;;
    "r") printf '%s %s' "$bg_red" "$reset" ;;
    "S") printf '%s %s' "$bg_orange" "$reset" ;;
    " ") printf ' ' ;;
    *)   printf ' ' ;;
  esac
}

while IFS= read -r line || [ -n "$line" ]; do
  out=""
  i=0
  len=${#line}
  while [ $i -lt $len ]; do
    ch="${line:$i:1}"
    out+=$(render_cell "$ch")
    i=$((i + 1))
  done
  printf '%b\n' "$out"
done < "$file"
