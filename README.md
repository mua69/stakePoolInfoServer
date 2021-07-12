# Particl Stakepool Info Server, Watchdog and Telegram Bot

## Usage

`stakepoolInfoServer <config file> [<telegram config file>]`

## JSON HTTP Interface

Server binds to localhost only. Port number is defined in configuration file.

The JSON HTTP server is enabled only if a port number > 0 is specified in the
configuration file. 

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

## Watchdog

Monitors a particld node and checks that it is actively staking. 
A failure status is reported by sending an email and/or a Telegram message.


Following message texts will be send:
* `normal operation`: send if particld resumes normal operation after failure was detected
* `communication to particld failed`: send if RPC communication to particld failed
* `particld is not staking, cause: <cause>`: send if particld is not staking, cause is taken from getstakinginfo results

Upon startup the watchdog will always send one of the above messages dependig on the current node status. 
This is useful to check that the messaging channels work.

The watchdog can use following messaging channels:
* email: Requires definition of configuration items `WatchdogEmailTo` and `WatchdogEmailFrom`.
* Telegram chat: Requires Telegram bot setup and definition of configuration item 
`WatchdogMsgChatName`. The Telegram bot must be a member of the defined chat.

The watchdog is only enabled if at least one of the messaging channels is defined.

Example for a minimal watchdog only setup using the email message channel:
```json
{
  "ParticldRpcPort": 51735,
  "ParticldDataDir":"<particld data dir>",
  "ParticldStakingWallet":"<staking wallet name>", 
  "WatchdogEmailTo": "<destination email address>",
  "WatchdogEmailFrom" : "<sender email address>",
  "WatchdogEmailSubject": "<optional subject>"
}
```

Example for an additional Telegram setup for sending watchdog messages to a chat:
```json
{
  "BotName": "<bot user name>",
  "BotAuth": "<bot authorization token",
  "WatchdogMsgChatName": "@<chat name>"
}
```
 
## Telegram Bot

Sends daily status message (same output like command `/status`) to a pre-configured chat.

Bot commands:
* `/start` - shows intro and help (the bot has no internal state so that an explicit start is not reuqired)
* `/status` - sends Particl node status message
* `/accountinfo <account id>` - retrieves balances of specified staking account
* `/stakinginfo [<amount>]` - sends information about current nominal and effective
 network staking interest rate. If an `<amount>` is given, the expected nominal and effective daily staking
 rewards for the given PART amount is printed as well.

## Configuration

Configuration files are in JSON format.

**Mandatory configuration file (`<config file>`):**
```json
{
  "Port": 9100, 
  "ParticldRpcPort": 51735,
  "ParticldDataDir":"/home/particl/pool",
  "ParticldStakingWallet":"pool_stake", 
  "StakePoolUrl": "<url>",
  "DbUrl": "dbname=<dbname>",
  "WatchdogEmailTo": "",
  "WatchdogEmailFrom" : "",
  "WatchdogEmailSubject": "<subject>"
}
```
* `Port`: integer: Port number for JSON HTTP server, defaults to `9100`, bind address is fixed to `localhost`
* `ParticldRpcPort`: integer: particld RPC port number, defaults to `51735`
* `ParticldDataDir`: string: particld data directory, used to retrieve authorization data from the .cookie file
* `ParticldStakingWallet:` string: name of staking wallet, used to retrieve staking status information
* `StakePoolUrl`: string: URL to staking pool JSON HTTP server, used to retrieve account info
* `DbUrl`: string: SQL database connect URL
* `WatchdogEmailTo`: string: RFC 5322 compliant email address, watchdog sends alert mails to this address
* `WatchdogEmailFrom`: string: RFC 5322 compliant email addr, used by watchdog as sender address for alert mails
* `WatchdogEmailSubject`: string: optional subject for watchdog alert mails, defaults to `"Particld Watchdog Alert"`

**Optional Telegram config file (`<telegram config file>`).**

If not provided the Telegram bot will not be started.
```json
{
  "BotName": "<bot user name>",
  "BotAuth": "<bot authorization token",
  "StatusMsgHour": 22,
  "StatusMsgMinute": 14,
  "StatusMsgChatName": "@<chat name>",
  "WatchdogMsgChatName": "@<chat name>"
}
```
* `BotName`: string: bot user name
* `BotAuth`: string: bot authorization token without leading `bot`
* `StatusMsgHour`: integer: hour (UTC) at which status message is sent
* `StatusMsgMinute`: integer: minute (UTC) at which status message is sent
* `StatusMsgChatName`: string: name of chat (including leading `@`) to which status message is sent
* `WatchdogMsgChatName`: string: optional name of chat (including leading `@`) to which watchdog messages will be send 