# Particl Stakepool Info Server and Telegram Bot

## Usage

`stakepoolInfoServer <config file> [<telegram config file>]`

## JSON HTTP Interface

Server binds to localhost only. Port number is defined in configuration file.

### Particl Node Status
 
GET Request: `http://localhost:<port>/stat`

Returns:
```json
{
  "status":"<node status message>",
  "uptime": "<uptime in days>",
  "peers":"<number of connected peers>",
  "last_block":"<last synced block>",
  "version":"<particld version>"
}
```

  
## Telegram Bot

Sends daily status message (same output like command `/status`) to a pre-configured chat.

Bot commands:
* `/start` - shows intro and help (the bot has no internal state so that an explicit start is not reuqired)
* `/status` - sends Particl node status message

## Configuration

Configuration files are in JSON format.

**Mandatory configuration file (`<config file>`):**
```json
{
  "Port": 9100, 
  "ParticldRpcPort": 51735,
  "ParticldDataDir":"/home/particl/pool",
  "ParticldStakingWallet":"pool_stake", 
  "DbUrl": ""
}
```
* `Port`: integer: Port number for JSON HTTP server, defaults to `9100`, bind address is fixed to `localhost`
* `ParticldRpcPort`: integer: particld RPC port number, defaults to `51735`
* `ParticldDataDir`: string: particld data directory, used to retrieve authorization data from the .cookie file
* `ParticldStakingWallet:` string: name of staking wallet, used to retrieve staking status information
* `DbUrl`: string: SQL database URL, not used up to now

**Optional Telegram config file (`<telegram config file>`).**

If not provided the Telegram bot will not be started.
```json
{
  "BotName": "<bot user name>",
  "BotAuth": "<bot authorization token",
  "StatusMsgHour": 22,
  "StatusMsgMinute": 14,
  "StatusMsgChatName": "@<chat name>"
}
```
* `BotName`: string: bot user name
* `BotAuth`: string: bot authorization token without leading `bot`
* `StatusMsgHour`: integer: hour (UTC) at which status message is sent
* `StatusMsgMinute`: integer: minute (UTC) at which status message is sent
* `StatusMsgChatName`: string: name of chat (including leading `@`) to which status message is sent
