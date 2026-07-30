%global debug_package %{nil}
%{!?run_tests:%global run_tests 1}

Name:           go-xpra
Version:        0.2.1
Release:        1%{?dist}
Summary:        Minimal Xpra client written in Go

License:        GPL-3.0-only
URL:            https://github.com/Xpra-org/go-xpra
Source0:        %{url}/archive/refs/tags/v%{version}.tar.gz#/%{name}-%{version}.tar.gz

BuildRequires:  golang >= 1.26
Recommends:     openssh-clients
Recommends:     pinentry
Suggests:       sshpass

%description
go-xpra is a minimal Xpra client. It connects to an Xpra server over TCP,
TLS, WebSocket, or SSH and displays forwarded application windows on the
local X11 or Wayland desktop.

%prep
%autosetup

%build
export CGO_ENABLED=0
export GOFLAGS="-mod=readonly -buildvcs=false"
go build -trimpath -ldflags="-s -w" -o %{name} ./cmd/go-xpra

%install
install -Dpm 0755 %{name} %{buildroot}%{_bindir}/%{name}

%check
%if 0%{?run_tests}
go test ./...
%endif

%files
%license LICENSE
%doc README.md
%{_bindir}/%{name}

%changelog
* Thu Jul 30 2026 Antoine Martin <antoine@xpra.org> - 0.2.1-1
- Initial package
