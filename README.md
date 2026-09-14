# IronSmith Multiplayer Lab

Public lab for testing and breaking the multiplayer system compatible with:

[IronSmith](https://github.com/FiammaMuscari/ironsmith)

## Live demo

[Open multiplayer lab](https://lab-multiplayer.onrender.com)

## What to test

Open the demo in two browser tabs or devices.

- Create a room
- Join from another client
- Send commands
- Burst commands
- Disconnect/reconnect
- Pause incoming messages
- Force divergence
- Test replay/resync

The goal is to reproduce multiplayer failures and validate fixes before porting them back to IronSmith.

## Local

```sh
go run ./cmd/server
```
