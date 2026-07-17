# CPA Auth Pool integration

CPA-Helper-s does not ship or publish the `cpa-auth-pool` plugin from this repository. It only calls an already installed CPA plugin through CPA management APIs.

Plugin repository and store source:

```text
https://github.com/ssiled/CPA-Auth-Pool-Plugin
https://raw.githubusercontent.com/ssiled/CPA-Auth-Pool-Plugin/main/registry.json
```

## Required CPA plugin

Install and enable `cpa-auth-pool` in CPA first. CPA-Helper-s expects these management endpoints to exist:

```text
/v0/management/plugins/cpa-auth-pool/status
/v0/management/plugins/cpa-auth-pool/pools
/v0/management/plugins/cpa-auth-pool/bindings
/v0/management/plugins/cpa-auth-pool/events
```

If the plugin is not installed or disabled, CPA-Helper-s will show an error when opening Auth Pools or binding an API key to a pool.

## Use from CPA-Helper-s

1. Configure CPA URL and Management Key in settings.
2. Confirm CPA auth accounts are visible in the account inspection pages.
3. Open `Auth Pools`, create pools, and select accounts for each pool.
4. Open `API Keys`, create or edit a key, and choose a request pool.
5. Clients keep using the same CPA Base URL and API key; pool scheduling happens inside CPA.
6. Open `Plugin Event Log` when a bound request is blocked or an upstream request fails.

## Notes

- Pool account IDs must match CPA scheduler candidate auth IDs. The UI currently uses account names from CPA-Helper-s account inspection.
- Bound pools intentionally fail closed in the plugin: empty or unavailable pools do not fall back to other pools.
- Back up the plugin state file in CPA because it stores pool definitions and key bindings used by CPA runtime.
- Event logs are held only in plugin process memory and are cleared when CPA restarts.
