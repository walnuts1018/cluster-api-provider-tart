#!/usr/bin/env bash

set -euo pipefail

source_dir="${1:-artifact/mkosi/mkosi.output}"
output_dir="${2:-dist/os-artifact}"

mkdir -p "$output_dir"

copy_unique() {
  local pattern="$1"
  local destination="$2"
  local -a matches

  mapfile -t matches < <(find "$source_dir" -maxdepth 1 -type f -name "$pattern" -print)
  if [ "${#matches[@]}" -ne 1 ]; then
    echo "Expected exactly one '$pattern' output, found ${#matches[@]}" >&2
    return 1
  fi
  cp --sparse=always "${matches[0]}" "$output_dir/$destination"
}

copy_unique '*.os.raw' os.img
copy_unique '*.verity.raw' os.verity
copy_unique '*.roothash' verity-root-hash
copy_unique '*.vmlinuz*' vmlinuz
copy_unique '*.initrd*' initrd
copy_unique '*.manifest' packages.json
