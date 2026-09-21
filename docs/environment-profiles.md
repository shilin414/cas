# Development, test, and production environment profiles

The backend selects a dotenv profile before configuration is loaded. Set only
`STUDIO_ENV`; files no longer need to be copied or renamed.

| Selector | Files loaded in precedence order |
| --- | --- |
| unset or `dev` | `.env.development.local`, `.env.local`, `.env.development`, `.env` |
| `test` | `.env.test.local`, `.env.development.local`, `.env.local`, `.env.test`, `.env.development`, `.env` |
| `prod` | `.env.production.local`, `.env.local`, `.env.production`, `.env` |

Earlier files win and real process environment variables override every file.
The test profile intentionally falls back to the development profile, so the
current development and test environments share database, Redis, and other
settings. `.env.test.local` only needs settings that differ, such as
`APP_ENV=test`.

PowerShell examples:

```powershell
# Development is the default
./bin/api.exe

$env:STUDIO_ENV = 'test'
./bin/api.exe

$env:STUDIO_ENV = 'prod'
./bin/api.exe
```

Linux/service examples:

```bash
STUDIO_ENV=test ./bin/api
STUDIO_ENV=prod ./bin/api
```

All `*.local` files are ignored by Git and may contain deployment secrets.
`STUDIO_ENV` must be provided by the shell, service manager, or container; a
profile cannot select itself before it has been loaded.
