## Lightning Network Daemon - special WHATSAT edition

This repo is a fork of [`lnd`](https://github.com/lightningnetwork/lnd) that demonstrates how the Lightning Network
can be used as an end-to-end encrypted, onion-routed, censorship-resistant, peer-to-peer chat messages protocol.

<img src="whatsat.gif" alt="screencast" width="880" />

Recent changes to the protocol made it possible to attach arbitrary data to a payment. This demo leverages that by attaching a text message and a sender signature.

A Lightning payment delivers the message, but no actual money is paid. Because the sender uses a random payment hash, the receiver is not able to settle the payment. The failure message that is returned to the sender serves as a delivery confirmation.

This means that chatting is currently free. However, there is a future in which 'free failures' don't exist anymore. Nodes may start charging a prepaid relay fee and/or start rate limiting sources that produce too many failures. In that case, chatting over Lightning may switch to actually settling the messaging payments and dropping off a few millisats at every hop.

## Usage

* Set up a Lightning Node as usual and open a channel to a well-connected node.

* Run `lncli chat <pubkey>` to start chatting with your chosen destination.

  If running `lncli chat` without a pubkey, a pubkey to send to can be set by typing `/<pubkey>` in the send box.

## Disclaimer

This code only serves to demonstrate the concept and doesn't pass the required quality checks. Use with testnet sats only.