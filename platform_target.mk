GCC_TARGET = $(CC)

ifneq ($(findstring darwin, $(GCC_TARGET)),)
    TARGET_OS = darwin
else ifneq ($(findstring mingw, $(GCC_TARGET)),)
    TARGET_OS = windows
else ifneq ($(findstring android, $(GCC_TARGET)),)
    TARGET_OS = android
else ifneq ($(findstring linux, $(GCC_TARGET)),)
    TARGET_OS = linux
endif

ifneq ($(findstring x86_64, $(GCC_TARGET)),)
    TARGET_ARCH = x64
else ifneq ($(findstring i386, $(GCC_TARGET)),)
    TARGET_ARCH = x86
else ifneq ($(findstring i486, $(GCC_TARGET)),)
    TARGET_ARCH = x86
else ifneq ($(findstring i586, $(GCC_TARGET)),)
    TARGET_ARCH = x86
else ifneq ($(findstring i686, $(GCC_TARGET)),)
    TARGET_ARCH = x86
else ifneq ($(findstring aarch64, $(GCC_TARGET)),)
    TARGET_ARCH = arm64
else ifneq ($(findstring armv7, $(GCC_TARGET)),)
    TARGET_ARCH = armv7
else ifneq ($(findstring arm, $(GCC_TARGET)),)
    ifeq ($(TARGET_OS), linux)
        TARGET_ARCH = armv6
    else 
        TARGET_ARCH = arm
    endif
endif
