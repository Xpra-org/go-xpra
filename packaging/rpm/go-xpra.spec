%define _disable_source_fetch 0
%global debug_package %{nil}
%global desktop_id org.xpra.go-xpra
%{!?run_tests:%global run_tests 1}

Name:           go-xpra
Version:        0.2.1
Release:        1%{?dist}
Summary:        Minimal Xpra client written in Go

License:        GPL-3.0-only
URL:            https://github.com/Xpra-org/go-xpra
Source0:        %{url}/archive/refs/tags/v%{version}.tar.gz#/%{name}-%{version}.tar.gz

BuildRequires:  golang >= 1.25
BuildRequires:  desktop-file-utils
Recommends:     openssh-clients
Recommends:     pinentry
Suggests:       sshpass

%description
go-xpra is a minimal Xpra client. It connects to an Xpra server over TCP,
TLS, WebSocket, or SSH and displays forwarded application windows on the
local X11 or Wayland desktop.

%prep
sha256=`sha256sum %{SOURCE0} | awk '{print $1}'`
if [ "${sha256}" != "5beed38336dc1614b71f2613b7772d123bd6fd81ca5217fb8305433cffd3eab2" ]; then
	echo "invalid checksum for %{SOURCE0}"
	exit 1
fi
%autosetup

%build
export CGO_ENABLED=0
export GOFLAGS="-mod=readonly -buildvcs=false"
go build -trimpath -ldflags="-s -w" -o %{name} ./cmd/go-xpra

%install
install -Dpm 0755 %{name} %{buildroot}%{_bindir}/%{name}
install -Dpm 0644 packaging/%{desktop_id}.desktop \
    %{buildroot}%{_datadir}/applications/%{desktop_id}.desktop
install -Dpm 0644 xpra.png \
    %{buildroot}%{_datadir}/pixmaps/%{name}.png
install -Dpm 0644 packaging/%{name}.1 \
    %{buildroot}%{_mandir}/man1/%{name}.1

%check
desktop-file-validate %{buildroot}%{_datadir}/applications/%{desktop_id}.desktop
%if 0%{?run_tests}
go test ./...
%endif

%files
%license LICENSE
%doc README.md
%{_bindir}/%{name}
%{_datadir}/applications/%{desktop_id}.desktop
%{_datadir}/pixmaps/%{name}.png
%{_mandir}/man1/%{name}.1*

%changelog
* Thu Jul 30 2026 Antoine Martin <antoine@xpra.org> 0.2.1-1
- Platforms, build and packaging:
   bump the version
   share common Linux UI and keysym code
   add a native Wayland backend
   report when the Wayland compositor does not draw window frames
   add an executable icon on MS Windows
   add the Go-styled icon to the main page
   keep the initial MS Windows title bar accessible
   fix dialog icons on MS Windows
   remove duplicated asset functions
   add RPM and Debian packaging
   package the desktop launcher and icon
   add and package the manual page
- Network:
   add SSL support
   add the SSH subprocess transport
   use modern Xpra packet types
   gate legacy packets behind the compatibility toggle
   gate the draw acknowledgement layout by compatibility mode
   add WebSocket transports
   support out-of-band chunks
- Encodings:
   decode WebP using video-range YCbCr
- Features:
   add native authentication prompts
   add a connection dialog for argument-free startup
   add window icon support
   use the Xpra icon in dialogs
- Documentation and testing:
   add the mock server and its test guide
   complete the mock server connection support
   document mock server testing
