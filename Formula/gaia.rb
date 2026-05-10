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
  version "0.3.0"

  on_macos do
    on_arm do
      url "https://github.com/stewartbrothers/gaia/releases/download/v0.3.0/gaia_v0.3.0_darwin_arm64.tar.gz"
      sha256 "bd5a1952330e62009cc6c3e7f629dfde6ea542ff7aaa5254dab95a467f962ad8"
    end
    on_intel do
      url "https://github.com/stewartbrothers/gaia/releases/download/v0.3.0/gaia_v0.3.0_darwin_x86_64.tar.gz"
      sha256 "04b50fdb28c27a6d0cee840bc3f96283acdcd4deb47ba08dbdf5b2b1a4e1cc9c"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/stewartbrothers/gaia/releases/download/v0.3.0/gaia_v0.3.0_linux_arm64.tar.gz"
      sha256 "38c2c168a0f0aaef0735fa622df56ac6e5133187681703b9772963ccb41d559d"
    end
    on_intel do
      url "https://github.com/stewartbrothers/gaia/releases/download/v0.3.0/gaia_v0.3.0_linux_x86_64.tar.gz"
      sha256 "e48807a26902c0234018b6a33f0303d1f29b413a6d5647abeee86356c6df6cb9"
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
