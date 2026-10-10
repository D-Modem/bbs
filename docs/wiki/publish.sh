#!/bin/sh
# publish.sh: copy docs/wiki to the repository's GitHub wiki.
#
# The wiki must be on (Settings > Features > Wikis) and have a page already:
# GitHub makes the wiki's repository when its first page is saved, so make
# any page in the web editor once.  Then, from the repository:
#
#   docs/wiki/publish.sh
#
# with push access to Neo-Desktop/bbs-sec (SSH or HTTPS credentials).
set -eu

here=$(cd "$(dirname "$0")" && pwd)
url=${WIKI_URL:-$(git -C "$here" remote get-url origin | sed 's/\.git$//').wiki.git}
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

git clone --quiet --depth 1 "$url" "$tmp/wiki"
# the wiki is this directory's pages, no more and no less
find "$tmp/wiki" -mindepth 1 -maxdepth 1 -name '*.md' -delete
cp "$here"/*.md "$tmp/wiki/"
cd "$tmp/wiki"
git add -A
if git diff --cached --quiet; then
	echo "the wiki is up to date"
	exit 0
fi
rev=$(git -C "$here" rev-parse --short HEAD)
git commit --quiet -m "From docs/wiki at $rev"
git push --quiet origin HEAD
echo "published docs/wiki ($rev) to $url"
