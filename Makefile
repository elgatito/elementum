CC = cc
CXX = c++
STRIP = strip
RACE = 

include platform_host.mk

ifneq ($(CROSS_TRIPLE),)
	CC := $(CROSS_TRIPLE)-$(CC)
	CXX := $(CROSS_TRIPLE)-$(CXX)
	STRIP := $(CROSS_TRIPLE)-strip
endif

include platform_target.mk

IS_SHARED = no
ifneq (,$(findstring shared, $(TARGET_SHARED)))
    IS_SHARED = yes
endif

ifneq ($(IS_RACE),)
	RACE := -race
endif

ifeq ($(TARGET_ARCH), x86)
	GOARCH = 386
else ifeq ($(TARGET_ARCH), x64)
	GOARCH = amd64
else ifeq ($(TARGET_ARCH), arm)
	GOARCH = arm
	GOARM = 6
else ifeq ($(TARGET_ARCH), armv6)
	GOARCH = arm
	GOARM = 6
else ifeq ($(TARGET_ARCH), armv7)
	GOARCH = arm
	GOARM = 7
	PKGDIR = -pkgdir /go/pkg/linux_armv7
else ifeq ($(TARGET_ARCH), armv7_softfp)
	CUSTOM_CFLAGS += -march=armv7-a -mfloat-abi=softfp -ffast-math
	CUSTOM_LDFLAGS += -march=armv7-a -mfloat-abi=softfp -ffast-math
	GOARCH = arm
	GOARM = 7
	PKGDIR = -pkgdir /go/pkg/linux_armv7
else ifeq ($(TARGET_ARCH), arm64)
	GOARCH = arm64
	GOARM =
endif

ifeq ($(TARGET_OS), windows)
	ifeq ($(IS_SHARED), no)
		EXT = .exe
	else
		EXT = .dll
	endif

	GOOS = windows
else ifeq ($(TARGET_OS), darwin)
	ifeq ($(IS_SHARED), no)
		EXT = 
	else
		EXT = .so
	endif

	GOOS = darwin
	# Needs this or cgo will try to link with libgcc, which will fail
	CC := $(CROSS_ROOT)/bin/$(CROSS_TRIPLE)-clang
	CXX := $(CROSS_ROOT)/bin/$(CROSS_TRIPLE)-clang++
	GO_LDFLAGS += -linkmode=external -extld=$(CC) -extldflags "-lm"
else ifeq ($(TARGET_OS), linux)
	ifeq ($(IS_SHARED), no)
		EXT = 
		GO_LDFLAGS += -linkmode=external -extld=$(CC) -extldflags "-L $(CROSS_ROOT)/lib/ -lm -lstdc++ $(CUSTOM_LDFLAGS)"
	else
		EXT = .so
		GO_LDFLAGS += -linkmode=external -extld=$(CC) -extldflags "$(CUSTOM_LDFLAGS)"
	endif

	GOOS = linux
else ifeq ($(TARGET_OS), android)
	ifeq ($(IS_SHARED), no)
		EXT = 
		GO_LDFLAGS += -linkmode=external -extld=$(CC) -extldflags "-pie -lm" 
	else
		EXT = .so
	endif

	GOOS = android
	ifeq ($(TARGET_ARCH), arm)
		GOARM = 7
	else
		GOARM =
	endif
	CC := $(CROSS_ROOT)/bin/$(CROSS_TRIPLE)-clang
	CXX := $(CROSS_ROOT)/bin/$(CROSS_TRIPLE)-clang++
endif

PROJECT = elementumorg
NAME = elementum
GO_PKG = github.com/elgatito/elementum
GO = go
GIT = git
DOCKER = docker
DOCKER_IMAGE = elementumorg/libtorrent-go
UPX = upx
GIT_VERSION = $(shell $(GIT) describe --tags)
CGO_ENABLED = 1
OUTPUT_NAME = $(NAME)$(EXT)
LIBTORRENT_GO = github.com/ElementumOrg/libtorrent-go
LIBTORRENT_GO_HOME = $(shell go env GOPATH)/src/$(LIBTORRENT_GO)
GO_BUILD_TAGS =
GO_LDFLAGS += -s -w -X $(GO_PKG)/util/ident.Version=$(GIT_VERSION)
GO_EXTRALDFLAGS =

ifeq ($(IS_SHARED), no)
	BUILD_PATH = build/$(TARGET_OS)_$(TARGET_ARCH)
	BUILD_MODE = -tags binary,go_json
else
	BUILD_PATH = build/$(TARGET_OS)_$(TARGET_ARCH)
	BUILD_MODE = -buildmode=c-shared -tags shared,go_json
endif


ANDROID_PLATFORMS = \
	android-arm \
	android-arm-shared \
	android-arm64 \
	android-arm64-shared \
	android-x64 \
	android-x64-shared \
	android-x86 \
	android-x86-shared
LINUX_PLATFORMS = \
	linux-armv6 \
	linux-armv6-shared \
	linux-armv7 \
	linux-armv7-shared \
	linux-armv7_softfp \
	linux-armv7_softfp-shared \
	linux-arm64 \
	linux-arm64-shared \
	linux-x64 \
	linux-x64-shared \
	linux-x86 \
	linux-x86-shared
WINDOWS_PLATFORMS = \
	windows-x64 \
	windows-x64-shared \
	windows-x86 \
	windows-x86-shared
DARWIN_PLATFORMS = \
	darwin-x64 \
	darwin-x64-shared

PLATFORMS =	$(ANDROID_PLATFORMS) $(LINUX_PLATFORMS) $(WINDOWS_PLATFORMS) $(DARWIN_PLATFORMS)


.PHONY: $(PLATFORMS)

all:
	for i in $(PLATFORMS); do \
		$(MAKE) $$i; \
	done

client:
	mkdir -p $(BUILD_PATH)/client

$(PLATFORMS):
	$(MAKE) build TARGET_OS=$(firstword $(subst -, ,$@)) TARGET_ARCH=$(word 2, $(subst -, ,$@)) TARGET_SHARED=$(word 3, $(subst -, ,$@))

force:
	@true

libtorrent-go: force
	$(MAKE) -C $(LIBTORRENT_GO_HOME) $(PLATFORM)

$(BUILD_PATH):
	mkdir -p $(BUILD_PATH)

$(BUILD_PATH)/$(OUTPUT_NAME): $(BUILD_PATH) force
	LDFLAGS='$(LDFLAGS)' \
	CFLAGS='$(CFLAGS) -std=c++11' \
	CC='$(CC)' CXX='$(CXX)' \
	GOOS='$(GOOS)' GOARCH='$(GOARCH)' GOARM='$(GOARM)' \
	CGO_ENABLED='$(CGO_ENABLED)' \
	$(GO) build -v \
		-gcflags '$(GO_GCFLAGS)' \
		-ldflags '$(GO_LDFLAGS)' \
		$(BUILD_MODE) $(RACE) \
		-o '$(BUILD_PATH)/$(OUTPUT_NAME)' \
		$(PKGDIR)
	# set -x && \
	# $(GO) vet -unsafeptr=false .
	chmod -R 777 $(BUILD_PATH)

vendor_darwin vendor_linux:

vendor_windows:
	#find $(shell go env GOPATH)/pkg/$(GOOS)_$(GOARCH) -name *.dll -exec cp -f {} $(BUILD_PATH) \;

vendor_android:
	cp $(CROSS_ROOT)/sysroot/usr/lib/$(CROSS_TRIPLE)/libc++_shared.so $(BUILD_PATH)
	chmod +rx $(BUILD_PATH)/libc++_shared.so
	# cp $(CROSS_ROOT)/$(CROSS_TRIPLE)/lib/libgnustl_shared.so $(BUILD_PATH)
	# chmod +rx $(BUILD_PATH)/libgnustl_shared.so

vendor_libs_windows:

vendor_libs_android:
	$(CROSS_ROOT)/sysroot/usr/lib/$(CROSS_TRIPLE)/libc++_shared.so
	# $(CROSS_ROOT)/$(CROSS_TRIPLE)/lib/libgnustl_shared.so

elementum: $(BUILD_PATH)/$(OUTPUT_NAME)

re: clean build

clean:
	rm -rf $(BUILD_PATH)

distclean:
	rm -rf build

prepare: force
	$(DOCKER) run --rm -v $(GOPATH):/go -e GOPATH=/go -v $(shell pwd):/go/src/$(GO_PKG) --ulimit memlock=67108864 -w /go/src/$(GO_PKG) $(DOCKER_IMAGE):$(TARGET_OS)-$(TARGET_ARCH) $(GO) get -u -x ./...

prepare_windows:
	# $(GO) get -u -x github.com/yusufpapurcu/wmi

build: force
ifeq ($(TARGET_OS), windows)
	# GOOS=windows $(GO) get -u -x github.com/yusufpapurcu/wmi
endif
	$(DOCKER) run --rm -v $(GOPATH):/go -e GOPATH=/go -e GOCACHE=/go-cache -v $(shell pwd):/go/src/$(GO_PKG) -v $(shell go env GOCACHE):/go-cache -u `stat -c "%u:%g" $(shell go env GOCACHE)` --ulimit memlock=67108864 -w /go/src/$(GO_PKG) $(DOCKER_IMAGE):$(TARGET_OS)-$(TARGET_ARCH) make dist TARGET_OS=$(TARGET_OS) TARGET_ARCH=$(TARGET_ARCH) TARGET_SHARED=$(TARGET_SHARED) GIT_VERSION=$(GIT_VERSION) IS_RACE=$(IS_RACE)

docker: force
	$(DOCKER) run --rm -v $(GOPATH):/go -e GOPATH=/go -e GOCACHE=/go-cache -v $(shell pwd):/go/src/$(GO_PKG) -v $(shell go env GOCACHE):/go-cache -u `stat -c "%u:%g" $(shell go env GOCACHE)` --ulimit memlock=67108864 -w /go/src/$(GO_PKG) $(DOCKER_IMAGE):$(TARGET_OS)-$(TARGET_ARCH)

strip: force
	# Temporary disable strip
	# @find $(BUILD_PATH) -type f ! -name "*.exe" -exec $(STRIP) {} \;

upx-all:
	@find build/ -type f ! -name "*.exe" -a ! -name "*.so" -exec $(UPX) --lzma {} \;

upx: force
# Do not .exe files, as upx doesn't really work with 8l/6l linked files.
# It's fine for other platforms, because we link with an external linker, namely
# GCC or Clang. However, on Windows this feature is not yet supported.
	@find $(BUILD_PATH) -type f ! -name "*.exe" -a ! -name "*.so" -exec $(UPX) --lzma {} \;

checksum: $(BUILD_PATH)/$(OUTPUT_NAME)
	shasum -b $(BUILD_PATH)/$(OUTPUT_NAME) | cut -d' ' -f1 >> $(BUILD_PATH)/$(OUTPUT_NAME)

ifeq ($(TARGET_ARCH), arm)
dist: elementum vendor_$(TARGET_OS) strip checksum
else ifeq ($(TARGET_ARCH), armv6)
dist: elementum vendor_$(TARGET_OS) strip checksum
else ifeq ($(TARGET_ARCH), armv7)
dist: elementum vendor_$(TARGET_OS) strip checksum
else ifeq ($(TARGET_ARCH), arm64)
dist: elementum vendor_$(TARGET_OS) strip checksum
else ifeq ($(TARGET_OS), darwin)
dist: elementum vendor_$(TARGET_OS) strip checksum
else
dist: elementum vendor_$(TARGET_OS) strip checksum
endif

libs: force
	$(MAKE) libtorrent-go PLATFORM=$(PLATFORM)

binaries:
	git config --global push.default simple
	git config --global http.postBuffer 524288000
	git clone --depth=1 https://${GH_USER}:${GH_TOKEN}@github.com/elgatito/elementum-binaries binaries
	cp -Rf build/* binaries/
	cd binaries && \
	git add * && \
	git commit -m "Update to ${GIT_VERSION}" && \
	git tag --force ${GIT_VERSION} && \
	git push origin master --tags

pull-all:
	for i in $(PLATFORMS); do \
		docker pull $(DOCKER_IMAGE):$$i; \
	done

pull:
	docker pull $(DOCKER_IMAGE):$(PLATFORM)

prepare-all:
	for i in $(PLATFORMS); do \
		$(MAKE) prepare PLATFORM=$(PLATFORM); \
	done

zip:
	cd build && \
	arch=$$(echo $(PLATFORM) | sed s/-/_/g) && \
	cd $${arch} && zip -9 -r ../$(NAME).$(GIT_VERSION).$${arch}.zip .