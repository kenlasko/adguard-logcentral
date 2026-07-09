# Overview
I have multiple (3) AdguardHome instances on my home network to provide highly available DNS services to devices within my home. The configuration is kept in sync with one another via [AdGuard-Sync](https://github.com/bakito/adguardhome-sync). This works well, but I sometimes need to query the logs to find out where a block might be happening. This requires me to open the web instances for all three AdguardHome instances.

As an AdguardHome administrator, I want the ability to use a single central web-based interface to centralize the viewing of all connected AdguardHome instances.

# Requirements
- Built on Go
- All configuration done through environment variables/secrets
- No persistent storage. Load logs live on request
- Authentication done only via OIDC (Pocket-ID is what I use in my environment)

## Critical Rules

### Code Organization
- Many small files over few large files (200-400 lines typical, 800 max)
- Organize by feature/domain, not by type
- Always create tests for any new functionality added

### Code Style
- No emojis in code, comments, or documentation
- Immutability always -- never mutate objects or arrays
- Never store sensitive information in the repo. Use secrets
