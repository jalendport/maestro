#!/bin/sh

set -eu

repository="jalendport/maestro"

case "$(uname -s)" in
	Darwin)
		os="darwin"
		;;
	Linux)
		os="linux"
		;;
	*)
		echo "Unsupported operating system: $(uname -s)" >&2
		exit 1
		;;
esac

case "$(uname -m)" in
	x86_64 | amd64)
		arch="amd64"
		;;
	arm64 | aarch64)
		arch="arm64"
		;;
	*)
		echo "Unsupported architecture: $(uname -m)" >&2
		exit 1
		;;
esac

if ! command -v curl >/dev/null 2>&1; then
	echo "curl is required to install maestro." >&2
	exit 1
fi

if ! command -v tar >/dev/null 2>&1; then
	echo "tar is required to install maestro." >&2
	exit 1
fi

if command -v shasum >/dev/null 2>&1; then
	checksum_command="shasum -a 256"
elif command -v sha256sum >/dev/null 2>&1; then
	checksum_command="sha256sum"
else
	echo "shasum or sha256sum is required to verify the download." >&2
	exit 1
fi

archive="maestro_${os}_${arch}.tar.gz"
release_url="https://github.com/${repository}/releases/latest/download"
temporary_directory="$(mktemp -d)"
trap 'rm -rf "$temporary_directory"' EXIT HUP INT TERM

echo "Downloading ${archive} from the latest maestro release..."
curl -fsSL "${release_url}/${archive}" -o "$temporary_directory/$archive"
curl -fsSL "${release_url}/checksums.txt" -o "$temporary_directory/checksums.txt"

expected="$(grep " ${archive}\$" "$temporary_directory/checksums.txt" | cut -d ' ' -f 1)"
actual="$($checksum_command "$temporary_directory/$archive" | cut -d ' ' -f 1)"
if [ -z "$expected" ] || [ "$expected" != "$actual" ]; then
	echo "Checksum mismatch for ${archive}; nothing was installed." >&2
	exit 1
fi
echo "Verified the checksum of ${archive}."

tar -xzf "$temporary_directory/$archive" -C "$temporary_directory"

if { [ -d /usr/local/bin ] && [ -w /usr/local/bin ]; } || { [ ! -e /usr/local/bin ] && [ -w /usr/local ]; }; then
	install_directory="/usr/local/bin"
	mkdir -p "$install_directory"
else
	install_directory="${HOME}/.local/bin"
	mkdir -p "$install_directory"
fi

cp "$temporary_directory/maestro" "$install_directory/maestro"
chmod 755 "$install_directory/maestro"

echo "Installed maestro to ${install_directory}/maestro."
if [ "$install_directory" = "${HOME}/.local/bin" ]; then
	case ":${PATH}:" in
		*":${install_directory}:"*) ;;
		*) echo 'Add this line to your shell profile: export PATH="$HOME/.local/bin:$PATH"' ;;
	esac
fi

echo "Next: maestro doctor, then maestro adapter install."
