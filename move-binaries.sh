#!/bin/bash

cd build/

if [ "$USER" = "travis" ]; then
	sudo chown -R travis:travis ../
fi
if [ "$USER" = "jenkins" ]; then
	sudo chown -R jenkins:jenkins ../
fi

for file in *; do
	if [ ! -d "$file" ]; then
		FILENAME=`echo $file | sed -e "s/.exe//"`
		OS=`echo $FILENAME | cut -d"-" -f 2`
		ARCH=`echo $FILENAME | rev | cut -d"-" -f1 | rev`
		EXT=""

		if [[ $file == *".exe"* ]]; then
			EXT=".exe"
		fi

		if [ "$ARCH" = "armv7" ]; then
			ARCH="arm-7"
		elif [ "$ARCH" = "amd64" ]; then
			ARCH="x64"
		elif [ "$ARCH" = "386" ]; then
			ARCH="x86"
		elif [ "$ARCH" = "6" ]; then
			ARCH="arm-6"
		elif [ "$ARCH" = "7" ]; then
			ARCH="arm-7"
		fi

		echo "Moving '$file' to ${OS}_${ARCH}/elementum$EXT \n"
		mkdir -p ${OS}_${ARCH}
		mv -f $file ${OS}_${ARCH}/elementum$EXT
	fi
done
