#!/bin/sh
# Push binaries to elementum-binaries repo
make binaries
cd binaries && git remote add binaries https://$GH_TOKEN@github.com/elgatito/elementum-binaries
git push binaries master
if [ $? -ne 0 ]; then
  cd .. && rm -rf binaries
  exit 1
fi
