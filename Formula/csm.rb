class Csm < Formula
  desc "TUI session manager for Claude Code + Codex"
  homepage "https://github.com/welcomra1n/session-manager-tui"
  url "https://github.com/welcomra1n/session-manager-tui/archive/refs/heads/master.tar.gz"
  version "0.1.0"
  license "MIT"

  depends_on "go" => :build

  def install
    system "go", "build", *std_go_args(ldflags: "-s -w"), "-o", bin/"csm", "."
  end

  test do
    assert_match "세션 불러오는", shell_output("#{bin}/csm --help 2>&1", 1)
  end
end
