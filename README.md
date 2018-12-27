# Particl Stakepool Info Server and Telegram Bot

## Usage

`stakepoolInfoServer <config file> [<telegram config file>]`
## JSON HTTP interface

###Particl node status
 
Request: `http://localhost:<port>/stat`

Returns:
```json
{
  "status":"<node status message>",
  "uptime": "<uptime in days",
  "peers":"<number of connected peers",
  "last_block":"<last synced block>",
  "version":"particld version"
}
```

  
## Telegram Bot

Sends daily status message to a pre-configured chat.

Bot commands:
* `/start` - starts bot, shows intro and help
* `/status` - sends Particl node status message

## Configuration

Configuration files are in JSON format.

Mandatory configuration file (`<config file>`):
```json
{
  "Port":9100,                          # Port number for JSON HTTP server
  "ParticldRpcPort":51735,              # RPC port number of particld
  "ParticldDataDir":"/data/particl",    # particld data directory, for .cookie lookup
  "ParticldStakingWallet":"pool_stake", # name of staking wallet
  "DbUrl":""                            # not used up to now
}

```

Optional Telegram config file (`<telegram config file>`). If not provided Telegram bot will not be started.
```json
{
  "BotName": "<bot user name>",          # bot user name
  "BotAuth": "<bot authorization token", # bot authorization token without lead "bot"
  "StatusMsgHour": 22,                   # hour (UTC) at which status message is sent
  "StatusMsgMinute": 14,                 # minute (UTC) at which status message is sent
  "StatusMsgChatName": "@<chat name>"    # name of chat to which status message is sent
}
```
