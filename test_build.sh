#/bin/sh

set -x
function test {
  echo "+ $@"
  "$@"
  local status=$?
  if [ $status -ne 0 ]; then
    exit $status
  fi
  return $status
}

function check_exit_code() {
    if [ ! \( $1 -eq 0 -o $1 -eq 52 \) ]; then # curl: (52) Empty reply from server
        echo "Elementum is not running"
    else
        echo "Action was successful"
    fi
}

function manage {
  if [[ "${DEST_MANAGE}" == *"-local"* ]]; then
    DEST_HOST="127.0.0.1"
  else
    DEST_HOST="$winhost"
  fi

  set +e
  if [[ "${DEST_MANAGE}" == *"restart"* ]]; then
    echo "Restarting Elementum at $DEST_HOST"
    curl --connect-timeout 2 -s $DEST_HOST:65220/restart
    check_exit_code $?
  elif [[ "${DEST_MANAGE}" == *"shutdown"* ]]; then
    echo "Shutting down Elementum at $DEST_HOST"
    curl --connect-timeout 2 -s $DEST_HOST:65220/shutdown
    check_exit_code $?
  elif [[ "${DEST_MANAGE}" == *"kill"* ]]; then
    echo "Killing Elementum at $DEST_HOST"
    killall -q elementum
    check_exit_code $?
  fi
  set -e

}

if [ -d "${GOPATH}/src/github.com/elgatito/elementum" ] && [ -f "${GOPATH}/src/github.com/elgatito/elementum/main.go" ];
then
  GIT_VERSION=`cd ${GOPATH}/src/github.com/elgatito/elementum; git describe --tags`
else
  GIT_VERSION=`git describe --tags`
fi

DEST_ACTION=$1
DEST_MANAGE=$2

DEST_NAME=elementum
DEST_LIBRARY=elementum.so
DEST_MAKE=linux-x64
DEST_PLATFORM=linux_x64
DEST_DIR="${DEST_DIR:-$HOME/.kodi}"
if [[ ! -z "${WSL_USER}" ]] && [[ "${DEST_MANAGE}" != *"local"* ]]; then
  DEST_NAME=elementum.exe
  DEST_LIBRARY=elementum.dll
  DEST_DIR=/mnt/c/Users/${WSL_USER}/AppData/Roaming/Kodi
  DEST_PLATFORM=windows_x64
  DEST_MAKE=windows-x64
fi

LOCAL_ENV=$GOPATH/src/github.com/ElementumOrg/libtorrent-go/local-env/
if [ -d "$CROSS_ROOT" ];
then
    LOCAL_ENV=$CROSS_ROOT
fi

TAGS="binary,go_json"

# This will run with local go using libtorrent-go/local-env/ locally copied dependencies compilation.
export LOCAL_ENV=$LOCAL_ENV
export PATH=$PATH:$LOCAL_ENV/bin/
export PKG_CONFIG_PATH=$LOCAL_ENV/lib/pkgconfig
export SWIG_LIB=$LOCAL_ENV/share/swig/4.1.1/

if [ "${DEST_ACTION}" == "local" ]
then
  set -e
  test go build $GO_FLAGS -ldflags="-w -X github.com/elgatito/elementum/util/ident.Version=${GIT_VERSION}" -tags $TAGS -o /var/tmp/${DEST_NAME} .
  shasum -b /var/tmp/${DEST_NAME} | cut -d' ' -f1 >> /var/tmp/${DEST_NAME}
  test chmod +x /var/tmp/elementum*
  mkdir -p $DEST_DIR/addons/plugin.video.elementum/resources/bin/$DEST_PLATFORM/
  mkdir -p $DEST_DIR/userdata/addon_data/plugin.video.elementum/bin/$DEST_PLATFORM/
  test cp -rf /var/tmp/elementum* $DEST_DIR/addons/plugin.video.elementum/resources/bin/$DEST_PLATFORM/
  test cp -rf /var/tmp/elementum* $DEST_DIR/userdata/addon_data/plugin.video.elementum/bin/$DEST_PLATFORM/
  chmod +x $DEST_DIR/addons/plugin.video.elementum/resources/bin/$DEST_PLATFORM/${DEST_NAME}
  chmod +x $DEST_DIR/userdata/addon_data/plugin.video.elementum/bin/$DEST_PLATFORM/${DEST_NAME}
  manage
elif [ "${DEST_ACTION}" == "library" ]
then
  TAGS="shared,go_json"
  set -e
  test go build -ldflags="-w -X github.com/elgatito/elementum/util/ident.Version=${GIT_VERSION}" -tags $TAGS -buildmode=c-shared -o /var/tmp/${DEST_LIBRARY} .
  test chmod +x /var/tmp/elementum*
  mkdir -p $DEST_DIR/addons/plugin.video.elementum/resources/bin/$DEST_PLATFORM/
  mkdir -p $DEST_DIR/userdata/addon_data/plugin.video.elementum/bin/$DEST_PLATFORM/
  test cp -rf /var/tmp/elementum* $DEST_DIR/addons/plugin.video.elementum/resources/bin/$DEST_PLATFORM/
  test cp -rf /var/tmp/elementum* $DEST_DIR/userdata/addon_data/plugin.video.elementum/bin/$DEST_PLATFORM/
  manage
elif [ "${DEST_ACTION}" == "sanitize" ]
then
  # This will run with local go
  set -e
  CGO_ENABLED=1 CGO_LDFLAGS='-fsanitize=leak -fsanitize=address' CGO_CFLAGS='-fsanitize=leak -fsanitize=address' test go build -ldflags="-w -X github.com/elgatito/elementum/util/ident.Version=${GIT_VERSION}" -tags $TAGS -o /var/tmp/elementum github.com/elgatito/elementum
  test chmod +x /var/tmp/elementum*
  mkdir -p $DEST_DIR/addons/plugin.video.elementum/resources/bin/$DEST_PLATFORM/
  mkdir -p $DEST_DIR/userdata/addon_data/plugin.video.elementum/bin/$DEST_PLATFORM/
  test cp -rf /var/tmp/elementum* $DEST_DIR/addons/plugin.video.elementum/resources/bin/$DEST_PLATFORM/
  test cp -rf /var/tmp/elementum* $DEST_DIR/userdata/addon_data/plugin.video.elementum/bin/$DEST_PLATFORM/
  manage
elif [ "${DEST_ACTION}" == "docker" ]
then
  # This will run with docker libtorrent:$DEST_MAKE image
  test make $DEST_MAKE
  mkdir -p $DEST_DIR/addons/plugin.video.elementum/resources/bin/$DEST_PLATFORM/
  mkdir -p $DEST_DIR/userdata/addon_data/plugin.video.elementum/bin/$DEST_PLATFORM/
  test cp -rf build/$DEST_PLATFORM/elementum* $DEST_DIR/addons/plugin.video.elementum/resources/bin/$DEST_PLATFORM/
  test cp -rf build/$DEST_PLATFORM/elementum* $DEST_DIR/userdata/addon_data/plugin.video.elementum/bin/$DEST_PLATFORM/
  manage
elif [ "${DEST_ACTION}" == "docker-library" ]
then
  # This will run with docker libtorrent:$DEST_MAKE image
  test make ${DEST_MAKE}-shared
  mkdir -p $DEST_DIR/addons/plugin.video.elementum/resources/bin/$DEST_PLATFORM/
  mkdir -p $DEST_DIR/userdata/addon_data/plugin.video.elementum/bin/$DEST_PLATFORM/
  test cp -rf build/${DEST_PLATFORM}/elementum.* $DEST_DIR/addons/plugin.video.elementum/resources/bin/$DEST_PLATFORM/
  test cp -rf build/${DEST_PLATFORM}/elementum.* $DEST_DIR/userdata/addon_data/plugin.video.elementum/bin/$DEST_PLATFORM/
  manage
fi
