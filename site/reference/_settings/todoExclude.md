| `todoExclude` | array of strings | `[]` | Extra folders or globs the TODO list skips. See [todoExclude](#todoexclude). |

### todoExclude

A list of patterns in `.gitignore` style, such as `"docs/legacy"`, `"/generated"` or `"*.min.js"`. These are added to the folders the TODO list always skips. Each pattern:

- can't be empty, start with `!` or contain a comma;
- is at most 200 characters long.

Up to 100 patterns are kept; duplicates are removed.
