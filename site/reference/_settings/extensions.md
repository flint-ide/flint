| `extensions` | object | `{}` | Which installed extensions are on, whether their agent tools are offered, and each extension's own settings. See [extensions](#extensions). |

### extensions

Used by [extensions](../guides/extensions.md). Settings ▸ Extensions and each extension's own Settings page write it.

```json
"extensions": {
  "enabled": { "acme.jira": false },
  "agentTools": { "acme.jira": false },
  "acme.jira": { "site": "acme.atlassian.net", "pollMinutes": 10 }
}
```

| Key | Type | Default | Meaning |
|---|---|---|---|
| `enabled` | object of booleans | `{}` | `false` turns an installed extension off: its process stops and its commands, panels and status items disappear. Missing means on. |
| `agentTools` | object of booleans | `{}` | `false` keeps an extension's tools away from agents on Flint's MCP server. Missing means offered. |
| `<extension id>` | object | `{}` | The extension's settings, described by its manifest. Values that don't fit the manifest are replaced with the manifest's defaults before the extension sees them. |

The objects of extensions that are not installed are kept, so uninstalling and installing again restores their settings. Secrets never go here: extensions keep them in the system keychain.
