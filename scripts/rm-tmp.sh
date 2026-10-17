#!/bin/sh
# rm-tmp.sh DIR... — removes each DIR a script made for itself (mktemp -d,
# viewer-page.sh), and only when it lies under a temp root: /tmp,
# /private/tmp, /var/folders (/private/var/folders), $TMPDIR or
# $RUNNER_TEMP. Anything else, a path with "..", or a bare root is refused
# (and said), never removed: a trap's `rm -rf "$DIR"` would delete whatever
# a command happened to print. An empty argument is a dir never made, and is
# skipped.
#
# What is checked is where DIR really is: its parent with every link
# resolved, so /tmp/link/sub with the link into a home is not "under /tmp".
# $TMPDIR and $RUNNER_TEMP count only when at least three levels deep and
# not $HOME or above it: TMPDIR=$HOME (or /home) would otherwise make a
# whole home a temp root.
st=0
# real PATH: PATH with its parent's links resolved ("" if it has none).
real() {
  p=$(CDPATH= cd -P -- "$(dirname -- "$1")" 2>/dev/null && pwd -P) || return 1
  printf '%s/%s\n' "${p%/}" "$(basename -- "$1")"
}
# under ROOT PATH: PATH is strictly inside ROOT (both as given).
under() {
  r=${1%/}
  case "$r" in "" | / | .) return 1 ;; esac
  case "$2" in "$r"/?*) return 0 ;; esac
  return 1
}
# envroot DIR: DIR resolved, if it may serve as a temp root ("" if not).
envroot() {
  [ -n "$1" ] || return 0
  r=$(CDPATH= cd -P -- "$1" 2>/dev/null && pwd -P) || return 0
  case "$r" in /*/*/?*) ;; *) return 0 ;; esac # three levels at least
  h=$(CDPATH= cd -P -- "${HOME:-/nonexistent}" 2>/dev/null && pwd -P) || h=
  if [ -n "$h" ]; then
    case "$h" in "$r" | "$r"/*) return 0 ;; esac # $HOME, or above it
  fi
  printf '%s\n' "$r"
}
troot=$(envroot "${TMPDIR:-}")
rroot=$(envroot "${RUNNER_TEMP:-}")
for d in "$@"; do
  [ -n "$d" ] || continue
  case "$d" in
    */../* | */.. | ../* | ..) echo "rm-tmp: refusing $d (has ..)" >&2; st=1; continue ;;
  esac
  rd=$(real "$d") || rd=
  if [ -n "$rd" ] && { under /tmp "$rd" || under /private/tmp "$rd" ||
    under /var/folders "$rd" || under /private/var/folders "$rd" ||
    under "$troot" "$rd" || under "$rroot" "$rd"; }; then
    rm -rf -- "$d"
  else
    echo "rm-tmp: refusing $d (not under a temp dir)" >&2; st=1
  fi
done
exit $st
