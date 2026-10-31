# Manage AWS profiles

Recipes for checking and renewing AWS credentials. For how the panel works, see [AWS](../guides/aws.md).

## Check which profiles are valid

1. Open the AWS Profiles panel: click `☁` on the activity bar, or run "AWS Profiles" from the palette.
2. Read each profile's status and the countdown to expiry.

The list refreshes every 30 seconds and when you come back to Flint. Click Refresh in the panel header to update it now.

## Refresh credentials without a terminal

1. Click ↻ on the profile's row, or right-click it and choose **Refresh Credentials**.
2. For SSO profiles, approve the login in the browser window that opens. Flint waits up to 5 minutes.

To cancel a running refresh, click ■.

If the login tool needs input, such as an MFA code, refresh stops and shows **Log In**. Use the next recipe instead.

## Log in interactively

1. Right-click the profile and choose **Log In**.
2. Answer the prompts in the terminal that opens.

Flint runs `aws sso login` for SSO profiles and `saml2aws login --role …` for saml2aws profiles.

## Open a terminal as a profile

1. Double-click the profile, or click `❯_` on its row.

The terminal has `AWS_PROFILE` and `AWS_REGION` set and keeps them after a restart.

## Check which identity you are using

1. Right-click the profile and choose **Check Identity**.

Flint asks STS (`aws sts get-caller-identity`) and shows the result.

## Use a profile elsewhere

- **Copy Profile Name** copies the name.
- **Copy export AWS_PROFILE** copies `export AWS_PROFILE=<name>`, ready to paste into any shell.

## Refresh credentials for an EKS cluster

1. Open the Kubernetes panel (`☸`) and select the EKS context.
2. Next to the AWS profile shown for the context, click refresh, or **Log In** if the login has expired.
