# Brine: unsandboxed Roblox Studio launcher for Wine with a built-in MCP bridge.
#
# Built from a source tarball that also carries the vendored Go modules
# (Source1), so the build needs no network access. packaging/rpm/build-rpm.sh
# creates both tarballs and runs rpmbuild (in a Fedora container if rpmbuild
# isn't installed). The binary is static, so the package is not tied to one
# Fedora release.

# Go binaries are built static and stripped by -ldflags below; there is no
# separate debuginfo to extract.
%global debug_package %{nil}

Name:           brine
Version:        0.1.1
Release:        1%{?dist}
Summary:        Unsandboxed Roblox Studio launcher for Wine with a built-in MCP bridge
License:        MIT
URL:            https://github.com/EggyEgged/brine
Source0:        %{url}/archive/v%{version}/%{name}-%{version}.tar.gz
Source1:        %{name}-%{version}-vendor.tar.xz

ExclusiveArch:  x86_64

BuildRequires:  golang >= 1.23
BuildRequires:  desktop-file-utils

# Needed to unpack the Wine build that `brine setup` can download.
Requires:       tar
Requires:       xz
# Studio renders through Vulkan (DXVK) and sign-in links go through xdg-open.
Recommends:     vulkan-loader
Recommends:     xdg-utils
Recommends:     desktop-file-utils
Recommends:     shared-mime-info
Recommends:     libnotify
# Brine can download its own Wine build; a system Wine is optional.
Suggests:       wine

%description
Brine installs, updates and runs Roblox Studio under Wine directly on your
system as your own user, with no Flatpak, Snap or AppImage sandbox. Its main
feature is that Studio's built-in MCP server works out of the box with Linux
MCP clients such as Claude Code and Claude Desktop (`brine mcp`).

It also registers the roblox-studio: and roblox-studio-auth: link handlers and
the place file types, keeps the previous Studio build for `brine rollback`, and
ships `brine doctor` to diagnose Wine, Vulkan, the prefix and MCP.

Run `brine setup` as your normal user after installing. Brine is not affiliated
with Roblox Corporation.

%prep
%autosetup -n %{name}-%{version}
tar -xJf %{SOURCE1}

%build
export CGO_ENABLED=0
export GOFLAGS="-mod=vendor -trimpath"
go build -ldflags "-s -w -X main.Version=%{version}" -o build/brine ./cmd/brine
mkdir -p build/completions
./build/brine completion bash > build/completions/brine.bash
./build/brine completion zsh  > build/completions/_brine
./build/brine completion fish > build/completions/brine.fish

%install
install -Dpm 0755 build/brine %{buildroot}%{_bindir}/brine
install -Dpm 0644 assets/brine.desktop %{buildroot}%{_datadir}/applications/brine.desktop
install -Dpm 0644 assets/brine.svg %{buildroot}%{_datadir}/icons/hicolor/scalable/apps/brine.svg
install -Dpm 0644 assets/brine-studio.xml %{buildroot}%{_datadir}/mime/packages/brine-studio.xml
install -Dpm 0644 build/completions/brine.bash %{buildroot}%{_datadir}/bash-completion/completions/brine
install -Dpm 0644 build/completions/_brine %{buildroot}%{_datadir}/zsh/site-functions/_brine
install -Dpm 0644 build/completions/brine.fish %{buildroot}%{_datadir}/fish/vendor_completions.d/brine.fish
install -Dpm 0644 packaging/brine.1 %{buildroot}%{_mandir}/man1/brine.1

%check
export HOME=%{_builddir}/test-home
mkdir -p "$HOME"
GOFLAGS="-mod=vendor" go test ./...
desktop-file-validate %{buildroot}%{_datadir}/applications/brine.desktop

# The mime, desktop and icon caches are refreshed by Fedora's file triggers
# (shared-mime-info, desktop-file-utils, gtk-update-icon-cache); no scriptlets.

%files
%license LICENSE
%doc README.md CHANGELOG.md SECURITY.md
%{_bindir}/brine
%{_datadir}/applications/brine.desktop
%{_datadir}/icons/hicolor/scalable/apps/brine.svg
%{_datadir}/mime/packages/brine-studio.xml
%{_datadir}/bash-completion/completions/brine
%{_datadir}/zsh/site-functions/_brine
%{_datadir}/fish/vendor_completions.d/brine.fish
%{_mandir}/man1/brine.1*

%changelog
* Sun Oct 25 2026 Brine contributors <186115781+EggyEgged@users.noreply.github.com> - 0.1.1-1
- Package for Fedora; includes the roblox-studio: link handling changes
