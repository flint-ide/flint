# AWS

AWS Profiles is an extension now, not part of Flint itself. It adds an AWS Profiles panel with the profiles in `~/.aws/config` and `~/.aws/credentials`, whether their credentials are still valid and for how long, and actions to refresh them, check who you are (`aws sts get-caller-identity`) and copy the profile name. **Log In** (`aws sso login`, `saml2aws login`) and **Open Terminal** as a profile open as terminal tabs, with `AWS_PROFILE` and `AWS_REGION` set.

## Install it

1. Open **Settings ▸ Extensions** and find the AWS Profiles extension under **Repository**.
2. Click **Install…**. Flint builds it with `go build` (Go must be installed) and shows its program and permissions. Click **Install**.

**Update…** appears there when the repository has a newer version. To install from a folder instead, build the `aws/` folder of [flint-ide/extensions](https://github.com/flint-ide/extensions) (`go build -o aws-ext .`) and run **Install Extension from Folder…**. See [Install an extension](extensions.md#install-an-extension).

The extension needs the AWS CLI (`brew install awscli`), and `saml2aws` if you use it. It gets `AWS_CONFIG_FILE` and the related variables of your environment because its manifest names them (`run.env`); the install dialog lists them.

## Settings

**Settings ▸ AWS Profiles** is the extension's own page. Its values are stored in `settings.json` under `extensions.flint.aws` (see [extensions](../reference/settings.md#extensions)). The `refresh.awsSeconds` key of the old built-in panel is ignored.

## Related

- [Manage AWS profiles](../how-to/manage-aws-profiles.md)
- [Kubernetes](kubernetes.md)
- [Extensions](extensions.md)
