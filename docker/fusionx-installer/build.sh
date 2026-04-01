#!/bin/bash
# Build the Aziron code-server image with FusionX pre-installed.
# Run from the Aziron workspace root:
#   ./aziron-pulse/docker/fusionx-installer/build.sh
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
VSIX_PATH="${1:-$(find /Users/damirdarasu/workspace/Aziron -name 'fusionx-prod*.vsix' | head -1)}"

if [ -z "$VSIX_PATH" ]; then
    echo "Error: fusionx.vsix not found. Pass the path as first argument."
    echo "Usage: $0 /path/to/fusionx.vsix [image-tag]"
    exit 1
fi

IMAGE_TAG="${2:-aziron/code-server-fusionx:latest}"

echo "Building $IMAGE_TAG using VSIX: $VSIX_PATH"
cp "$VSIX_PATH" "$SCRIPT_DIR/fusionx.vsix"
docker build -t "$IMAGE_TAG" "$SCRIPT_DIR"
rm -f "$SCRIPT_DIR/fusionx.vsix"
echo "Done: $IMAGE_TAG"
