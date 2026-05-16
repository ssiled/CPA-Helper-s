# CPA Helper Backend

Go backend for the local CPA Helper application.

Run from this directory:

```powershell
go mod download
go run ./cmd/cpa-helper
```

Useful checks:

```powershell
go fmt ./...
go test ./...
```

Local build output goes under `bin/`:

```powershell
go build -o bin/cpa-helper.exe ./cmd/cpa-helper
```

Database migrations are managed by embedded goose migrations in `migrations/`.
The Docker image runs the same migrations automatically on application startup;
Alembic is not part of the Go runtime.
For Docker upgrades, the new image only needs the persisted SQLite database;
it does not require the old Python source tree or Alembic files.

