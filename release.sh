#!/bin/bash

set -e

TAG=$(git describe --tags)

unset GOROOT

git checkout master

echo "Building with tag=$TAG"

sudo -S true

# Special dependency for Windows platform
# GOOS=windows go get -u github.com/yusufpapurcu/wmi

# Compile all platforms, with the translations and providers of the add-on's and Burst's master on GitHub
BUNDLE_SOURCE=github make all

# Run binaries uploads if we are on the tag
if [[ $TAG != *-* ]]
then
	./push-binaries.sh
fi