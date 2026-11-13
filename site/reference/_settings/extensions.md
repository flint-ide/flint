| `extensions` | object | `{}` | Which installed extensions are on, whether their agent tools are offered, the extension index to use, and each extension's own settings. See [extensions](#extensions). |

### extensions

Used by [extensions](../guides/extensions.md). Settings ▸ Extensions and each extension's own Settings page write it.

```json
"extensions": {
  "enabled": { "acme.jira": false },
  "agentTools": { "acme.jira": false },
  "decorations": { "acme.jira.refs": false },
  "indexUrl": "https://extensions.example.com/index.json",
  "indexPublicKey": "3q2+7w…=",
  "acme.jira": { "site": "acme.atlassian.net", "pollMinutes": 10 }
}
```

| Key | Type | Default | Meaning |
|---|---|---|---|
| `enabled` | object of booleans | `{}` | `false` turns an installed extension off: its process stops and its commands, panels and status items disappear. Missing means on. |
| `agentTools` | object of booleans | `{}` | `false` keeps an extension's tools away from agents on Flint's MCP server. Missing means offered. |
| `decorations` | object of booleans | `{}` | Turns an extension's [marks in the editor](../guides/extensions.md#marks-in-the-editor) on or off, by decoration set id. Missing means the default the extension declares (on, unless it says otherwise). |
| `indexUrl` | string | `""` | Address of the signed [extension index](../extensions/index-format.md) Flint downloads when you click **Browse Extensions** or **Check for Updates**. Must be `https` (`http` only for `localhost`). Empty: `https://github.com/flint-ide/extensions/releases/latest/download/index.json`. |
| `indexPublicKey` | string | `""` | The Ed25519 public key (base64, 44 characters) the index must be signed with. Empty: the key built into Flint. Set it together with `indexUrl` to use an index of your own. |
| `<extension id>` | object | `{}` | The extension's settings, described by its manifest. Values that don't fit the manifest are replaced with the manifest's defaults before the extension sees them. |

`indexUrl` and `indexPublicKey` decide what Flint is willing to download and install from, so they are read from your own `settings.json` only; nothing in a project folder can set them. Flint never downloads the index on its own.

The objects of extensions that are not installed are kept, so uninstalling and installing again restores their settings. Secrets never go here: extensions keep them in the system keychain.
