#!/bin/sh

set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
source_png="$script_dir/../../xpra.png"
work_dir=$(mktemp -d /tmp/go-xpra-icon.XXXXXX)
trap 'rm -rf "$work_dir"' EXIT HUP INT TERM

# Use sub-1% alpha only to find the crop rectangle. Cropping the untouched
# source to that rectangle preserves its antialiased edges.
crop_geometry=$(magick "$source_png" \
	-alpha extract -threshold 1% -format '%@' info:)
magick "$source_png" \
	-crop "$crop_geometry" +repage "$work_dir/trimmed.png"

# The inner sizes give 8, 4, 2 and 1 pixels of horizontal padding at 256,
# 128, 64 and 32 respectively. The 24 and 16 pixel images sit nearly flush.
for spec in 256:240 128:120 64:60 48:45 32:30 24:23 16:16; do
	size=${spec%%:*}
	inner=${spec##*:}
	magick "$work_dir/trimmed.png" \
		-filter Lanczos -resize "${inner}x${inner}" \
		-gravity center -background none -extent "${size}x${size}" \
		"$work_dir/xpra-${size}.png"
done

icotool --create --output="$script_dir/xpra.ico" \
	"$work_dir/xpra-256.png" \
	"$work_dir/xpra-128.png" \
	"$work_dir/xpra-64.png" \
	"$work_dir/xpra-48.png" \
	"$work_dir/xpra-32.png" \
	"$work_dir/xpra-24.png" \
	"$work_dir/xpra-16.png"

(
	cd "$script_dir"
	x86_64-w64-mingw32-windres \
		--input icon.rc \
		--output-format coff \
		--output rsrc_windows_amd64.syso
)
