# Use the HTTP client

Recipes for writing and sending requests from `.http` files. For the full file format, see [HTTP client](../guides/http-client.md).

## Create a request file

1. Run "New HTTP Request File" from the command palette, or create a file ending in `.http` or `.rest`.
2. Write a request:

   ```http
   ### Health check
   GET http://localhost:8080/healthz
   Accept: application/json
   ```

Separate requests with `###`. Text after `###` names the request.

## Send a request

1. Put the cursor anywhere in the request.
2. Press `⌘↵`, click ▶ in the gutter, or click ▶ **Send** in the bar above the editor.

The response opens in a tab next to the file. Requests are never sent automatically.

## Set up environments

1. Next to the `.http` file, create `http-client.env.json`:

   ```json
   {
     "$shared": { "apiVersion": "v1" },
     "local": { "baseUrl": "http://localhost:8080" },
     "staging": { "baseUrl": "https://staging.example.com" }
   }
   ```

2. Put secrets in `http-client.private.env.json`, with the same structure:

   ```json
   {
     "local": { "token": "local-dev-token" },
     "staging": { "token": "paste-your-token-here" }
   }
   ```

3. Add `http-client.private.env.json` to `.gitignore`.
4. Pick the environment in the bar above the editor, or with "Select HTTP Environment" in the palette.

Values in the private file override the public one, and `$shared` applies to every environment.

## Use variables

1. Reference a variable with `{{name}}`:

   ```http
   GET {{baseUrl}}/{{apiVersion}}/orders
   Authorization: Bearer {{token}}
   ```

2. Define file-level values with `@name = value` above the requests that use them:

   ```http
   @orderId = 42

   ### Get one order
   GET {{baseUrl}}/{{apiVersion}}/orders/{{orderId}}
   ```

3. Use dynamic values where you need fresh data: `{{$uuid}}`, `{{$timestamp}}`, `{{$isoTimestamp}}`, `{{$randomInt}}`.

When you send, Flint reports unknown variables with their line number.

## Send a body from a file

1. Add an empty line after the headers.
2. Write `< ./payload.json` (relative to the `.http` file), or `<@ ./payload.json` to substitute variables inside it.

The file must be inside the open project.

## Save a response

1. In the response tab, click **Save to file**.
2. Choose where to save it.

This writes the whole body, even when the tab shows only the beginning of a large one.

## Copy a request as curl

1. Put the cursor in the request.
2. Run "Copy Request as curl" from the palette, or use **Copy as curl** in the response tab.

## Control redirects, cookies and timeouts

- Turn **Follow redirects** on or off in the bar above the editor.
- Add `# @no-redirect` or `# @no-cookie-jar` above a single request.
- Add `# @timeout 60` (seconds, or for example `2m`) to change the 30-second default.
- Run "Clear HTTP Cookies" to empty the file's cookie jar.
