#/bin/sh

function test {
  echo "+ $@"
  "$@"
  local status=$?
  if [ $status -ne 0 ]; then
    exit $status
  fi
  return $status
}

GIT_VERSION=`cd ${GOPATH}/src/github.com/elgatito/elementum; git describe --tags`

if [ "$1" == "local" ]
then
  # This will run with local go
  cd $GOPATH
  set -e
  test go build -ldflags="-w -X github.com/elgatito/elementum/util.Version=${GIT_VERSION}" -o /var/tmp/elementum github.com/elgatito/elementum
  test chmod +x /var/tmp/elementum
  test cp -rf /var/tmp/elementum $HOME/.kodi/addons/plugin.video.elementum/resources/bin/linux_x64/
  test cp -rf /var/tmp/elementum $HOME/.kodi/userdata/addon_data/plugin.video.elementum/bin/linux_x64/
elif [ "$1" == "docker" ]
then
  # This will run with docker libtorrent:linux-x64 image
  cd $GOPATH/src/github.com/elgatito/elementum
  test make linux-x64
  test cp -rf build/linux_x64/elementum $HOME/.kodi/addons/plugin.video.elementum/resources/bin/linux_x64/
  test cp -rf build/linux_x64/elementum $HOME/.kodi/userdata/addon_data/plugin.video.elementum/bin/linux_x64/
fi
