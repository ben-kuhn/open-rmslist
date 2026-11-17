# RMS Radar RPM spec
#
# Build:
#   spectool -g packaging/rpm/rms-radar.spec   # fetch Source0 tarball
#   rpmbuild -ba packaging/rpm/rms-radar.spec
#
# Note: point-to-point VOACAP forecasts are optional. The "voacapl" engine is
# not packaged in Fedora/EPEL proper (see the Recommends comment below), so
# RMS Radar falls back to its built-in real-time heuristic until a COPR or
# local build provides it.

Name:           rms-radar
Version:        0.2.0
Release:        1%{?dist}
Summary:        Winlink RMS gateway map with service-code filtering and propagation ranking

License:        MIT
URL:            https://github.com/ben-kuhn/rms-radar
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
rms-radar at a remote VOACAP API with -voacap-url.

%prep
%autosetup

%build
# Static, portable binary (no cgo, no external Go dependencies).
export CGO_ENABLED=0
go build -trimpath -ldflags "-s -w -X main.version=%{version}" -o rms-radar .

%install
install -Dm755 rms-radar %{buildroot}%{_bindir}/rms-radar
install -Dm644 contrib/systemd/rms-radar.service %{buildroot}%{_unitdir}/rms-radar.service
install -Dm644 contrib/desktop/rms-radar.desktop %{buildroot}%{_datadir}/applications/rms-radar.desktop

%files
%{_bindir}/rms-radar
%{_unitdir}/rms-radar.service
%{_datadir}/applications/rms-radar.desktop
%license LICENSE
%doc README.md

%post
%systemd_post rms-radar.service

%preun
%systemd_preun rms-radar.service

%postun
%systemd_postun rms-radar.service

%changelog
* Tue Nov 17 2026 RMS Radar contributors - 0.2.0-1
- Rename to RMS Radar; native desktop build; gateway setup guide
* Mon Nov 16 2026 RMS Radar contributors - 0.1.0-1
- Initial package
