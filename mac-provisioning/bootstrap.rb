directory File.expand_path("~/local")
directory File.expand_path("~/local/bin")

execute "git clone --depth=1 https://github.com/robbyrussell/oh-my-zsh.git ~/.oh-my-zsh" do
  not_if "test -d ~/.oh-my-zsh"
end

package "ghq"
package "zsh"
package "tmux"
package "reattach-to-user-namespace"
package "peco"
package "wget"
package "ag"
package "go"
package "hg"

cask_options = ['--appdir=/Applications', '--binarydir=/opt/brew/bin']

cask "dropbox"
cask "iterm2"
cask "google-japanese-ime"
cask "alfred"
cask "moom"
cask "appcleaner"
cask "night-owl"
cask "slack"
cask "virtualbox"
cask "vagrant"
cask "sourcetree" do
  options cask_options
end
cask "atom" do
  options cask_options
end

# https://github.com/splhack/macvim-kaoriya/releases/download/20151211/MacVim-KaoriYa-20151211.dmg
# cask "karabiner"
# cask "witch" # app store

