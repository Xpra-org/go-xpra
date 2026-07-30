#!/bin/bash

set -e

eval "$(dpkg-architecture -s)"

if [ -z "${REPO_ARCH_PATH}" ]; then
	REPO_ARCH_PATH="$(pwd)/../repo"
fi

changelog_version="$(dpkg-parsechangelog -l go-xpra/changelog -S Version)"
upstream_version="${changelog_version%-*}"
GO_XPRA_TAR_GZ="../pkgs/go-xpra-${upstream_version}.tar.gz"
if [ ! -f "${GO_XPRA_TAR_GZ}" ]; then
	echo "no go-xpra ${upstream_version} source found"
	exit 1
fi

source_dir="$(basename "${GO_XPRA_TAR_GZ}" .tar.gz)"
rm -rf -- "./${source_dir:?}"
tar -xzf "${GO_XPRA_TAR_GZ}"
pushd "./${source_dir}"
ln -s ../go-xpra debian

mk-build-deps \
	--install \
	--remove \
	--tool='apt-get --no-install-recommends --yes' \
	debian/control

deb_filename="go-xpra_${changelog_version}_${DEB_BUILD_ARCH}.deb"
mkdir -p "${REPO_ARCH_PATH}"
if find "${REPO_ARCH_PATH}" -name "${deb_filename}" -print -quit | grep -q .; then
	echo "${deb_filename} already exists"
else
	debuild --no-lintian -us -uc -b
	find .. -maxdepth 1 -type f \
		\( -name 'go-xpra_*.deb' -o -name 'go-xpra_*.changes' -o -name 'go-xpra_*.buildinfo' \) \
		-exec cp -p {} "${REPO_ARCH_PATH}/" \;
fi

popd
