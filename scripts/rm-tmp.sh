#!/bin/sh
# rm-tmp.sh DIR... — removes each DIR a script made for itself (mktemp -d,
# viewer-page.sh), and only when it lies under a temp root: /tmp,
# /private/tmp, /var/folders, $TMPDIR or $RUNNER_TEMP. Anything else, a
# path with "..", or a bare root is refused (and said), never removed: a
# trap's `rm -rf "$DIR"` would delete whatever a command happened to print.
# An empty argument is a dir never made, and is skipped.
st=0
under() { # under ROOT PATH: PATH is strictly inside ROOT
  r=${1%/}
  case "$r" in "" | / | .) return 1 ;; esac
  case "$2" in "$r"/?*) return 0 ;; esac
  return 1
}
for d in "$@"; do
  [ -n "$d" ] || continue
  case "$d" in
    */../* | */.. | ../* | ..) echo "rm-tmp: refusing $d (has ..)" >&2; st=1; continue ;;
  esac
  if under /tmp "$d" || under /private/tmp "$d" || under /var/folders "$d" ||
    under "${TMPDIR:-}" "$d" || under "${RUNNER_TEMP:-}" "$d"; then
    rm -rf -- "$d"
  else
    echo "rm-tmp: refusing $d (not under a temp dir)" >&2; st=1
  fi
done
exit $st
