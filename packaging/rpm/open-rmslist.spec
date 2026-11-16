# open-rmslist RPM spec
#
# Build:
#   spectool -g packaging/rpm/open-rmslist.spec   # fetch Source0 tarball
#   rpmbuild -ba packaging/rpm/open-rmslist.spec
#
# Note: point-to-point VOACAP forecasts are optional. The "voacapl" engine is
# not packaged in Fedora/EPEL proper (see the Recommends comment below), so
# open-rmslist falls back to its built-in real-time heuristic until a COPR or
# local build provides it.

Name:           open-rmslist
Version:        0.1.0
Release:        1%{?dist}
Summary:        Winlink RMS gateway map with service-code filtering and propagation ranking

License:        MIT
URL:            https://github.com/ben-kuhn/open-rmslist
Source0:        %{url}/archive/refs/tags/v%{version}.tar.gz#/%{name}-%{version}.tar.gz

# Static Go binary with no usable DWARF; disable the debuginfo subpackage to
# avoid an empty debugsourcefiles.list.
%global debug_package %{nil}

BuildRequires:  golang
BuildRequires:  systemd-rpm-macros

# Optional runtime engine for point-to-point HF propagation forecasts.
# voacapl is NOT packaged in Fedora/EPEL; this only resolves if a COPR or
# third-party repo provides it, and is skipped gracefully otherwise.
Recommends:     voacapl

%description
A minimal, self-hostable web service that periodically refreshes the Winlink
RMS gateway list, maps gateways on OpenStreetMap, and lets users filter and
rank stations by service code (e.g. MERCURY, VardeFM), band, and mode using
current propagation conditions.

Point-to-point VOACAP forecasts are optional. To enable them, install the
voacapl engine and its itshfbc data (not packaged in Fedora proper — use a
COPR or build from https://github.com/jawatson/voacapl), or point
open-rmslist at a remote VOACAP API with -voacap-url.

%prep
%autosetup

%build
# Static, portable binary (no cgo, no external Go dependencies).
export CGO_ENABLED=0
go build -trimpath -ldflags "-s -w -X main.version=%{version}" -o open-rmslist .

%install
install -Dm755 open-rmslist %{buildroot}%{_bindir}/open-rmslist
install -Dm644 contrib/systemd/open-rmslist.service %{buildroot}%{_unitdir}/open-rmslist.service

%files
%{_bindir}/open-rmslist
%{_unitdir}/open-rmslist.service
%license LICENSE
%doc README.md

%post
%systemd_post open-rmslist.service

%preun
%systemd_preun open-rmslist.service

%postun
%systemd_postun open-rmslist.service

%changelog
* Mon Nov 16 2026 open-rmslist contributors - 0.1.0-1
- Initial package
