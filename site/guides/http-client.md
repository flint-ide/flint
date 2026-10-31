# HTTP client

Flint has a built-in HTTP client for calling your own APIs. Requests live in `.http` or `.rest` files, in the same format IntelliJ uses, so they can be committed and shared.

## File format

A file holds one or more requests, separated by `###`. Text after `###` names the request.

```http
@pageSize = 20

### List users
GET {{baseUrl}}/users
    ?limit={{pageSize}}
    &sort=name
Accept: application/json

### Create user
# @name createUser
POST {{baseUrl}}/users
Content-Type: application/json
Authorization: Bearer {{token}}

{
  "id": "{{$uuid}}",
  "name": "Ada"
}

### Upload from a file
POST {{baseUrl}}/import
Content-Type: application/json

< ./fixtures/import.json
```

`{{baseUrl}}` and `{{token}}` come from the selected [environment](#environments); `{{pageSize}}` is defined in the file.

### Request line

`METHOD URL [HTTP/1.1]`. Without a method, the request is a GET; without a scheme, the URL uses `http://`. A long URL can continue on indented lines that start with `?` or `&`.

### Headers and body

Headers follow the request line. After an empty line comes the body, either inline or from a file:

- `< ./body.json` sends the file as is (the path is relative to the `.http` file);
- `<@ ./body.json` also substitutes variables inside it.

Body files must be inside the open project.

### Comments and directives

Lines starting with `#` or `//` are comments. These directives change how a request is sent:

| Directive | Effect |
|---|---|
| `# @name createUser` | Names the request |
| `# @no-redirect` | Doesn't follow redirects |
| `# @no-cookie-jar` | Doesn't use the file's cookie jar |
| `# @timeout 60` | Timeout in seconds, or a duration such as `2m` |

## Variables

Write `{{name}}` anywhere in a request. Values come from:

1. `@name = value` lines in the file. They apply to the requests below them and take precedence over environments.
2. The selected environment (see below).
3. Dynamic variables:

| Variable | Value |
|---|---|
| `{{$uuid}}` | A random UUID |
| `{{$timestamp}}` | The current Unix timestamp |
| `{{$isoTimestamp}}` | The current UTC time in ISO 8601 (RFC 3339) |
| `{{$randomInt}}` | A random integer from 0 to 1000 |

## Environments

Put environments in `http-client.env.json` next to the `.http` file. A `$shared` section applies to every environment.

```json
{
  "$shared": { "apiVersion": "v2" },
  "dev": { "baseUrl": "http://localhost:8080" },
  "staging": { "baseUrl": "https://staging.example.com" }
}
```

Keep secrets in `http-client.private.env.json`, with the same structure. Its values override the public file.

```json
{
  "dev": { "token": "dev-token-here" },
  "staging": { "token": "staging-token-here" }
}
```

> **Warning:** Add `http-client.private.env.json` to your `.gitignore`. It holds tokens and passwords.

Pick the environment in the bar above the editor, or with "Select HTTP Environment" in the palette. Flint remembers the choice until it quits.

## Sending requests

A request is sent only when you ask for it:

- `⌘↵` with the cursor in the request;
- ▶ in the gutter next to the request;
- ▶ **Send** in the bar above the editor;
- "Send Request" in the palette.

Each request has a 30-second timeout (unless `@timeout` says otherwise) and can be cancelled. Every `.http` file has its own cookie jar; "Clear HTTP Cookies" empties it. **Follow redirects** in the bar (or "Toggle HTTP Follow Redirects") turns redirect following on or off; `# @no-redirect` turns it off for a single request. TLS errors are shown in readable form.

The editor highlights syntax and marks errors as you type. When you send, unknown variables are reported too, with their line number.

## Responses

The response opens in a tab next to the `.http` file. It shows the status, time, size, collapsible headers and the body in a read-only editor with JSON, XML or HTML highlighting. JSON is formatted.

- Bodies over 5 MB are shown partially; **Save to file** writes the whole body.
- **Copy as curl** (or "Copy Request as curl") copies the request as a `curl` command.
- Flint keeps a short history of the last few responses of each request, in memory only. Nothing is written to disk.

Flint doesn't write headers or variable values to its log.

## Command palette

"Send Request", "New HTTP Request File", "Select HTTP Environment", "Copy Request as curl", "Toggle HTTP Follow Redirects" and "Clear HTTP Cookies".

## Related

- [Use the HTTP client](../how-to/use-the-http-client.md)
- [Debug a Go service](../use-cases/debug-a-go-service.md)
