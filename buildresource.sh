#!/bin/sh

# Builds the .syso file for Windows resource (program icon, metadata)
# Run in MINGW64, then build with Go as usual (it will pick up the syso)

set -xe

WINDRES=windres
# (on linux crosscompile)
command -v x86_64-w64-mingw32-windres >/dev/null && WINDRES=x86_64-w64-mingw32-windres
$WINDRES -i ortlinde.rc -O coff -o ortlinde.syso
