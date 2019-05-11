# lndmon

#### A drop-in monitoring solution for your lnd node using Prometheus and Grafana.

## What is this?
`lndmon` is a drop-in, dockerized monitoring/metric collection solution for your individual lnd nodes connected to bitcoin. With this system, you'll be able to closely monitor the health, status, and behavioral patterns of your lnd node and the lightning network as a whole.

There are three primary components of the `lndmon` system:
1. [lnd](https://github.com/lightningnetwork/lnd) built with the `monitoring` tag, which enables lnd to export metrics about its gRPC performance and usage. These metrics provide insights such as how many bytes lnd is transmitting over gRPC, whether any calls are taking a long time to complete, and other related statistics.
2. `lndmon`: while lnd provides some information, `lndmon` by far does the heavy lifting with regards to metrics. With `lndmon`'s data, you can track routing fees over time, track how the channel graph evolves, and have a highly configurable "crystal ball" to forecast and de-escalate potential issues as the network changes over time. There is also a strong set of metrics for users who want to keep track of their own node and channels, or just explore and create their own lightning data visualizations.
3. Last but not least, `lndmon` uses [Grafana](https://grafana.com/) as its primary dashboard to display all its collected metrics. Grafana is highly configurable and can create beautiful and detailed graphs organized by category (i.e., chain-related graphs, fee-related graphs, etc). Users have the option of making their Grafana dashboards remotely accessible over TLS with passwords to ensure their data is kept private.

## Why would I want to use this?
Monitoring can provide crucial insights into the health of large-scale distributed systems. Without monitoring systems like `lndmon`, the only view into the health of your lnd node and the overall network is (1) fragmented logs, and (2) individually-dispatched `getinfo` and similar commands. By exporting and graphing interesting metrics, one can get a real-time transparent view of the behavior of your lnd node and the network. It's also cool to see how this view changes over time and how it's affected by events in the larger bitcoin ecosystem (i.e., "wow, the day [Lightning App](https://github.com/lightninglabs/lightning-app) was released coincides with the addition of 3000 channels to the network!").

## How do I install this?
Head over to `INSTALL.md` in the same directory as this readme. `INSTALL.md` also includes instructions to set up, access, and password-protect the dashboard that comes with Prometheus, called the Prometheus expression browser, for those interested in using it.
