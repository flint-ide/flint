# Homebrew cask for Flint, rendered by the release workflow (.github/workflows/release.yml,
# job "homebrew") with the version and the zip's SHA-256 when a release is published, and
# pushed to the tap as Casks/flint.rb. Edit this template, not the copy in the tap.
cask "flint" do
  version "{{VERSION}}"
  sha256 "{{SHA256}}"

  url "https://github.com/ODudek/flint/releases/download/v#{version}/Flint-v#{version}-macos-universal.zip"
  name "Flint"
  desc "Fast, AI-first IDE built around coding agents"
  homepage "https://github.com/ODudek/flint"

  livecheck do
    url :url
    strategy :github_latest
  end

  depends_on macos: :monterey

  app "flint.app"

  zap trash: [
    "~/Library/Application Support/flint",
    "~/Library/Caches/com.wails.flint",
    "~/Library/HTTPStorages/com.wails.flint",
    "~/Library/Logs/Flint",
    "~/Library/Preferences/com.wails.flint.plist",
    "~/Library/Saved Application State/com.wails.flint.savedState",
    "~/Library/WebKit/com.wails.flint",
  ]
end
