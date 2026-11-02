| `features` | object | see below | Which built-in extensions (Database, Docker, Kubernetes) are switched off. See [features](#features). |

### features

Set in **Settings ▸ Extensions ▸ Built-in**. A built-in extension that is switched off isn't started: its panel, commands, Settings page, agent tool and backend are left out until Flint restarts with it on. Its own settings are kept.

| Key | Type | Range | Default | Meaning |
|---|---|---|---|---|
| `enabled` | object | `database`, `docker`, `kubernetes` | `{}` | Feature id → `false` to switch it off (`true` or a missing id means on). Ids of other features are ignored. Takes effect after a restart. |

```json
"features": { "enabled": { "kubernetes": false } }
```
