#!/bin/bash
set -eu
# rpm passes the number of instances left once this one is gone (0 on erase,
# 1 on upgrade) and runs this after the new package's postinst. dpkg passes an
# action (remove, purge, upgrade, ...). Only clean up on a real removal, or an
# upgrade would delete what the new package just created.
case "${1:-0}" in
0 | remove | purge)
	rm -f /usr/local/bin/cloudflared
	rm -f /usr/local/etc/cloudflared/.installedFromPackageManager
	;;
esac
