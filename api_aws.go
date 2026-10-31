package main

import (
	"fmt"
	"time"

	"flint/internal/awsprof"
	"flint/internal/shellenv"
)

// AWSAPI lists the AWS CLI profiles with the state of their credentials.
// Logging in and terminals with a profile run as ordinary terminals.
type AWSAPI struct {
	app     *App
	refresh *awsprof.Refresher
}

func newAWSAPI(app *App) *AWSAPI { return &AWSAPI{app: app, refresh: awsprof.NewRefresher()} }

// Profiles reads ~/.aws/config and ~/.aws/credentials (never returning secrets).
func (a *AWSAPI) Profiles() ([]awsprof.Profile, error) {
	shellenv.Wait() // AWS_CONFIG_FILE may come from the login shell
	files, err := awsprof.DefaultFiles()
	if err != nil {
		return nil, err
	}
	return awsprof.Load(files, time.Now())
}

// Identity asks STS who a profile's credentials belong to (needs the network,
// and may refresh SSO role credentials).
func (a *AWSAPI) Identity(profile string) (awsprof.Identity, error) {
	shellenv.Wait()
	return awsprof.CallerIdentity(a.app.ctx, profile)
}

// Refresh renews a profile's credentials without a terminal (aws sso login,
// saml2aws without prompts, or an STS call for roles and credential_process) and returns the end of the tool's output.
func (a *AWSAPI) Refresh(profile string) (string, error) {
	profiles, err := a.Profiles()
	if err != nil {
		return "", err
	}
	for _, p := range profiles {
		if p.Name == profile {
			return a.refresh.Refresh(a.app.ctx, p)
		}
	}
	return "", fmt.Errorf("no AWS profile %q", profile)
}

// CancelRefresh stops a running Refresh of profile.
func (a *AWSAPI) CancelRefresh(profile string) { a.refresh.Cancel(profile) }
