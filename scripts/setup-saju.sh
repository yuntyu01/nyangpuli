#!/usr/bin/env bash
set -euo pipefail
ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
PIN='a8c0cf64adfde8fb1ce253112529edd8e90cc94b'
TARGET="$ROOT_DIR/.upstream/oh-my-saju"
command -v node >/dev/null
command -v git >/dev/null
command -v pnpm >/dev/null || { echo 'Install pnpm 10.15.0 first (npm install -g pnpm@10.15.0).' >&2; exit 1; }
if [[ -e "$TARGET" ]]; then
  [[ -d "$TARGET/.git" ]] || { echo 'Existing upstream directory is not a Git checkout; refusing to overwrite.' >&2; exit 1; }
else
  mkdir -p "$ROOT_DIR/.upstream"
  git init "$TARGET"
  git -C "$TARGET" remote add origin https://github.com/JaeSang1998/oh-my-saju.git
fi
git -C "$TARGET" fetch --depth=1 origin "$PIN"
if [[ -n "$(git -C "$TARGET" status --porcelain --untracked-files=no)" ]]; then
  echo 'Upstream checkout has tracked modifications; refusing to overwrite.' >&2
  exit 1
fi
git -C "$TARGET" checkout --detach "$PIN"
[[ "$(git -C "$TARGET" rev-parse HEAD)" = "$PIN" ]]
cd "$TARGET"
# The bundled dist embeds dependency install paths in comments and source maps.
# Pin the default isolated layout so user-level pnpm settings cannot change it.
pnpm install --frozen-lockfile --config.node-linker=isolated --config.enable-global-virtual-store=false --config.virtual-store-dir=node_modules/.pnpm
SAJU_ENGINE_SOURCE_REVISION="$PIN" node tools/build.mjs
printf 'OhMySaju ready: %s\n' "$PIN"
