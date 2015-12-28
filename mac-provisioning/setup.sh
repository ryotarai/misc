#!/bin/bash
set -ex

brew tap caskroom/cask
which bundle || sudo gem i bundler
bundle install
bundle exec itamae local --log-level debug bootstrap.rb
