#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

qemu_path="$(nix build ./nix#nixosConfigurations.workspace.config.system.build.qemuImage --print-out-paths)"
meta_path="$(nix build ./nix#nixosConfigurations.workspace.config.system.build.metadata --print-out-paths)"

qemu_image="$(find "$qemu_path" -maxdepth 2 -type f -name '*.qcow2' -print -quit)"
metadata="$(find "$meta_path" -maxdepth 3 -type f \( -name '*.tar.xz' -o -name '*.tar.gz' \) -print -quit)"

if [[ -z "${qemu_image:-}" || -z "${metadata:-}" ]]; then
  echo "error: could not find Incus VM image artifacts" >&2
  exit 1
fi

# --reuse atomically moves the alias when a newer image is imported.
incus image import "$metadata" "$qemu_image" --alias webnix-base --reuse
incus image info webnix-base >/dev/null

echo "Imported WebNix base image as alias: webnix-base"
