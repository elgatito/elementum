Elementum daemon [![Build Status](https://gitlab.com/elgatito/elementum/badges/master/pipeline.svg?ignore_skipped=true)](https://gitlab.com/elgatito/elementum)
======

Fork of the great [Pulsar daemon](https://github.com/steeve/pulsar) and [Quasar daemon](https://github.com/scakemyer/quasar)

# Supported platforms

Each platform is built as a binary (`make <platform>`) and a shared library (`make <platform>-shared`):

| Platform | Binary folder | Minimum OS |
| -------- | ------------- | ---------- |
| Windows x64 / x86 | `windows_x64`, `windows_x86` | Windows 10 / Windows Server 2016 |
| Linux x64 / x86 | `linux_x64`, `linux_x86` | glibc 2.28 (Debian 10, Ubuntu 20.04, RHEL 8), kernel 3.2 |
| Linux arm64 | `linux_arm64` | glibc 2.28, kernel 3.2 |
| Linux armv7 (hard-float / softfp) | `linux_armv7`, `linux_armv7_softfp` | glibc 2.28, kernel 3.2 |
| Linux armv6 (Raspberry Pi 1 / Zero) | `linux_armv6` | glibc 2.10, kernel 3.2 |
| Android arm / arm64 / x86 / x64 | `android_arm`, `android_arm64`, `android_x86`, `android_x64` | Android 5.0 (API 21) |
| macOS Intel | `darwin_x64` | macOS 13 Ventura |
| macOS Apple Silicon | `darwin_arm64` | macOS 13 Ventura |

# Easy development environment set up

There is an easy way to prepare local environment for running/compiling Elementum.

1. Clone `libtorrent-go` repository:
```
git clone git@github.com:ElementumOrg/libtorrent-go.git
```
2. Run `make local-env` that will create `local-env` directory will all required components.
```
make local-env
```
3. Modify `LOCAL-ENV` variable in `test_build.sh` to use `local-env` directory:
```
  export LOCAL_ENV=$GOPATH/src/github.com/ElementumOrg/libtorrent-go/local-env/
```

Or you can apply environment variables and compile in any other way:
```
  export LOCAL_ENV=$GOPATH/src/github.com/ElementumOrg/libtorrent-go/local-env/
  export PATH=$PATH:$LOCAL_ENV/bin/
  export PKG_CONFIG_PATH=$LOCAL_ENV/lib/pkgconfig
  export SWIG_LIB=$LOCAL_ENV/share/swig/4.1.1/
```

# Setting up Go workspace for using local environment for libtorrent-go

By default Go will fetch `libtorrent-go` module from Github. 
To use local version of `libtorrent-go` we can set up the Go workspace, by adding `go.work` file inside `elementum directory:

```

go 1.26.0

use (
	.
	../libtorrent-go
)

// Enable for local development of libtorrent-go
replace github.com/ElementumOrg/libtorrent-go v0.0.0 => ../libtorrent-go

```

That would expect `libtorrent-go` folder to be available at `../libtorrent-go`.
That would be also used when running compilation with `test_build.sh`, but would not work with `Docker` runs, as it is forced to ignore Go workspaces.

# How to run

1. Build the [cross-compiler](https://github.com/ElementumOrg/cross-compiler) images,
    or alternatively, pull the cross-compiler images from [Docker Hub](https://hub.docker.com/r/elementumorg/cross-compiler):

    ```
    make pull-all
    ```

    Or for a specific platform:
    ```
    make pull PLATFORM=android-x64
    ```

2. Set GOPATH

    ```
    export GOPATH="~/go"
    ```

3. go get

    ```
    go get -d github.com/elgatito/elementum
    ```

    For Windows support, but required for all builds, you also need:

    ```
    go get github.com/mattn/go-isatty
    ```

4. Build libtorrent-go libraries:

    ```
    make libs
    ```

5. Make specific platforms, or all of them:

    Linux-x64
    ```
    make linux-x64
    ```

    Darwin-x64 / Darwin-arm64 (Apple Silicon)
    ```
    make darwin-x64
    make darwin-arm64
    ```

    Windows
    ```
    make windows-x86
    ```

    All platforms (4 builds in parallel; use `make JOBS=1` to build one at a time with full live output)
    ```
    make
    ```

Find memory leaks

To find memory leaks, we can use Valgrind or use Sanitizers.

```
/bin/bash test_build.sh sanitize
```
This will build the binary with enabled sanitizer, so just run it and wait for errors in the console.

```
valgrind --leak-check=full ./elementum -disableBackup
```
This will run the binary with Valgrind. When you cose the process, Valgrind will show statistics.
It is better to use usual binary, without sanitizer, and add backup disable options, as well as disable Kodi library integration. Or it will take a lot of time/CPU at the startup (unless you do need it).

# How to release

Release of a binary part of Elementum means compiling binaries for all platforms and putting them into <https://github.com/elgatito/elementum-binaries> repository (if we run on the git tag).

Release is done with `release.sh` script.