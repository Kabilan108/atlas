documentation for agents + skill

should be able to update ttitle for snippets

$ atlas snippet update --help
Update a snippet

Usage:
  atlas snippet update <id> [flags]

Flags:
  -f, --file strings       Files to add or update
  -h, --help               help for update
  -r, --remove strings     Files to remove
      --workspace string   Target workspace

Global Flags:
      --no-cache   Bypass disk cache entirely
  -v, --verbose    Show inferred values (repo from git remote, etc.)

the snippets view command should expose a way to pipe the file contents to a real file or download all files in a snippet
