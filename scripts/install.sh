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


echo "Add $INSTALL_FOLDER to your PATH"

