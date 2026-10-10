#!/bin/sh
# build.sh NAME URL REF: clone a dosemu2 piece at REF and build its .debs
# into the current directory.
set -e
name=$1 url=$2 ref=${3:-HEAD}
if [ "$ref" = HEAD ]; then
	git clone --depth 1 "$url" "$name"
else
	git clone "$url" "$name"
	git -C "$name" checkout "$ref"
fi
cd "$name"
debuild -i -us -uc -b
