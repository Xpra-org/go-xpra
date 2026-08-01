#!/bin/bash

set -e

# unpacked when the distribution's Go is older than go.mod asks for,
# which is not the case on Debian sid or Ubuntu stonking
GO_VERSION="1.26.5"

eval "$(dpkg-architecture -s)"

if [ -z "${REPO_ARCH_PATH}" ]; then
	REPO_ARCH_PATH="$(pwd)/../repo"
fi

# "<go arch> <sha256 of go${GO_VERSION}.linux-<go arch>.tar.gz>", by Debian architecture
go_tarball_for() {
	case "$1" in
	amd64)       echo "amd64 5c2c3b16caefa1d968a94c1daca04a7ca301a496d9b086e17ad77bb81393f053" ;;
	arm64)       echo "arm64 fe4789e92b1f33358680864bbe8704289e7bb5fc207d80623c308935bd696d49" ;;
	armel|armhf) echo "armv6l 6dae9edab81c13bccf962dec15f1fd2ec26c14a6821b4d2c92dab4130c289d7a" ;;
	i386)        echo "386 88c162b204e6eefcc32499453b492e80209f4a4c78c33092636901c540fb0d05" ;;
	loong64)     echo "loong64 82736bb7794547a73372ddfeb1b370cfea4d17f04782a65e82a389a01ff0f7aa" ;;
	ppc64el)     echo "ppc64le c5d60e2b303bb612f20cd82786594b64874e73b35134025e27d3390bf284ae43" ;;
	riscv64)     echo "riscv64 d4a24dd4484d3f86b99c2d300af0dea5d184557e6d61eb7aba19ff61662750e3" ;;
	s390x)       echo "s390x 09ce3c504c0323968b75a717244dca4f25cd4cf0443e5ff6bc0bfa74add89fa7" ;;
	esac
}

fetch() {
	if command -v curl >/dev/null; then
		curl -fsSL -o "$2" "$1"
	elif command -v wget >/dev/null; then
		wget -q -O "$2" "$1"
	else
		echo "neither curl nor wget is available to download $1"
		exit 1
	fi
}

installed_go_version() {
	local version
	command -v go >/dev/null || return 0
	version="$(go version | cut -d' ' -f3)"
	version="${version#go}"
	# drop any distribution suffix, dpkg cannot compare "1.26.5-X:nodwarf5"
	echo "${version%%[!0-9.]*}"
}

# the Go apt would install, found out without installing it:
# golang-1.18 and friends are hundreds of megabytes
candidate_go_version() {
	local version
	version="$(apt-cache policy golang-go 2>/dev/null | awk '/Candidate:/ { print $2 }')"
	[ -n "${version}" ] && [ "${version}" != "(none)" ] || return 0
	# "2:1.18~0ubuntu2" is an epoch, an upstream version and a distribution suffix
	version="${version#*:}"
	echo "${version%%[!0-9.]*}"
}

# stop apt from installing a Go the build is not going to use
drop_golang_build_dep() {
	if ! grep -Eq '^[[:space:]]*golang-go,?$' debian/control; then
		echo "no golang-go build dependency found in debian/control"
		exit 1
	fi
	sed -Ei '/^[[:space:]]*golang-go,?$/d' debian/control
}

# put a Go at least as new as go.mod asks for first on the PATH,
# unpacking the upstream release when the distribution's is too old.
# This runs before the build dependencies are installed so that apt is
# never asked for a Go the build would then ignore.
setup_go() {
	local minimum version tarball_info arch sha256 tarball go_root
	minimum="$(awk '$1 == "go" { print $2; exit }' go.mod)"

	version="$(installed_go_version)"
	if [ -n "${version}" ] && dpkg --compare-versions "${version}" ge "${minimum}"; then
		echo "building with the installed Go ${version}, go.mod asks for ${minimum}"
		# debuild replaces the PATH with a sanitised one of its own
		DEBUILD_ARGS=("--prepend-path=$(dirname "$(command -v go)")")
		drop_golang_build_dep
		return
	fi

	# apt does not upgrade an installed golang-go to satisfy an unversioned
	# build dependency, so what apt offers only matters when no Go is installed
	if [ -z "${version}" ]; then
		version="$(candidate_go_version)"
		if [ -n "${version}" ] && dpkg --compare-versions "${version}" ge "${minimum}"; then
			echo "building with the distribution's Go ${version}, go.mod asks for ${minimum}"
			return
		fi
	fi
	if [ -z "${version}" ]; then
		echo "no Go is available, go.mod asks for ${minimum}: unpacking ${GO_VERSION}"
	else
		echo "Go ${version} is older than the ${minimum} go.mod asks for: unpacking ${GO_VERSION}"
	fi
	drop_golang_build_dep

	tarball_info="$(go_tarball_for "${DEB_BUILD_ARCH}")"
	if [ -z "${tarball_info}" ]; then
		echo "no Go ${GO_VERSION} tarball is known for ${DEB_BUILD_ARCH}"
		exit 1
	fi
	read -r arch sha256 <<<"${tarball_info}"

	tarball="${GO_TOOLCHAIN_PATH}/go${GO_VERSION}.linux-${arch}.tar.gz"
	if [ ! -f "${tarball}" ]; then
		if ! fetch "https://go.dev/dl/$(basename "${tarball}")" "${tarball}.part"; then
			rm -f -- "${tarball}.part"
			df -h "${GO_TOOLCHAIN_PATH}"
			echo "failed to download go${GO_VERSION} to ${GO_TOOLCHAIN_PATH}"
			exit 1
		fi
		mv "${tarball}.part" "${tarball}"
	fi
	echo "${sha256}  ${tarball}" | sha256sum --check --strict -

	go_root="${GO_TOOLCHAIN_PATH}/go-${GO_VERSION}"
	if [ ! -x "${go_root}/bin/go" ]; then
		rm -rf -- "${go_root:?}"
		mkdir -p "${go_root}"
		tar -xzf "${tarball}" -C "${go_root}" --strip-components=1
	fi
	export PATH="${go_root}/bin:${PATH}"
	# debuild replaces the PATH with a sanitised one of its own
	DEBUILD_ARGS=("--prepend-path=${go_root}/bin")
}

changelog_version="$(dpkg-parsechangelog -l go-xpra/changelog -S Version)"
upstream_version="${changelog_version%-*}"
GO_XPRA_TAR_GZ="../pkgs/go-xpra-${upstream_version}.tar.gz"
if [ ! -f "${GO_XPRA_TAR_GZ}" ]; then
	echo "no go-xpra ${upstream_version} source found"
	exit 1
fi

if [ -z "${GO_TOOLCHAIN_PATH}" ]; then
	GO_TOOLCHAIN_PATH="$(cd "$(dirname "${GO_XPRA_TAR_GZ}")" && pwd)"
fi

source_dir="$(basename "${GO_XPRA_TAR_GZ}" .tar.gz)"
rm -rf -- "./${source_dir:?}"
tar -xzf "${GO_XPRA_TAR_GZ}"
pushd "./${source_dir}"
cp -a ../go-xpra debian

deb_filename="go-xpra_${changelog_version}_${DEB_BUILD_ARCH}.deb"
mkdir -p "${REPO_ARCH_PATH}"
if find "${REPO_ARCH_PATH}" -name "${deb_filename}" -print -quit | grep -q .; then
	echo "${deb_filename} already exists"
else
	DEBUILD_ARGS=()
	setup_go
	mk-build-deps \
		--install \
		--remove \
		--tool='apt-get -o Debug::pkgProblemResolver=yes --no-install-recommends --yes' \
		debian/control
	debuild "${DEBUILD_ARGS[@]}" --no-lintian -us -uc -b
	find .. -maxdepth 1 -type f \
		\( -name 'go-xpra_*.deb' -o -name 'go-xpra_*.changes' -o -name 'go-xpra_*.buildinfo' \) \
		-exec cp -p {} "${REPO_ARCH_PATH}/" \;
fi

popd
