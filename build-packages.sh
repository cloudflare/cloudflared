VERSION=$(git describe --tags --always --match "[0-9][0-9][0-9][0-9].*.*")
echo $VERSION

# Avoid depending on C code since we don't need it.
export CGO_ENABLED=0

# This controls the directory the built artifacts go into
export ARTIFACT_DIR=built_artifacts/
mkdir -p $ARTIFACT_DIR
windowsArchs=("amd64" "386")
export TARGET_OS=windows
for arch in ${windowsArchs[@]}; do
    export TARGET_ARCH=$arch
    make cloudflared-msi
    mv ./cloudflared.exe $ARTIFACT_DIR/cloudflared-windows-$arch.exe
    mv cloudflared-$VERSION-$arch.msi $ARTIFACT_DIR/cloudflared-windows-$arch.msi
done


linuxArchs=("386" "amd64" "arm" "armhf" "arm64")
export TARGET_OS=linux
for arch in ${linuxArchs[@]}; do
    unset TARGET_ARM
    export TARGET_ARCH=$arch
    
    ## Support for armhf builds 
    if [[ $arch == armhf ]] ; then
        export TARGET_ARCH=arm
        export TARGET_ARM=7 
    fi
    
    make cloudflared-deb
    mv cloudflared\_$VERSION\_$arch.deb $ARTIFACT_DIR/cloudflared-linux-$arch.deb

    # rpm packages invert the - and _ and use x86_64 instead of amd64.
    RPMVERSION=$(echo $VERSION|sed -r 's/-/_/g')
    RPMARCH=$arch
    if [ $arch == "amd64" ];then
        RPMARCH="x86_64"
    fi
    if [ $arch == "arm64" ]; then
        RPMARCH="aarch64"
    fi
    make cloudflared-rpm
    mv cloudflared-$RPMVERSION-1.$RPMARCH.rpm $ARTIFACT_DIR/cloudflared-linux-$RPMARCH.rpm

    # finally move the linux binary as well.
    mv ./cloudflared $ARTIFACT_DIR/cloudflared-linux-$arch
done
