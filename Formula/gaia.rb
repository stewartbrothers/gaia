# Homebrew formula for gaia.
#
# This file is **auto-updated by goreleaser** on every tagged release
# (see the `brews:` block in .goreleaser.yml). The values below are
# the initial scaffold targeting v0.1.0 — once a release lands, the
# release workflow will rewrite the `url` and `sha256` fields to
# point at the new archive. Manual edits are fine for the
# auto-managed fields will be clobbered on the next release.
#
# Tap usage:
#
#   brew tap Gerwood/gaia https://github.com/stewartbrothers/gaia
#   brew install gaia
#
# The tap URL form is required because Homebrew defaults to
# github.com/<owner>/homebrew-<name> — the `https://...` argument
# overrides that for non-GitHub remotes.
class Gaia < Formula
  desc "Token-trimmed CLI + MCP server for Forgejo and GitHub"
  homepage "https://github.com/stewartbrothers/gaia"
  license "Apache-2.0"
  version "0.1.0"

  on_macos do
    on_arm do
      url "https://github.com/stewartbrothers/gaia/releases/download/v0.1.0/gaia_v0.1.0_darwin_arm64.tar.gz"
      sha256 "0000000000000000000000000000000000000000000000000000000000000000"
    end
    on_intel do
      url "https://github.com/stewartbrothers/gaia/releases/download/v0.1.0/gaia_v0.1.0_darwin_x86_64.tar.gz"
      sha256 "0000000000000000000000000000000000000000000000000000000000000000"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/stewartbrothers/gaia/releases/download/v0.1.0/gaia_v0.1.0_linux_arm64.tar.gz"
      sha256 "0000000000000000000000000000000000000000000000000000000000000000"
    end
    on_intel do
      url "https://github.com/stewartbrothers/gaia/releases/download/v0.1.0/gaia_v0.1.0_linux_x86_64.tar.gz"
      sha256 "0000000000000000000000000000000000000000000000000000000000000000"
    end
  end

  def install
    bin.install "gaia"
    bin.install "gaia-mcp"
  end

  test do
    # `gaia version` always reports a version triple; if the binary
    # can't even produce that, the install is broken.
    assert_match(/v\d+\.\d+\.\d+/, shell_output("#{bin}/gaia version --format pretty"))
  end
end
