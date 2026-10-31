# AWS

The AWS Profiles panel shows the profiles on your Mac, whether their credentials are still valid, and lets you refresh them or open a terminal that uses one.

Open it with the `☁` icon on the activity bar or "AWS Profiles" in the command palette.

## Profiles

Flint reads `~/.aws/config` and `~/.aws/credentials` (or the files named by `AWS_CONFIG_FILE` and `AWS_SHARED_CREDENTIALS_FILE`). For each profile it shows:

- the kind: SSO, session keys (from saml2aws and similar tools), static keys, role, or `credential_process`;
- the account, role and region;
- whether the credentials are valid, and the time left until they expire.

Validity comes from the SSO token cache in `~/.aws/sso/cache` and, for session keys, from `x_security_token_expires` or `aws_expiration`. The countdown is live. The list refreshes every 30 seconds and when you return to the Flint window.

## Actions

Right-click a profile for its actions. Refresh Credentials and Log In appear only for kinds of profile that support them.

| Action | What it does |
|---|---|
| Open Terminal | Opens a terminal with `AWS_PROFILE` and `AWS_REGION` set (also: double-click or `❯_`) |
| Refresh Credentials | Renews the credentials without a terminal (also: ↻) |
| Log In | Runs the interactive login in a terminal |
| Check Identity | Asks STS who you are (`aws sts get-caller-identity`) |
| Copy Profile Name | Copies the name |
| Copy export AWS_PROFILE | Copies `export AWS_PROFILE=…` for use elsewhere |

### Terminal as profile

The terminal tab is named `aws:<profile> · …`, and it keeps its profile after a restart.

### Log In

Runs `aws sso login` for SSO profiles or `saml2aws login --role …` for saml2aws profiles in a terminal, where you can answer any prompts.

### Refresh

Refresh does the same work without a terminal:

- SSO: `aws sso login`, which waits up to 5 minutes for you to approve in the browser;
- saml2aws: login with the stored password and role (`--skip-prompt --force`);
- roles and `credential_process`: an STS call that fetches and caches the credentials.

Refresh can't answer questions. If the tool asks for input, for example an MFA code, refresh stops immediately and offers a **Log In** button so you can finish in a terminal. You can cancel a running refresh with ■.

## Not supported

aws-azure-login is deprecated and not supported; its `azure_*` settings are ignored.

## How secrets are handled

Access keys and tokens are read only by Flint's native process, to check that they exist. They never reach the user interface or the log.

## EKS

The [Kubernetes](kubernetes.md#eks-and-aws-credentials) panel shows the AWS profile of EKS contexts and offers the same refresh and Log In buttons.

## Related

- [Manage AWS profiles](../how-to/manage-aws-profiles.md)
