class Mdfetch < Formula
  desc "CLI tool to convert web pages to clean markdown"
  homepage "https://www.npmjs.com/package/@sandgarden/mdfetch"
  url "https://registry.npmjs.org/@sandgarden/mdfetch/-/mdfetch-1.1.0.tgz"
  sha256 "5bdaf448c10addb675e0f97d1aec865d67c7dca459cdf062d49aba8613157727"
  license "MIT"

  depends_on "node"

  def install
    system "npm", "install", *std_npm_args
    bin.install_symlink libexec.glob("bin/*")
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/mdfetch --version")
  end
end
