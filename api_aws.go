package main

import (
	"time"

	"flint/internal/awsprof"
	"flint/internal/shellenv"
)

// AWSAPI lists the AWS CLI profiles with the state of their credentials.
// Logging in and terminals with a profile run as ordinary terminals.
type AWSAPI struct {
	app *App
}

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
