#!/bin/sh

# Builds the .syso file for Windows resource (program icon, metadata)
# Run in MINGW64, then build with Go as usual (it will pick up the syso)

set -xe

windres -i ortlinde.rc -O coff -o ortlinde.syso
