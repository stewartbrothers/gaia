# Homebrew formula for gaia.
#
# This file is **auto-updated by goreleaser** on every tagged release
# (see the `brews:` block in .goreleaser.yml). Manual edits are fine;
# auto-managed fields (url/sha256/version) will be clobbered on the
# next release.
#
# Tap usage:
#
#   brew tap stewartbrothers/gaia https://github.com/stewartbrothers/gaia
#   brew install gaia
#
# The tap URL form is required because Homebrew defaults to
# github.com/<owner>/homebrew-<name> — the `https://...` argument
# overrides that for non-GitHub remotes.
class Gaia < Formula
  desc "Token-trimmed CLI + MCP server for Forgejo and GitHub"
  homepage "https://github.com/stewartbrothers/gaia"
  license "Apache-2.0"
  version "0.2.8"

  on_macos do
    on_arm do
      url "https://github.com/stewartbrothers/gaia/releases/download/v0.2.8/gaia_v0.2.8_darwin_arm64.tar.gz"
      sha256 "41e8d3ea837c807710342fa46f226b6f90ce307fdc54b1931378da116df163b7"
    end
    on_intel do
      url "https://github.com/stewartbrothers/gaia/releases/download/v0.2.8/gaia_v0.2.8_darwin_x86_64.tar.gz"
      sha256 "6a8e8e17143750c966bd3a891866e284f78c3013a34c4145fa2a7fc6a6d0fdd5"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/stewartbrothers/gaia/releases/download/v0.2.8/gaia_v0.2.8_linux_arm64.tar.gz"
      sha256 "dd9bfb816a3579d27a5b67fdbdffc13888a0a9d03b8ccf6bd16f1bcdff10f232"
    end
    on_intel do
      url "https://github.com/stewartbrothers/gaia/releases/download/v0.2.8/gaia_v0.2.8_linux_x86_64.tar.gz"
      sha256 "2bd16c8270f652a69d07b428107ad7e1e82f1e1268efa671bdedf3aaf5710833"
    end
  end

  def install
    bin.install "gaia"
    bin.install "gaia-mcp"
  end

  test do
    assert_match(/v\d+\.\d+\.\d+/, shell_output("#{bin}/gaia version --format pretty"))
  end
end
