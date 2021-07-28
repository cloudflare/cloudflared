export CGO_ENABLED=0
# This controls the directory the built artifacts go into
export ARTIFACT_DIR=built_artifacts/
mkdir -p $ARTIFACT_DIR
windowsArchs=("amd64" "386")
export TARGET_OS=windows
for arch in ${windowsArchs[@]}; do 
    export TARGET_ARCH=$arch
    make cloudflared-msi
done

mv *.msi $ARTIFACT_DIR

export FIPS=true
linuxArchs=("amd64" "386" "arm")
export TARGET_OS=linux
for arch in ${linuxArchs[@]}; do 
    export TARGET_ARCH=$arch
    make cloudflared-deb
    make cloudflared-rpm
done

mv *.deb $ARTIFACT_DIR
mv *.rpm $ARTIFACT_DIR
