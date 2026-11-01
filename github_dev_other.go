//go:build !server

package main

import "flint/internal/github"

// devGitHubEndpoints: only development server builds can talk to a fake
// GitHub (github_dev_server.go); the app always uses settings.GitHub.Host.
func devGitHubEndpoints() (github.Endpoints, bool, error) { return github.Endpoints{}, false, nil }

// devSecrets: the app always keeps tokens in the OS keyring.
func devSecrets() (secretStore, bool) { return nil, false }
