#!/bin/bash
set -e

#
# This script downloads the latest builds of pi-bell from GitHub releases
# and copies them to /usr/local/bin/pi-bell
# For details of setting as systemd services and configuration 
# see https://github.com/stuartleeks/pi-bell/blob/main/README.md
#


INSTALL_FOLDER="/usr/local/bin/pi-bell"

echo "Looking up latest version..."
LATEST_VERSION=$(curl -s https://api.github.com/repos/stuartleeks/pi-bell/releases/latest | grep "\"tag_name\": "  | sed -E 's/.*\"tag_name\": \"(.*)\",/\1/g')

echo "Found $LATEST_VERSION"

echo "Downloading..."
wget -O /tmp/pi-bell.tar.gz https://github.com/stuartleeks/pi-bell/releases/download/$LATEST_VERSION/pi-bell.tar.gz



echo "Extracting files to $INSTALL_FOLDER"
mkdir -p "$INSTALL_FOLDER"
tar -xzvf /tmp/pi-bell.tar.gz -C "$INSTALL_FOLDER"


echo "Downloading go2rtc..."
# go2rtc is shipped as its own prebuilt binary (not built by the pi-bell Makefile),
# so fetch the release binary that matches this Pi's architecture.
GO2RTC_VERSION="v1.9.4"
case "$(uname -m)" in
	armv6l)
		GO2RTC_ARCH="arm" ;;
	armv7l)
		GO2RTC_ARCH="arm" ;;
	aarch64 | arm64)
		GO2RTC_ARCH="arm64" ;;
	x86_64 | amd64)
		GO2RTC_ARCH="amd64" ;;
	*)
		echo "Unknown architecture $(uname -m); skipping go2rtc download." >&2
		GO2RTC_ARCH="" ;;
esac

if [ -n "$GO2RTC_ARCH" ]; then
	wget -O "$INSTALL_FOLDER/go2rtc" "https://github.com/AlexxIT/go2rtc/releases/download/${GO2RTC_VERSION}/go2rtc_linux_${GO2RTC_ARCH}"
	chmod +x "$INSTALL_FOLDER/go2rtc"
	echo "Installed go2rtc ${GO2RTC_VERSION} (${GO2RTC_ARCH})"
fi

echo "Add $INSTALL_FOLDER to your PATH"

