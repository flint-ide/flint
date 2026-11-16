# The extension index

Flint can install prebuilt extensions from a **signed index**: one JSON document that lists extensions and, for each, an archive per platform. It is not a marketplace. There is one index, curated in one repository ([flint-ide/extensions](https://github.com/flint-ide/extensions) publishes it with each release), and Flint trusts it through one public key. A company can run its own index with its own key.

This page is the format, for people who publish an index or want to check what Flint does with one. For users, see [Extensions](../guides/extensions.md#from-the-extension-index).

## What Flint does with it

- Flint downloads the index only when you ask: **Browse Extensions** / **Check for Updates** in Settings ▸ Extensions, or the palette commands **Browse Extensions** and **Check for Extension Updates**. It never fetches it on its own or in the background.
- It makes two anonymous `GET` requests: the index (`extensions.indexUrl`) and its signature (the same address plus `.sig`). No cookies, no token, no identifier; the only header about Flint is `User-Agent: Flint/<version>`. Addresses must be `https` (`http` is accepted for `localhost` only). The index may be at most 4 MiB.
- The signature is verified against the public key before anything in the index is read or shown. An index that doesn't verify is not used.
- **Install…** or **Update to x.y.z…** downloads the archive for your platform, which must have exactly the size and SHA-256 the index gives (and at most 256 MiB), unpacks it into a temporary folder, checks the manifest with the same rules as any extension, and requires the manifest's `id` and `version` to equal the index entry's. Then you get the same confirmation as for **Install from Folder…**; for an update it starts with what changed (the version, and every permission and agent tool the new version adds).
- The verified index is kept in `extension-index/` in Flint's [configuration folder](../reference/files-and-data.md), so **Update to x.y.z…** stays visible after a restart without a request. It is verified again each time it is read.

## index.json

```json
{
  "format": 1,
  "generatedAt": "2026-03-01T12:00:00Z",
  "extensions": [
    {
      "id": "flint.stats",
      "name": "Project Stats",
      "description": "Counts files and lines per language.",
      "publisher": "Flint",
      "version": "0.3.0",
      "contract": 1,
      "kind": "program",
      "permissions": ["workspace.read"],
      "agentTools": [{ "name": "summary" }, { "name": "reset", "acts": true }],
      "archives": {
        "darwin-arm64": { "url": "https://github.com/flint-ide/extensions/releases/download/v12/flint.stats-0.3.0-darwin-arm64.tar.gz", "sha256": "…64 hex digits…", "size": 2411520 },
        "darwin-amd64": { "url": "…", "sha256": "…", "size": 2530304 },
        "linux-amd64": { "url": "…", "sha256": "…", "size": 2498560 },
        "linux-arm64": { "url": "…", "sha256": "…", "size": 2383872 }
      }
    },
    {
      "id": "flint.nord",
      "name": "Nord",
      "version": "1.0.0",
      "contract": 1,
      "kind": "themes",
      "permissions": [],
      "agentTools": [],
      "archives": { "any": { "url": "…/flint.nord-1.0.0-any.tar.gz", "sha256": "…", "size": 1730 } }
    }
  ]
}
```

| Key | Meaning |
|---|---|
| `format` | The format version, `1`. Flint refuses an index with another number and says to update Flint. |
| `generatedAt` | When the index was built (RFC 3339). Shown in Settings. |
| `extensions` | At most 2,000 entries, each id once. |
| `id`, `name`, `description`, `publisher`, `version`, `contract` | As in the extension's [manifest](protocol.md#manifest). Shown before anything is downloaded. An entry whose `contract` this Flint doesn't support is listed but can't be installed. |
| `kind` | `program` (it runs a program), `themes` (color themes only), `languages` (languages with their servers, debuggers and test runners only) or `data` (other data without a program). |
| `permissions`, `agentTools` | The manifest's permissions and the names of its agent tools (`acts` marks tools that change things), for display. The manifest inside the archive is what the confirmation shows and what counts. |
| `archives` | One archive per platform for a `program`: `darwin-arm64`, `darwin-amd64`, `linux-amd64`, `linux-arm64`. Exactly one, `any`, for the other kinds. An extension without an archive for your platform is listed but can't be installed. |
| `archives.*.url` | An absolute `https` address. Redirects are followed to `https` addresses only. |
| `archives.*.sha256` | The archive's SHA-256, 64 lower-case hex digits. |
| `archives.*.size` | The archive's size in bytes, at most 256 MiB. |

## Archives

An archive is a `.tar.gz` of the extension's folder, with `flint-extension.json` at the archive's root and the program already built (named as `run.command` says, with its executable bit). Flint accepts folders and regular files only. It refuses the whole archive when it holds an absolute path, a `..` element, a symbolic or hard link, a device or a FIFO, the same name twice, more than 10,000 entries, folders nested deeper than 32, or more than 512 MiB of content. Owners, times and mode bits other than "executable" are ignored.

## Signature

The signature is a file next to the index, `index.json.sig`: one line with the Ed25519 signature over the exact bytes of `index.json`, in standard base64. The format is Flint's own and uses only Ed25519 from Go's standard library; it is not minisign (which needs BLAKE2b).

- A **public key** is the 32-byte Ed25519 public key in base64 (44 characters). Flint has the official index's key built in; `extensions.indexPublicKey` sets another one.
- A **secret key** is the 64-byte Ed25519 private key in base64 (88 characters). It belongs in the publishing workflow's secret store and nowhere else.

The official key is not built into Flint yet. Until a maintainer generates the key pair and adds the public key, Settings ▸ Extensions says the official index is not configured and Flint requests nothing; an index of your own with `extensions.indexPublicKey` works.

## flint-extindex

`cmd/flint-extindex` in the Flint repository makes an index with the same code Flint reads it with:

```sh
# Once: a key pair. Keep the secret key in your CI's secrets.
go run ./cmd/flint-extindex keygen

# Pack built extensions and write dist/index.json.
# An extension with a program is named with the platform it was built for;
# one without is packed once, as "any".
go run ./cmd/flint-extindex build -out dist \
  -base-url https://extensions.example.com/v12/ \
  darwin-arm64=build/darwin-arm64/stats linux-amd64=build/linux-amd64/stats themes/nord

# Sign it: writes dist/index.json.sig. The secret key is read from the
# environment (FLINT_EXTINDEX_KEY, or -key-env NAME) or from -key-file,
# never from the command line. Without a key nothing is written.
FLINT_EXTINDEX_KEY=… go run ./cmd/flint-extindex sign dist/index.json

# Check the result as Flint would, and the archives against the index.
go run ./cmd/flint-extindex verify -key <public key> -archives dist dist/index.json
```

`build` checks each manifest with Flint's rules, requires the program of each platform to exist and be executable, and refuses builds of one extension that differ in what the index shows. An input can also be a `.tar.gz` made earlier. Archives are reproducible: the same folder always packs to the same bytes. Then serve the files of `dist` at the base address.

## Your own index

1. Build and sign an index as above and serve `index.json`, `index.json.sig` and the archives over `https`.
2. In **Settings ▸ Extensions ▸ Extension index**, set **Index address** to your `index.json` and **Index public key** to your public key (`extensions.indexUrl` and `extensions.indexPublicKey` in [settings.json](../reference/settings.md#extensions)).

Both are your own settings in Flint's `settings.json`. A project or repository can't set them.
